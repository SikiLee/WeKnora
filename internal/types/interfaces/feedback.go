package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// FeedbackRepository owns persistence and transactional aggregate updates.
type FeedbackRepository interface {
	CreateMessageChunkReferences(ctx context.Context, refs []*types.MessageChunkReference) error
	ListMessageChunkReferences(ctx context.Context, sessionTenantID uint64, messageID string) ([]*types.MessageChunkReference, error)
	GetMessageFeedback(ctx context.Context, sessionTenantID uint64, userID, messageID string) (*types.MessageFeedback, error)
	ListMessageFeedbacks(ctx context.Context, sessionTenantID uint64, userID string, messageIDs []string) ([]*types.MessageFeedback, error)
	ApplyMessageFeedback(ctx context.Context, mutation types.MessageFeedbackMutation, cfg *types.ChunkFeedbackConfig) (*types.MessageFeedback, error)
}

// FeedbackService validates caller scope and applies answer feedback state transitions.
type FeedbackService interface {
	PersistMessageChunkReferences(ctx context.Context, message *types.Message) error
	SetMessageFeedback(ctx context.Context, sessionID, messageID string, input *types.MessageFeedbackInput) (*types.MessageFeedbackState, error)
	GetMessageFeedback(ctx context.Context, sessionID, messageID string) (*types.MessageFeedbackState, error)
}
