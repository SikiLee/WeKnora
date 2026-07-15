package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

func setupFeedbackRepositoryTest(t *testing.T) (*gorm.DB, *feedbackRepository, *types.Message, *types.Chunk) {
	t.Helper()
	dsn := fmt.Sprintf("file:feedback-repo-%d?mode=memory&cache=shared&_busy_timeout=5000", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	// Lite mode uses the same single-writer connection policy in container.go.
	// PostgreSQL lock contention is covered by feedback_postgres_test.go.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`CREATE TABLE messages (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		is_completed BOOLEAN NOT NULL DEFAULT 0,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create messages table: %v", err)
	}
	if err := db.AutoMigrate(
		&types.Chunk{},
		&types.MessageFeedback{},
		&types.MessageChunkReference{},
		&types.ChunkFeedbackWeightLog{},
	); err != nil {
		t.Fatalf("migrate feedback tables: %v", err)
	}

	message := &types.Message{ID: "message-1", SessionID: "session-1", Role: "assistant", Content: "answer", IsCompleted: true}
	if err := db.Exec(
		"INSERT INTO messages (id, session_id, role, is_completed) VALUES (?, ?, ?, ?)",
		message.ID, message.SessionID, message.Role, message.IsCompleted,
	).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	chunk := &types.Chunk{
		ID:              "chunk-1",
		TenantID:        9,
		KnowledgeBaseID: "kb-1",
		KnowledgeID:     "knowledge-1",
		Content:         "chunk",
		RecallWeight:    1,
	}
	if err := db.Create(chunk).Error; err != nil {
		t.Fatalf("create chunk: %v", err)
	}
	ref := &types.MessageChunkReference{
		SessionTenantID: 1,
		ChunkTenantID:   chunk.TenantID,
		SessionID:       message.SessionID,
		MessageID:       message.ID,
		ChunkID:         chunk.ID,
		KnowledgeBaseID: chunk.KnowledgeBaseID,
		KnowledgeID:     chunk.KnowledgeID,
	}
	if err := db.Create(ref).Error; err != nil {
		t.Fatalf("create reference: %v", err)
	}
	return db, &feedbackRepository{db: db}, message, chunk
}

func feedbackMutation(message *types.Message, feedbackType, reasonCode string) types.MessageFeedbackMutation {
	return types.MessageFeedbackMutation{
		SessionTenantID: 1,
		UserID:          "user-1",
		SessionID:       message.SessionID,
		MessageID:       message.ID,
		FeedbackType:    feedbackType,
		ReasonCode:      reasonCode,
	}
}

func loadFeedbackChunk(t *testing.T, db *gorm.DB, id string) types.Chunk {
	t.Helper()
	var chunk types.Chunk
	if err := db.Where("id = ?", id).First(&chunk).Error; err != nil {
		t.Fatalf("load chunk: %v", err)
	}
	return chunk
}

func countWeightLogs(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&types.ChunkFeedbackWeightLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count logs: %v", err)
	}
	return count
}

func TestFeedbackRepositoryStateMachineAndIdempotency(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	ctx := context.Background()
	cfg := types.DefaultChunkFeedbackConfig()

	liked, err := repo.ApplyMessageFeedback(ctx, feedbackMutation(message, types.FeedbackTypeLike, ""), cfg)
	if err != nil {
		t.Fatalf("like: %v", err)
	}
	if liked == nil || liked.FeedbackType != types.FeedbackTypeLike {
		t.Fatalf("liked feedback = %#v", liked)
	}
	got := loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount != 1 || got.DislikeCount != 0 || got.PositiveRate == nil || *got.PositiveRate != 1 || got.RecallWeight != 1.2 {
		t.Fatalf("chunk after like = %#v", got)
	}
	if count := countWeightLogs(t, db); count != 1 {
		t.Fatalf("weight logs after like = %d, want 1", count)
	}
	var likeLog types.ChunkFeedbackWeightLog
	if err := db.Where("source_action = ?", types.ChunkFeedbackLogActionLike).First(&likeLog).Error; err != nil {
		t.Fatalf("load like weight log: %v", err)
	}
	if likeLog.ChunkTenantID != chunk.TenantID || likeLog.ChunkID != chunk.ID ||
		likeLog.Source != types.ChunkFeedbackLogSourceUserFeedback ||
		likeLog.SourceMessageID != message.ID || likeLog.SourceFeedbackID != liked.ID ||
		likeLog.OldWeight != 1 || likeLog.NewWeight != 1.2 || likeLog.Reason != "" {
		t.Fatalf("like weight log = %#v", likeLog)
	}

	if _, err := repo.ApplyMessageFeedback(ctx, feedbackMutation(message, types.FeedbackTypeLike, ""), cfg); err != nil {
		t.Fatalf("repeat like: %v", err)
	}
	got = loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount != 1 || got.DislikeCount != 0 || countWeightLogs(t, db) != 1 {
		t.Fatalf("repeat like drifted aggregate: %#v", got)
	}

	disliked, err := repo.ApplyMessageFeedback(
		ctx, feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonIncorrect), cfg,
	)
	if err != nil {
		t.Fatalf("switch to dislike: %v", err)
	}
	feedbackAt := disliked.FeedbackAt
	got = loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount != 0 || got.DislikeCount != 1 || got.PositiveRate == nil || *got.PositiveRate != 0 || got.RecallWeight != 0.8 || !got.NeedsOptimization {
		t.Fatalf("chunk after dislike = %#v", got)
	}
	if countWeightLogs(t, db) != 2 {
		t.Fatal("switch should create one additional weight log")
	}
	var dislikeLog types.ChunkFeedbackWeightLog
	if err := db.Where("source_action = ?", types.ChunkFeedbackLogActionDislike).First(&dislikeLog).Error; err != nil {
		t.Fatalf("load dislike weight log: %v", err)
	}
	if dislikeLog.Source != types.ChunkFeedbackLogSourceUserFeedback ||
		dislikeLog.SourceMessageID != message.ID || dislikeLog.SourceFeedbackID != disliked.ID ||
		dislikeLog.OldWeight != 1.2 || dislikeLog.NewWeight != 0.8 ||
		dislikeLog.Reason != types.FeedbackReasonIncorrect {
		t.Fatalf("dislike weight log = %#v", dislikeLog)
	}

	reasonUpdate := feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonOther)
	reasonUpdate.ReasonText = "specific detail"
	updated, err := repo.ApplyMessageFeedback(ctx, reasonUpdate, cfg)
	if err != nil {
		t.Fatalf("update reason: %v", err)
	}
	if !updated.FeedbackAt.Equal(feedbackAt) {
		t.Fatalf("reason-only update changed feedback baseline timestamp: got %v want %v", updated.FeedbackAt, feedbackAt)
	}
	if countWeightLogs(t, db) != 2 {
		t.Fatal("reason-only update should not create a weight log")
	}

	cleared, err := repo.ApplyMessageFeedback(ctx, feedbackMutation(message, types.FeedbackTypeNone, ""), cfg)
	if err != nil {
		t.Fatalf("cancel feedback: %v", err)
	}
	if cleared != nil {
		t.Fatalf("cancel result = %#v, want nil", cleared)
	}
	got = loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount != 0 || got.DislikeCount != 0 || got.PositiveRate != nil || got.RecallWeight != 1 || got.NeedsOptimization {
		t.Fatalf("chunk after cancel = %#v", got)
	}
	if countWeightLogs(t, db) != 3 {
		t.Fatal("cancel should log the weight returning to normal")
	}
	if _, err := repo.ApplyMessageFeedback(ctx, feedbackMutation(message, types.FeedbackTypeNone, ""), cfg); err != nil {
		t.Fatalf("repeat cancel: %v", err)
	}
	if countWeightLogs(t, db) != 3 {
		t.Fatal("repeat cancel should be idempotent")
	}
}

func TestFeedbackRepositoryResetBaselineDoesNotReviveReasonUpdate(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	ctx := context.Background()
	cfg := types.DefaultChunkFeedbackConfig()
	dislike := feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonIncorrect)
	feedback, err := repo.ApplyMessageFeedback(ctx, dislike, cfg)
	if err != nil {
		t.Fatalf("create dislike: %v", err)
	}
	resetAt := feedback.FeedbackAt.Add(time.Nanosecond)
	if err := db.Model(&types.Chunk{}).Where("id = ?", chunk.ID).Updates(map[string]interface{}{
		"like_count":          0,
		"dislike_count":       0,
		"positive_rate":       nil,
		"recall_weight":       1.0,
		"needs_optimization":  false,
		"feedback_reset_at":   resetAt,
		"feedback_updated_at": resetAt,
	}).Error; err != nil {
		t.Fatalf("reset aggregate: %v", err)
	}

	reasonUpdate := feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonOther)
	reasonUpdate.ReasonText = "clarified"
	updated, err := repo.ApplyMessageFeedback(ctx, reasonUpdate, cfg)
	if err != nil {
		t.Fatalf("reason update after reset: %v", err)
	}
	if !updated.FeedbackAt.Equal(feedback.FeedbackAt) {
		t.Fatalf("old feedback timestamp revived: got %v want %v", updated.FeedbackAt, feedback.FeedbackAt)
	}
	got := loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount != 0 || got.DislikeCount != 0 || got.RecallWeight != 1 {
		t.Fatalf("old feedback revived after reset: %#v", got)
	}

	liked, err := repo.ApplyMessageFeedback(ctx, feedbackMutation(message, types.FeedbackTypeLike, ""), cfg)
	if err != nil {
		t.Fatalf("new transition after reset: %v", err)
	}
	if !liked.FeedbackAt.After(resetAt) {
		t.Fatalf("new transition timestamp %v is not after reset %v", liked.FeedbackAt, resetAt)
	}
	got = loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount != 1 || got.DislikeCount != 0 || got.RecallWeight != 1.2 {
		t.Fatalf("new feedback after reset not counted: %#v", got)
	}
}

func TestFeedbackMutationTimeIsStrictlyAfterEveryResetBaseline(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	firstReset := now
	latestReset := now.Add(2 * time.Second)
	got := feedbackMutationTime([]*types.Chunk{
		{FeedbackResetAt: &firstReset},
		nil,
		{FeedbackResetAt: &latestReset},
	}, now)
	if !got.After(firstReset) || !got.After(latestReset) {
		t.Fatalf("feedback time %v must be after reset baselines %v and %v", got, firstReset, latestReset)
	}
	if want := latestReset.Add(time.Microsecond); !got.Equal(want) {
		t.Fatalf("feedback time = %v, want %v", got, want)
	}

	subMicrosecondNow := latestReset.Add(100 * time.Nanosecond)
	got = feedbackMutationTime([]*types.Chunk{{FeedbackResetAt: &latestReset}}, subMicrosecondNow)
	if want := latestReset.Add(time.Microsecond); !got.Equal(want) {
		t.Fatalf("sub-microsecond feedback time = %v, want persisted value %v", got, want)
	}
}

func TestFeedbackRepositoryRollsBackWhenWeightLogFails(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	if err := db.Migrator().DropTable(&types.ChunkFeedbackWeightLog{}); err != nil {
		t.Fatalf("drop log table: %v", err)
	}
	_, err := repo.ApplyMessageFeedback(
		context.Background(), feedbackMutation(message, types.FeedbackTypeLike, ""), types.DefaultChunkFeedbackConfig(),
	)
	if err == nil {
		t.Fatal("expected log insert failure")
	}
	var feedbackCount int64
	if err := db.Model(&types.MessageFeedback{}).Count(&feedbackCount).Error; err != nil {
		t.Fatalf("count feedbacks: %v", err)
	}
	if feedbackCount != 0 {
		t.Fatalf("feedback transaction did not roll back: count=%d", feedbackCount)
	}
	got := loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount != 0 || got.DislikeCount != 0 || got.RecallWeight != 1 {
		t.Fatalf("chunk transaction did not roll back: %#v", got)
	}
}

func TestFeedbackRepositorySQLiteSerializedTransitionsDoNotDrift(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	ctx := context.Background()
	cfg := types.DefaultChunkFeedbackConfig()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		feedbackType := types.FeedbackTypeLike
		reason := ""
		if i%2 == 1 {
			feedbackType = types.FeedbackTypeDislike
			reason = types.FeedbackReasonIncorrect
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ApplyMessageFeedback(ctx, feedbackMutation(message, feedbackType, reason), cfg)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent transition: %v", err)
		}
	}

	var active types.MessageFeedback
	if err := db.First(&active).Error; err != nil {
		t.Fatalf("load active feedback: %v", err)
	}
	got := loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount+got.DislikeCount != 1 {
		t.Fatalf("aggregate drifted: %#v", got)
	}
	if active.FeedbackType == types.FeedbackTypeLike && (got.LikeCount != 1 || got.DislikeCount != 0) {
		t.Fatalf("like feedback disagrees with aggregate: feedback=%#v chunk=%#v", active, got)
	}
	if active.FeedbackType == types.FeedbackTypeDislike && (got.LikeCount != 0 || got.DislikeCount != 1) {
		t.Fatalf("dislike feedback disagrees with aggregate: feedback=%#v chunk=%#v", active, got)
	}
}
