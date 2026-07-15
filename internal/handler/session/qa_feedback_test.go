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

func TestCompleteAssistantMessagePersistsAttributionAfterSuccessfulUpdate(t *testing.T) {
	messageService := &completionMessageService{
		updated: make(chan *types.Message, 1),
		indexed: make(chan struct{}, 1),
	}
	feedbackService := &completionFeedbackService{
		err:   errors.New("simulated asynchronous failure"),
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
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(17))

	handler.completeAssistantMessage(ctx, message, "question")

	select {
	case updated := <-messageService.updated:
		if updated != message || !updated.IsCompleted {
			t.Fatalf("updated message = %#v", updated)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message update was not called")
	}

	select {
	case call := <-feedbackService.calls:
		if call.message == message || call.message.ID != message.ID {
			t.Fatalf("attribution message copy = %#v", call.message)
		}
		if call.message.KnowledgeReferences[0] == reference {
			t.Fatal("attribution retained a mutable search-result pointer")
		}
		if tenantID, ok := types.SessionTenantIDFromContext(call.ctx); !ok || tenantID != 17 {
			t.Fatalf("attribution tenant = %d, ok=%v", tenantID, ok)
		}
		if call.ctx.Err() != nil {
			t.Fatalf("attribution context was canceled: %v", call.ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("feedback attribution was not called")
	}

	select {
	case <-messageService.indexed:
	case <-time.After(2 * time.Second):
		t.Fatal("history indexing was not called")
	}
}

func TestCompleteAssistantMessageSkipsAttributionWhenUpdateFails(t *testing.T) {
	messageService := &completionMessageService{
		updateErr: errors.New("update failed"),
		updated:   make(chan *types.Message, 1),
		indexed:   make(chan struct{}, 1),
	}
	feedbackService := &completionFeedbackService{calls: make(chan completionFeedbackCall, 1)}
	handler := &Handler{messageService: messageService, feedbackService: feedbackService}

	handler.completeAssistantMessage(
		context.WithValue(context.Background(), types.TenantIDContextKey, uint64(17)),
		&types.Message{ID: "message-1", SessionID: "session-1", Role: "assistant"},
		"question",
	)

	select {
	case <-messageService.updated:
	case <-time.After(2 * time.Second):
		t.Fatal("message update was not called")
	}
	select {
	case <-feedbackService.calls:
		t.Fatal("feedback attribution ran after the message update failed")
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-messageService.indexed:
	case <-time.After(2 * time.Second):
		t.Fatal("existing history indexing behavior did not run")
	}
}
