package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

func setupFeedbackTestRepository(t *testing.T) (*feedbackRepository, *gorm.DB, *types.Session, *types.Message, *types.Chunk) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&types.Session{},
		&types.Message{},
		&types.Chunk{},
		&types.MessageChunkReference{},
		&types.MessageFeedback{},
		&types.ChunkFeedbackAudit{},
	))
	session := &types.Session{TenantID: 101, UserID: "user-a"}
	require.NoError(t, db.Create(session).Error)
	message := &types.Message{SessionID: session.ID, Role: "assistant", Content: "draft"}
	require.NoError(t, db.Create(message).Error)
	chunk := &types.Chunk{
		ID: "chunk-a", TenantID: 202, KnowledgeBaseID: "kb-a", KnowledgeID: "knowledge-a",
		Content: "source", SourceContent: "source", RecallWeight: 1, IsEnabled: true,
	}
	require.NoError(t, db.Create(chunk).Error)
	return &feedbackRepository{db: db}, db, session, message, chunk
}

func feedbackReference(chunk *types.Chunk) types.References {
	return types.References{&types.SearchResult{
		ID: chunk.ID, KnowledgeBaseID: chunk.KnowledgeBaseID, ChunkType: types.ChunkTypeText,
	}}
}

func loadFeedbackChunk(t *testing.T, db *gorm.DB, id string) types.Chunk {
	t.Helper()
	var chunk types.Chunk
	require.NoError(t, db.First(&chunk, "id = ?", id).Error)
	return chunk
}

func TestFeedbackLifecycleAndResetBaseline(t *testing.T) {
	repo, db, session, message, chunk := setupFeedbackTestRepository(t)
	ctx := context.Background()
	message.Content = "final"

	eligible, err := repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)
	assert.True(t, eligible)

	// The exact same completion is idempotent; attribution cannot later drift.
	eligible, err = repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)
	assert.True(t, eligible)
	_, err = repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, nil)
	assert.ErrorIs(t, err, ErrFeedbackCompletionState)

	state, err := repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: types.FeedbackTypeLike,
	})
	require.NoError(t, err)
	assert.Equal(t, types.FeedbackTypeLike, state.Type)
	got := loadFeedbackChunk(t, db, chunk.ID)
	assert.EqualValues(t, 1, got.LikeCount)
	assert.Equal(t, 1.2, got.RecallWeight)

	reason := types.FeedbackReasonInaccurate
	_, err = repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-b", SessionID: session.ID, MessageID: message.ID,
		Type: types.FeedbackTypeDislike, ReasonCode: &reason,
	})
	require.NoError(t, err)
	got = loadFeedbackChunk(t, db, chunk.ID)
	assert.EqualValues(t, 1, got.LikeCount)
	assert.EqualValues(t, 1, got.DislikeCount)
	assert.Equal(t, 1.0, got.RecallWeight)

	require.NoError(t, repo.ResetChunkFeedback(ctx, types.ResetChunkFeedbackInput{
		ChunkTenantID: chunk.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "admin", KnowledgeBaseID: chunk.KnowledgeBaseID, ChunkID: chunk.ID,
	}))
	got = loadFeedbackChunk(t, db, chunk.ID)
	assert.Zero(t, got.LikeCount)
	assert.Zero(t, got.DislikeCount)
	assert.Nil(t, got.PositiveRate)
	assert.Equal(t, 1.0, got.RecallWeight)

	// Re-submitting an unchanged old vote creates a revision beyond the reset baseline.
	_, err = repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: types.FeedbackTypeLike,
	})
	require.NoError(t, err)
	got = loadFeedbackChunk(t, db, chunk.ID)
	assert.EqualValues(t, 1, got.LikeCount)
	assert.Equal(t, 1.2, got.RecallWeight)

	require.NoError(t, repo.DeleteMessageWithFeedback(ctx, session.TenantID, session.ID, message.ID, "user-a"))
	got = loadFeedbackChunk(t, db, chunk.ID)
	assert.Zero(t, got.LikeCount)
	assert.Zero(t, got.DislikeCount)
	assert.Equal(t, 1.0, got.RecallWeight)
}

func TestFeedbackTransactionRollsBackOnAuditFailure(t *testing.T) {
	repo, db, session, message, chunk := setupFeedbackTestRepository(t)
	ctx := context.Background()
	_, err := repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_feedback_audit
		BEFORE INSERT ON chunk_feedback_audits
		BEGIN SELECT RAISE(ABORT, 'audit failure'); END;
	`).Error)

	_, err = repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: types.FeedbackTypeLike,
	})
	require.Error(t, err)

	var count int64
	require.NoError(t, db.Model(&types.MessageFeedback{}).Count(&count).Error)
	assert.Zero(t, count)
	got := loadFeedbackChunk(t, db, chunk.ID)
	assert.Zero(t, got.LikeCount)
	assert.Equal(t, 1.0, got.RecallWeight)
}

func TestCompletionExcludesWebOnlyReferences(t *testing.T) {
	repo, db, session, message, _ := setupFeedbackTestRepository(t)
	eligible, err := repo.CompleteAssistantMessageWithReferences(
		context.Background(), session.TenantID, message,
		types.References{&types.SearchResult{ID: "web-result", ChunkType: types.ChunkTypeWebSearch}},
	)
	require.NoError(t, err)
	assert.False(t, eligible)

	var count int64
	require.NoError(t, db.Model(&types.MessageChunkReference{}).Count(&count).Error)
	assert.Zero(t, count)
	_, err = repo.ApplyMessageFeedback(context.Background(), types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: types.FeedbackTypeLike,
	})
	assert.True(t, errors.Is(err, ErrFeedbackNotEligible))
}

func TestHydrateChunksUsesPersistedAttributionTable(t *testing.T) {
	repo, _, session, message, chunk := setupFeedbackTestRepository(t)
	ctx := context.Background()
	_, err := repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)

	hydrated := *chunk
	require.NoError(t, repo.HydrateChunks(ctx, []*types.Chunk{&hydrated}, 0.5))
	assert.EqualValues(t, 1, hydrated.SessionCount)
}

func TestOrdinaryChunkSaveCannotOverwriteFeedbackProjection(t *testing.T) {
	_, db, _, _, chunk := setupFeedbackTestRepository(t)
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", chunk.ID).Updates(map[string]interface{}{
		"like_count": 4, "dislike_count": 1, "positive_rate": 0.8, "recall_weight": 1.2,
	}).Error)

	stale := *chunk
	stale.Content = "edited content"
	stale.LikeCount = 0
	stale.DislikeCount = 0
	stale.PositiveRate = nil
	stale.RecallWeight = 1
	require.NoError(t, (&chunkRepository{db: db}).UpdateChunk(context.Background(), &stale))

	got := loadFeedbackChunk(t, db, chunk.ID)
	assert.Equal(t, "edited content", got.Content)
	assert.EqualValues(t, 4, got.LikeCount)
	assert.EqualValues(t, 1, got.DislikeCount)
	require.NotNil(t, got.PositiveRate)
	assert.Equal(t, 0.8, *got.PositiveRate)
	assert.Equal(t, 1.2, got.RecallWeight)
}

func TestConcurrentFeedbackConvergesToExactProjection(t *testing.T) {
	repo, db, session, message, chunk := setupFeedbackTestRepository(t)
	ctx := context.Background()
	_, err := repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			feedbackType := types.FeedbackTypeLike
			if index%3 == 0 {
				feedbackType = types.FeedbackTypeDislike
			}
			_, applyErr := repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
				MessageTenantID: session.TenantID,
				ActorTenantID:   session.TenantID,
				ActorUserID:     fmt.Sprintf("concurrent-%02d", index),
				SessionID:       session.ID,
				MessageID:       message.ID,
				Type:            feedbackType,
			})
			errs <- applyErr
		}(i)
	}
	wg.Wait()
	close(errs)
	for applyErr := range errs {
		require.NoError(t, applyErr)
	}

	got := loadFeedbackChunk(t, db, chunk.ID)
	assert.EqualValues(t, 8, got.LikeCount)
	assert.EqualValues(t, 4, got.DislikeCount)
	require.NotNil(t, got.PositiveRate)
	assert.InDelta(t, 2.0/3.0, *got.PositiveRate, 1e-9)
	assert.Equal(t, 1.0, got.RecallWeight)
}
