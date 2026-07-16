package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type completionMessageService struct {
	interfaces.MessageService
	updateErr error
	updated   chan *types.Message
	indexed   chan struct{}
}

func (s *completionMessageService) UpdateMessage(_ context.Context, message *types.Message) error {
	s.updated <- message
	return s.updateErr
}

func (s *completionMessageService) IndexMessageToKB(context.Context, string, string, string, string) {
	s.indexed <- struct{}{}
}

type completionFeedbackService struct {
	interfaces.FeedbackService
	err   error
	calls chan completionFeedbackCall
}

type completionFeedbackCall struct {
	ctx     context.Context
	message *types.Message
}

func (s *completionFeedbackService) PersistMessageChunkReferences(ctx context.Context, message *types.Message) error {
	s.calls <- completionFeedbackCall{ctx: ctx, message: message}
	return s.err
}

func (s *completionFeedbackService) CompleteAssistantMessage(ctx context.Context, message *types.Message) error {
	s.calls <- completionFeedbackCall{ctx: ctx, message: message}
	return s.err
}

func TestCompleteAssistantMessageUsesAtomicFeedbackCompletion(t *testing.T) {
	messageService := &completionMessageService{
		updated: make(chan *types.Message, 1),
		indexed: make(chan struct{}, 1),
	}
	feedbackService := &completionFeedbackService{
		calls: make(chan completionFeedbackCall, 1),
	}
	handler := &Handler{messageService: messageService, feedbackService: feedbackService}
	reference := &types.SearchResult{ID: "chunk-1", Content: "original"}
	message := &types.Message{
		ID:                  "message-1",
		SessionID:           "session-1",
		Role:                "assistant",
		KnowledgeReferences: types.References{reference},
	}
	parent, cancel := context.WithCancel(context.Background())
	ctx := context.WithValue(parent, types.TenantIDContextKey, uint64(17))
	cancel()

	handler.completeAssistantMessage(ctx, message, "question")

	select {
	case <-messageService.updated:
		t.Fatal("non-transactional message update was called")
	default:
	}

	select {
	case call := <-feedbackService.calls:
		if call.message != message || !call.message.IsCompleted {
			t.Fatalf("completion message = %#v", call.message)
		}
		if tenantID, ok := types.SessionTenantIDFromContext(call.ctx); !ok || tenantID != 17 {
			t.Fatalf("completion tenant = %d, ok=%v", tenantID, ok)
		}
		if call.ctx.Err() != nil {
			t.Fatalf("completion context was canceled: %v", call.ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("atomic feedback completion was not called")
	}

	select {
	case <-messageService.indexed:
	case <-time.After(2 * time.Second):
		t.Fatal("history indexing was not called")
	}
}

func TestCompleteAssistantMessageSkipsDerivedWorkWhenTransactionFails(t *testing.T) {
	messageService := &completionMessageService{
		updated: make(chan *types.Message, 1),
		indexed: make(chan struct{}, 1),
	}
	feedbackService := &completionFeedbackService{
		err:   errors.New("transaction failed"),
		calls: make(chan completionFeedbackCall, 1),
	}
	handler := &Handler{messageService: messageService, feedbackService: feedbackService}

	handler.completeAssistantMessage(
		context.WithValue(context.Background(), types.TenantIDContextKey, uint64(17)),
		&types.Message{ID: "message-1", SessionID: "session-1", Role: "assistant"},
		"question",
	)

	select {
	case <-feedbackService.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("atomic completion was not called")
	}
	select {
	case <-messageService.indexed:
		t.Fatal("history indexing ran after completion transaction failed")
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case <-messageService.updated:
		t.Fatal("non-transactional fallback update ran despite feedback service")
	default:
	}
}

func TestCompleteAssistantMessageFallsBackWithoutFeedbackService(t *testing.T) {
	messageService := &completionMessageService{
		updated: make(chan *types.Message, 1),
		indexed: make(chan struct{}, 1),
	}
	handler := &Handler{messageService: messageService}
	message := &types.Message{ID: "message-1", SessionID: "session-1", Role: "assistant"}

	handler.completeAssistantMessage(
		context.WithValue(context.Background(), types.TenantIDContextKey, uint64(17)),
		message,
		"question",
	)

	select {
	case updated := <-messageService.updated:
		if updated != message || !updated.IsCompleted {
			t.Fatalf("updated message = %#v", updated)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fallback message update was not called")
	}
	select {
	case <-messageService.indexed:
	case <-time.After(2 * time.Second):
		t.Fatal("history indexing was not called")
	}
}
