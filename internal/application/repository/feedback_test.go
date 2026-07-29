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
	require.NoError(t, db.Exec(`
		CREATE TABLE sessions (
			id text PRIMARY KEY,
			tenant_id integer NOT NULL,
			user_id text NOT NULL,
			deleted_at datetime
		);
		CREATE TABLE messages (
			id text PRIMARY KEY,
			session_id text NOT NULL,
			content text,
			role text,
			knowledge_references json,
			agent_steps json,
			is_completed numeric NOT NULL DEFAULT 0,
			is_fallback numeric NOT NULL DEFAULT 0,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime
		);
	`).Error)
	require.NoError(t, db.AutoMigrate(
		&types.Chunk{},
		&types.MessageChunkReference{},
		&types.MessageFeedback{},
		&types.ChunkFeedbackAudit{},
	))
	session := &types.Session{ID: "session-a", TenantID: 101}
	require.NoError(t, db.Exec(
		"INSERT INTO sessions (id, tenant_id, user_id) VALUES (?, ?, ?)",
		session.ID, session.TenantID, session.UserID,
	).Error)
	message := &types.Message{
		ID: "message-a", SessionID: session.ID, Role: "assistant", Content: "draft",
	}
	require.NoError(t, db.Exec(
		"INSERT INTO messages (id, session_id, content, role, is_completed) VALUES (?, ?, ?, ?, ?)",
		message.ID, message.SessionID, message.Content, message.Role, false,
	).Error)
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

func feedbackReferences(chunks ...*types.Chunk) types.References {
	references := make(types.References, 0, len(chunks))
	for _, chunk := range chunks {
		references = append(references, &types.SearchResult{
			ID: chunk.ID, KnowledgeBaseID: chunk.KnowledgeBaseID, ChunkType: types.ChunkTypeText,
		})
	}
	return references
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
	var deleteAudit types.ChunkFeedbackAudit
	require.NoError(t, db.Order("id DESC").First(&deleteAudit).Error)
	assert.Equal(t, types.FeedbackTriggerContentDelete, deleteAudit.TriggerSource)
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

func TestFeedbackAuditTriggerSourcesAndIdempotency(t *testing.T) {
	repo, db, session, message, chunk := setupFeedbackTestRepository(t)
	ctx := context.Background()
	_, err := repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)

	apply := func(feedbackType types.FeedbackType) {
		t.Helper()
		_, applyErr := repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
			MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
			ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: feedbackType,
		})
		require.NoError(t, applyErr)
	}
	auditSources := func() []types.FeedbackTriggerSource {
		t.Helper()
		var audits []types.ChunkFeedbackAudit
		require.NoError(t, db.Order("id").Find(&audits).Error)
		sources := make([]types.FeedbackTriggerSource, 0, len(audits))
		for _, audit := range audits {
			sources = append(sources, audit.TriggerSource)
		}
		return sources
	}

	apply(types.FeedbackTypeLike)
	assert.Equal(t, []types.FeedbackTriggerSource{types.FeedbackTriggerLike}, auditSources())

	apply(types.FeedbackTypeLike)
	assert.Equal(t, []types.FeedbackTriggerSource{types.FeedbackTriggerLike}, auditSources(),
		"idempotent feedback must not create another weight-change audit")

	apply(types.FeedbackTypeDislike)
	apply(types.FeedbackTypeNone)
	require.NoError(t, repo.ResetChunkFeedback(ctx, types.ResetChunkFeedbackInput{
		ChunkTenantID: chunk.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "admin", KnowledgeBaseID: chunk.KnowledgeBaseID, ChunkID: chunk.ID,
	}))
	assert.Equal(t, []types.FeedbackTriggerSource{
		types.FeedbackTriggerLike,
		types.FeedbackTriggerDislike,
		types.FeedbackTriggerCancel,
		types.FeedbackTriggerAdminReset,
	}, auditSources())
	var audits []types.ChunkFeedbackAudit
	require.NoError(t, db.Order("id").Find(&audits).Error)
	require.Len(t, audits, 4)
	for _, audit := range audits[:3] {
		assert.Equal(t, types.ChunkFeedbackAuditActionWeightChanged, audit.Action)
	}
	assert.Equal(t, types.ChunkFeedbackAuditActionReset, audits[3].Action)
}

func TestFeedbackAuditTriggerSourceDefaultsToLegacy(t *testing.T) {
	_, db, _, _, chunk := setupFeedbackTestRepository(t)
	require.NoError(t, db.Exec(`
		INSERT INTO chunk_feedback_audits
			(chunk_tenant_id, chunk_id, actor_tenant_id, actor_user_id, action, old_weight, new_weight, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, chunk.TenantID, chunk.ID, uint64(101), "legacy-user",
		types.ChunkFeedbackAuditActionWeightChanged, 1.0, 1.2).Error)

	var audit types.ChunkFeedbackAudit
	require.NoError(t, db.Last(&audit).Error)
	assert.Equal(t, types.FeedbackTriggerLegacy, audit.TriggerSource)
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

func TestDeletingOneOfThreeReferencesStillAllowsLike(t *testing.T) {
	repo, db, session, message, chunkA := setupFeedbackTestRepository(t)
	ctx := context.Background()
	chunkB := &types.Chunk{
		ID: "chunk-b", TenantID: chunkA.TenantID, KnowledgeBaseID: chunkA.KnowledgeBaseID,
		KnowledgeID: chunkA.KnowledgeID, Content: "b", SourceContent: "b",
		RecallWeight: 1, IsEnabled: true,
	}
	chunkC := &types.Chunk{
		ID: "chunk-c", TenantID: chunkA.TenantID, KnowledgeBaseID: chunkA.KnowledgeBaseID,
		KnowledgeID: chunkA.KnowledgeID, Content: "c", SourceContent: "c",
		RecallWeight: 1, IsEnabled: true,
	}
	require.NoError(t, db.Create([]*types.Chunk{chunkB, chunkC}).Error)
	_, err := repo.CompleteAssistantMessageWithReferences(
		ctx, session.TenantID, message, feedbackReferences(chunkA, chunkB, chunkC),
	)
	require.NoError(t, err)
	require.NoError(t, (&chunkRepository{db: db}).DeleteChunk(ctx, chunkB.TenantID, chunkB.ID))

	_, err = repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: types.FeedbackTypeLike,
	})
	require.NoError(t, err)
	for _, id := range []string{chunkA.ID, chunkC.ID} {
		got := loadFeedbackChunk(t, db, id)
		assert.EqualValues(t, 1, got.LikeCount)
		assert.Equal(t, 1.2, got.RecallWeight)
	}

	var referenceCount int64
	require.NoError(t, db.Model(&types.MessageChunkReference{}).
		Where("message_id = ?", message.ID).Count(&referenceCount).Error)
	assert.EqualValues(t, 2, referenceCount)
	var deleted types.Chunk
	require.NoError(t, db.Unscoped().First(&deleted, "id = ?", chunkB.ID).Error)
	assert.True(t, deleted.DeletedAt.Valid)
	assert.Zero(t, deleted.LikeCount)
}

func TestDeletedReferencedChunkDoesNotBlockRemainingFeedback(t *testing.T) {
	repo, db, session, message, chunkA := setupFeedbackTestRepository(t)
	ctx := context.Background()
	chunkB := &types.Chunk{
		ID: "chunk-b", TenantID: chunkA.TenantID, KnowledgeBaseID: chunkA.KnowledgeBaseID,
		KnowledgeID: chunkA.KnowledgeID, Content: "b", SourceContent: "b",
		RecallWeight: 1, IsEnabled: true,
	}
	chunkC := &types.Chunk{
		ID: "chunk-c", TenantID: chunkA.TenantID, KnowledgeBaseID: chunkA.KnowledgeBaseID,
		KnowledgeID: chunkA.KnowledgeID, Content: "c", SourceContent: "c",
		RecallWeight: 1, IsEnabled: true,
	}
	require.NoError(t, db.Create([]*types.Chunk{chunkB, chunkC}).Error)
	_, err := repo.CompleteAssistantMessageWithReferences(
		ctx, session.TenantID, message, feedbackReferences(chunkA, chunkB, chunkC),
	)
	require.NoError(t, err)

	apply := func(feedbackType types.FeedbackType) {
		t.Helper()
		_, applyErr := repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
			MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
			ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: feedbackType,
		})
		require.NoError(t, applyErr)
	}
	apply(types.FeedbackTypeLike)
	for _, id := range []string{chunkA.ID, chunkB.ID, chunkC.ID} {
		got := loadFeedbackChunk(t, db, id)
		assert.EqualValues(t, 1, got.LikeCount)
		assert.Zero(t, got.DislikeCount)
	}

	chunkRepo := &chunkRepository{db: db}
	require.NoError(t, chunkRepo.DeleteChunk(ctx, chunkB.TenantID, chunkB.ID))

	var deletedRefCount int64
	require.NoError(t, db.Model(&types.MessageChunkReference{}).
		Where("chunk_id = ?", chunkB.ID).Count(&deletedRefCount).Error)
	assert.Zero(t, deletedRefCount)

	apply(types.FeedbackTypeDislike)
	for _, id := range []string{chunkA.ID, chunkC.ID} {
		got := loadFeedbackChunk(t, db, id)
		assert.Zero(t, got.LikeCount)
		assert.EqualValues(t, 1, got.DislikeCount)
		assert.Equal(t, 0.8, got.RecallWeight)
	}

	apply(types.FeedbackTypeNone)
	for _, id := range []string{chunkA.ID, chunkC.ID} {
		got := loadFeedbackChunk(t, db, id)
		assert.Zero(t, got.LikeCount)
		assert.Zero(t, got.DislikeCount)
		assert.Equal(t, 1.0, got.RecallWeight)
	}

	var deleted types.Chunk
	require.NoError(t, db.Unscoped().First(&deleted, "id = ?", chunkB.ID).Error)
	assert.True(t, deleted.DeletedAt.Valid)
	assert.EqualValues(t, 1, deleted.LikeCount, "a deleted chunk must never be restored or recomputed")
	assert.Zero(t, deleted.DislikeCount)
	assert.Equal(t, 1.2, deleted.RecallWeight)
}

func TestLegacyMissingReferenceDoesNotBlockRemainingFeedback(t *testing.T) {
	repo, db, session, message, chunkA := setupFeedbackTestRepository(t)
	ctx := context.Background()
	chunkB := &types.Chunk{
		ID: "chunk-b", TenantID: chunkA.TenantID, KnowledgeBaseID: chunkA.KnowledgeBaseID,
		KnowledgeID: chunkA.KnowledgeID, Content: "b", SourceContent: "b",
		RecallWeight: 1, IsEnabled: true,
	}
	require.NoError(t, db.Create(chunkB).Error)
	_, err := repo.CompleteAssistantMessageWithReferences(
		ctx, session.TenantID, message, feedbackReferences(chunkA, chunkB),
	)
	require.NoError(t, err)

	// Simulate a pre-fix soft delete that left its attribution row behind.
	require.NoError(t, db.Where("tenant_id = ? AND id = ?", chunkB.TenantID, chunkB.ID).
		Delete(&types.Chunk{}).Error)

	_, err = repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: types.FeedbackTypeLike,
	})
	require.NoError(t, err)

	got := loadFeedbackChunk(t, db, chunkA.ID)
	assert.EqualValues(t, 1, got.LikeCount)
	var staleCount int64
	require.NoError(t, db.Model(&types.MessageChunkReference{}).
		Where("chunk_id = ?", chunkB.ID).Count(&staleCount).Error)
	assert.Zero(t, staleCount)
}

func TestAllLegacyReferencedChunksMissingReturnsBusinessErrorAndCleansReferences(t *testing.T) {
	repo, db, session, message, chunk := setupFeedbackTestRepository(t)
	ctx := context.Background()
	_, err := repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)
	// Simulate a pre-fix soft delete that left its attribution row behind.
	require.NoError(t, db.Where("tenant_id = ? AND id = ?", chunk.TenantID, chunk.ID).
		Delete(&types.Chunk{}).Error)

	_, err = repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: types.FeedbackTypeLike,
	})
	assert.ErrorIs(t, err, ErrFeedbackNotEligible)

	var feedbackCount, referenceCount, auditCount int64
	require.NoError(t, db.Model(&types.MessageFeedback{}).Count(&feedbackCount).Error)
	require.NoError(t, db.Model(&types.MessageChunkReference{}).Count(&referenceCount).Error)
	require.NoError(t, db.Model(&types.ChunkFeedbackAudit{}).Count(&auditCount).Error)
	assert.Zero(t, feedbackCount)
	assert.Zero(t, referenceCount)
	assert.Zero(t, auditCount)
}

func TestLegacyReferenceCleanupDatabaseErrorIsNotBusinessError(t *testing.T) {
	repo, db, session, message, chunk := setupFeedbackTestRepository(t)
	ctx := context.Background()
	_, err := repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)
	require.NoError(t, db.Where("tenant_id = ? AND id = ?", chunk.TenantID, chunk.ID).
		Delete(&types.Chunk{}).Error)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_legacy_reference_cleanup
		BEFORE DELETE ON message_chunk_references
		BEGIN SELECT RAISE(ABORT, 'legacy reference cleanup failure'); END;
	`).Error)

	_, err = repo.ApplyMessageFeedback(ctx, types.ApplyMessageFeedbackInput{
		MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
		ActorUserID: "user-a", SessionID: session.ID, MessageID: message.ID, Type: types.FeedbackTypeLike,
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrFeedbackNotEligible)

	var feedbackCount, referenceCount int64
	require.NoError(t, db.Model(&types.MessageFeedback{}).Count(&feedbackCount).Error)
	require.NoError(t, db.Model(&types.MessageChunkReference{}).Count(&referenceCount).Error)
	assert.Zero(t, feedbackCount)
	assert.EqualValues(t, 1, referenceCount)
}

func TestChunkDeleteRollsBackWhenReferenceCleanupFails(t *testing.T) {
	repo, db, session, message, chunk := setupFeedbackTestRepository(t)
	ctx := context.Background()
	_, err := repo.CompleteAssistantMessageWithReferences(ctx, session.TenantID, message, feedbackReference(chunk))
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_reference_cleanup
		BEFORE DELETE ON message_chunk_references
		BEGIN SELECT RAISE(ABORT, 'reference cleanup failure'); END;
	`).Error)

	err = (&chunkRepository{db: db}).DeleteChunk(ctx, chunk.TenantID, chunk.ID)
	require.Error(t, err)

	var activeCount, referenceCount int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", chunk.ID).Count(&activeCount).Error)
	require.NoError(t, db.Model(&types.MessageChunkReference{}).
		Where("chunk_id = ?", chunk.ID).Count(&referenceCount).Error)
	assert.EqualValues(t, 1, activeCount)
	assert.EqualValues(t, 1, referenceCount)
}

func TestRepeatedBatchChunkDeleteIsIdempotent(t *testing.T) {
	repo, db, session, message, chunkA := setupFeedbackTestRepository(t)
	ctx := context.Background()
	chunkB := &types.Chunk{
		ID: "chunk-b", TenantID: chunkA.TenantID, KnowledgeBaseID: chunkA.KnowledgeBaseID,
		KnowledgeID: chunkA.KnowledgeID, Content: "b", SourceContent: "b",
		RecallWeight: 1, IsEnabled: true,
	}
	require.NoError(t, db.Create(chunkB).Error)
	_, err := repo.CompleteAssistantMessageWithReferences(
		ctx, session.TenantID, message, feedbackReferences(chunkA, chunkB),
	)
	require.NoError(t, err)

	chunkRepo := &chunkRepository{db: db}
	require.NoError(t, chunkRepo.DeleteChunks(ctx, chunkA.TenantID, []string{chunkB.ID, chunkB.ID}))
	require.NoError(t, chunkRepo.DeleteChunks(ctx, chunkA.TenantID, []string{chunkB.ID}))

	var activeCount, referenceCount int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", chunkB.ID).Count(&activeCount).Error)
	require.NoError(t, db.Model(&types.MessageChunkReference{}).
		Where("chunk_id = ?", chunkB.ID).Count(&referenceCount).Error)
	assert.Zero(t, activeCount)
	assert.Zero(t, referenceCount)
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
