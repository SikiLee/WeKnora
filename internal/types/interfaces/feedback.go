package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// FeedbackRepository owns persistence and transactional aggregate updates.
type FeedbackRepository interface {
	CompleteAssistantMessage(
		ctx context.Context, message *types.Message, refs []*types.MessageChunkReference,
	) error
	CreateMessageChunkReferences(ctx context.Context, refs []*types.MessageChunkReference) error
	ListMessageChunkReferences(
		ctx context.Context, sessionTenantID uint64, messageID string,
	) ([]*types.MessageChunkReference, error)
	GetMessageFeedback(
		ctx context.Context, sessionTenantID uint64, userID, messageID string,
	) (*types.MessageFeedback, error)
	ListMessageFeedbacks(
		ctx context.Context, sessionTenantID uint64, userID string, messageIDs []string,
	) ([]*types.MessageFeedback, error)
	ApplyMessageFeedback(
		ctx context.Context, mutation types.MessageFeedbackMutation, cfg *types.ChunkFeedbackConfig,
	) (*types.MessageFeedback, error)
	ListChunkFeedback(
		ctx context.Context,
		tenantID uint64,
		kbID string,
		query *types.ChunkFeedbackListQuery,
		cfg *types.ChunkFeedbackConfig,
	) ([]*types.ChunkFeedbackListItem, int64, error)
	GetChunkFeedbackDetail(ctx context.Context, tenantID uint64, kbID, chunkID string) (*types.ChunkFeedbackDetail, error)
	ListChunkFeedbackWeightLogs(
		ctx context.Context, tenantID uint64, kbID, chunkID string, page *types.Pagination,
	) ([]*types.ChunkFeedbackWeightLogItem, int64, error)
	ResetChunkFeedback(
		ctx context.Context,
		tenantID uint64,
		kbID, chunkID, reason string,
		cfg *types.ChunkFeedbackConfig,
	) (*types.ChunkFeedbackDetail, error)
}

// FeedbackService validates caller scope and applies answer feedback state transitions.
type FeedbackService interface {
	CompleteAssistantMessage(ctx context.Context, message *types.Message) error
	PersistMessageChunkReferences(ctx context.Context, message *types.Message) error
	SetMessageFeedback(
		ctx context.Context, sessionID, messageID string, input *types.MessageFeedbackInput,
	) (*types.MessageFeedbackState, error)
	GetMessageFeedback(ctx context.Context, sessionID, messageID string) (*types.MessageFeedbackState, error)
	ListChunkFeedback(ctx context.Context, kbID string, query *types.ChunkFeedbackListQuery) (*types.PageResult, error)
	GetChunkFeedbackDetail(ctx context.Context, kbID, chunkID string) (*types.ChunkFeedbackDetail, error)
	ListChunkFeedbackWeightLogs(
		ctx context.Context, kbID, chunkID string, page *types.Pagination,
	) (*types.PageResult, error)
	ResetChunkFeedback(
		ctx context.Context, kbID, chunkID string, input *types.ChunkFeedbackResetInput,
	) (*types.ChunkFeedbackDetail, error)
}
