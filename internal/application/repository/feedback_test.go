package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	if err := db.Exec(`CREATE TABLE knowledges (
		id TEXT PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		knowledge_base_id TEXT NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create knowledges table: %v", err)
	}
	if err := db.AutoMigrate(
		&types.Chunk{},
		&types.MessageFeedback{},
		&types.MessageChunkReference{},
		&types.ChunkFeedbackWeightLog{},
	); err != nil {
		t.Fatalf("migrate feedback tables: %v", err)
	}

	message := &types.Message{
		ID: "message-1", SessionID: "session-1", Role: "assistant", Content: "answer", IsCompleted: true,
	}
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
	if got.LikeCount != 1 || got.DislikeCount != 0 || got.PositiveRate == nil ||
		*got.PositiveRate != 1 || got.RecallWeight != 1.2 {
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
	if got.LikeCount != 0 || got.DislikeCount != 1 || got.PositiveRate == nil ||
		*got.PositiveRate != 0 || got.RecallWeight != 0.8 || !got.NeedsOptimization {
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
	if got.LikeCount != 0 || got.DislikeCount != 0 || got.PositiveRate != nil ||
		got.RecallWeight != 1 || got.NeedsOptimization {
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

func TestFeedbackRepositoryResetRollsBackWhenWeightLogFails(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	cfg := types.DefaultChunkFeedbackConfig()
	if _, err := repo.ApplyMessageFeedback(
		context.Background(), feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonIncorrect), cfg,
	); err != nil {
		t.Fatalf("create dislike: %v", err)
	}
	before := loadFeedbackChunk(t, db, chunk.ID)
	if err := db.Migrator().DropTable(&types.ChunkFeedbackWeightLog{}); err != nil {
		t.Fatalf("drop log table: %v", err)
	}
	if _, err := repo.ResetChunkFeedback(
		context.Background(), chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID, "reset", cfg,
	); err == nil {
		t.Fatal("expected reset log insert failure")
	}
	after := loadFeedbackChunk(t, db, chunk.ID)
	if after.LikeCount != before.LikeCount || after.DislikeCount != before.DislikeCount ||
		after.RecallWeight != before.RecallWeight || after.FeedbackResetAt != nil {
		t.Fatalf("reset transaction did not roll back: before=%#v after=%#v", before, after)
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

func TestFeedbackRepositoryGovernanceListFiltersSortsAndPaginates(t *testing.T) {
	db, repo, _, unrated := setupFeedbackRepositoryTest(t)
	ctx := context.Background()
	now := time.Now().UTC()
	rows := []*types.Chunk{
		{
			ID: "chunk-high", TenantID: unrated.TenantID, KnowledgeBaseID: unrated.KnowledgeBaseID,
			KnowledgeID: "knowledge-high", Content: "high quality content", ChunkIndex: 3,
			LikeCount: 4, DislikeCount: 1, PositiveRate: float64Pointer(0.8), RecallWeight: 1.2,
			FeedbackUpdatedAt: timePointer(now.Add(3 * time.Minute)),
		},
		{
			ID: "chunk-normal", TenantID: unrated.TenantID, KnowledgeBaseID: unrated.KnowledgeBaseID,
			KnowledgeID: "knowledge-normal", Content: "normal content", ChunkIndex: 2,
			LikeCount: 1, DislikeCount: 1, PositiveRate: float64Pointer(0.5), RecallWeight: 1,
			FeedbackUpdatedAt: timePointer(now.Add(2 * time.Minute)),
		},
		{
			ID: "chunk-low", TenantID: unrated.TenantID, KnowledgeBaseID: unrated.KnowledgeBaseID,
			KnowledgeID: "knowledge-low", Content: "  " + strings.Repeat("界", 205) + " trailing", ChunkIndex: 1,
			LikeCount: 19, DislikeCount: 21, PositiveRate: float64Pointer(0.475), RecallWeight: 0.8,
			NeedsOptimization: true, FeedbackUpdatedAt: timePointer(now.Add(time.Minute)),
		},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create %s: %v", row.ID, err)
		}
	}
	knowledgeRows := []struct{ id, title string }{
		{unrated.KnowledgeID, "Unrated guide"},
		{"knowledge-high", "High guide"},
		{"knowledge-normal", "Normal guide"},
		{"knowledge-low", "Low governance handbook"},
	}
	for _, row := range knowledgeRows {
		if err := db.Exec(
			"INSERT INTO knowledges (id, tenant_id, knowledge_base_id, title) VALUES (?, ?, ?, ?)",
			row.id, unrated.TenantID, unrated.KnowledgeBaseID, row.title,
		).Error; err != nil {
			t.Fatalf("create knowledge %s: %v", row.id, err)
		}
	}

	statusCases := map[string]string{
		types.ChunkFeedbackStatusHigh:    "chunk-high",
		types.ChunkFeedbackStatusNormal:  "chunk-normal",
		types.ChunkFeedbackStatusLow:     "chunk-low",
		types.ChunkFeedbackStatusUnrated: unrated.ID,
	}
	for status, wantID := range statusCases {
		t.Run(status, func(t *testing.T) {
			query := &types.ChunkFeedbackListQuery{FeedbackStatus: status}
			if err := query.Validate(); err != nil {
				t.Fatal(err)
			}
			items, total, err := repo.ListChunkFeedback(
				ctx, unrated.TenantID, unrated.KnowledgeBaseID, query, types.DefaultChunkFeedbackConfig(),
			)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if total != 1 || len(items) != 1 || items[0].ChunkID != wantID {
				t.Fatalf("status %s returned total=%d items=%#v", status, total, items)
			}
		})
	}
	rated := &types.ChunkFeedbackListQuery{FeedbackStatus: types.ChunkFeedbackStatusRated}
	if err := rated.Validate(); err != nil {
		t.Fatal(err)
	}
	ratedItems, ratedTotal, err := repo.ListChunkFeedback(
		ctx, unrated.TenantID, unrated.KnowledgeBaseID, rated, types.DefaultChunkFeedbackConfig(),
	)
	if err != nil {
		t.Fatalf("rated list: %v", err)
	}
	if ratedTotal != 3 || len(ratedItems) != 3 || ratedItems[0].ChunkID != "chunk-high" ||
		ratedItems[1].ChunkID != "chunk-normal" || ratedItems[2].ChunkID != "chunk-low" {
		t.Fatalf("rated list total=%d items=%#v", ratedTotal, ratedItems)
	}

	needsOptimization := true
	query := &types.ChunkFeedbackListQuery{
		Keyword: "governance", NeedsOptimization: &needsOptimization,
		SortBy: "positive_rate", SortOrder: "asc", Page: 1, PageSize: 20,
	}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	items, total, err := repo.ListChunkFeedback(
		ctx, unrated.TenantID, unrated.KnowledgeBaseID, query, types.DefaultChunkFeedbackConfig(),
	)
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ChunkID != "chunk-low" ||
		items[0].KnowledgeTitle != "Low governance handbook" {
		t.Fatalf("filtered list total=%d items=%#v", total, items)
	}
	if items[0].ContentPreview != strings.Repeat("界", 200) ||
		len([]rune(items[0].ContentPreview)) != 200 || items[0].Content != "" {
		t.Fatalf("list projection loaded unexpected content: %#v", items[0])
	}

	query = &types.ChunkFeedbackListQuery{
		SortBy: "positive_rate", SortOrder: "asc", Page: 2, PageSize: 2,
	}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	items, total, err = repo.ListChunkFeedback(
		ctx, unrated.TenantID, unrated.KnowledgeBaseID, query, types.DefaultChunkFeedbackConfig(),
	)
	if err != nil {
		t.Fatalf("paged list: %v", err)
	}
	if total != 4 || len(items) != 2 || items[0].ChunkID != "chunk-high" || items[1].ChunkID != unrated.ID {
		t.Fatalf("NULL-last page total=%d items=%#v", total, items)
	}

	query = &types.ChunkFeedbackListQuery{}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	items, total, err = repo.ListChunkFeedback(
		ctx, unrated.TenantID+1, unrated.KnowledgeBaseID, query, types.DefaultChunkFeedbackConfig(),
	)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("cross-tenant list leaked rows: total=%d items=%#v err=%v", total, items, err)
	}
}

func TestFeedbackRepositoryGovernanceDetailLogsAndResetBaseline(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	ctx := context.Background()
	cfg := types.DefaultChunkFeedbackConfig()
	if err := db.Exec(
		"INSERT INTO knowledges (id, tenant_id, knowledge_base_id, title) VALUES (?, ?, ?, ?)",
		chunk.KnowledgeID, chunk.TenantID, chunk.KnowledgeBaseID, "Reset handbook",
	).Error; err != nil {
		t.Fatalf("create knowledge: %v", err)
	}
	mutation := feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonIncorrect)
	feedback, err := repo.ApplyMessageFeedback(ctx, mutation, cfg)
	if err != nil {
		t.Fatalf("create dislike: %v", err)
	}
	detail, err := repo.GetChunkFeedbackDetail(ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.DislikeCount != 1 || detail.SessionCount != 1 || len(detail.ReasonCounts) != 1 ||
		detail.ReasonCounts[0].ReasonCode != types.FeedbackReasonIncorrect || detail.ReasonCounts[0].Count != 1 {
		t.Fatalf("detail before reset = %#v", detail)
	}

	resetDetail, err := repo.ResetChunkFeedback(
		ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID, "content corrected", cfg,
	)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if resetDetail.LikeCount != 0 || resetDetail.DislikeCount != 0 ||
		resetDetail.SessionCount != 1 || len(resetDetail.ReasonCounts) != 0 {
		t.Fatalf("reset snapshot = %#v", resetDetail)
	}
	resetChunk := loadFeedbackChunk(t, db, chunk.ID)
	if resetChunk.LikeCount != 0 || resetChunk.DislikeCount != 0 || resetChunk.PositiveRate != nil ||
		resetChunk.RecallWeight != cfg.NormalRecallWeight || resetChunk.NeedsOptimization ||
		resetChunk.FeedbackResetAt == nil {
		t.Fatalf("chunk after reset = %#v", resetChunk)
	}
	var feedbackCount, referenceCount int64
	if err := db.Model(&types.MessageFeedback{}).Count(&feedbackCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&types.MessageChunkReference{}).Count(&referenceCount).Error; err != nil {
		t.Fatal(err)
	}
	if feedbackCount != 1 || referenceCount != 1 {
		t.Fatalf("reset deleted raw data: feedbacks=%d references=%d", feedbackCount, referenceCount)
	}
	firstResetAt := *resetChunk.FeedbackResetAt
	secondDetail, err := repo.ResetChunkFeedback(
		ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID, "verified again", cfg,
	)
	if err != nil {
		t.Fatalf("second reset: %v", err)
	}
	resetChunk = loadFeedbackChunk(t, db, chunk.ID)
	if resetChunk.FeedbackResetAt == nil || !resetChunk.FeedbackResetAt.After(firstResetAt) ||
		secondDetail.SessionCount != 1 || len(secondDetail.ReasonCounts) != 0 {
		t.Fatalf("second reset was not monotonic: first=%v chunk=%#v detail=%#v", firstResetAt, resetChunk, secondDetail)
	}
	if err := db.Model(&types.MessageFeedback{}).Count(&feedbackCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&types.MessageChunkReference{}).Count(&referenceCount).Error; err != nil {
		t.Fatal(err)
	}
	if feedbackCount != 1 || referenceCount != 1 {
		t.Fatalf("second reset deleted raw data: feedbacks=%d references=%d", feedbackCount, referenceCount)
	}
	detail, err = repo.GetChunkFeedbackDetail(ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID)
	if err != nil {
		t.Fatalf("detail after reset: %v", err)
	}
	if detail.SessionCount != 1 || len(detail.ReasonCounts) != 0 {
		t.Fatalf("pre-reset data remained active: %#v", detail)
	}
	logs, total, err := repo.ListChunkFeedbackWeightLogs(
		ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID, &types.Pagination{Page: 1, PageSize: 20},
	)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if total != 3 || len(logs) != 3 || logs[0].Source != types.ChunkFeedbackLogSourceAdminReset ||
		logs[0].SourceAction != types.ChunkFeedbackLogActionReset || logs[0].Reason != "verified again" ||
		logs[1].Source != types.ChunkFeedbackLogSourceAdminReset || logs[1].Reason != "content corrected" {
		t.Fatalf("logs after reset total=%d logs=%#v", total, logs)
	}

	reasonOnly := feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonOther)
	reasonOnly.ReasonText = "clarified"
	updated, err := repo.ApplyMessageFeedback(ctx, reasonOnly, cfg)
	if err != nil {
		t.Fatalf("reason-only update: %v", err)
	}
	if !updated.FeedbackAt.Equal(feedback.FeedbackAt) {
		t.Fatalf("reason-only update changed baseline time: %v != %v", updated.FeedbackAt, feedback.FeedbackAt)
	}
	if got := loadFeedbackChunk(t, db, chunk.ID); got.LikeCount != 0 || got.DislikeCount != 0 {
		t.Fatalf("old feedback revived after reset: %#v", got)
	}

	liked, err := repo.ApplyMessageFeedback(ctx, feedbackMutation(message, types.FeedbackTypeLike, ""), cfg)
	if err != nil {
		t.Fatalf("new transition: %v", err)
	}
	if !liked.FeedbackAt.After(*resetChunk.FeedbackResetAt) {
		t.Fatalf("new feedback %v is not after reset %v", liked.FeedbackAt, *resetChunk.FeedbackResetAt)
	}
	if got := loadFeedbackChunk(t, db, chunk.ID); got.LikeCount != 1 ||
		got.DislikeCount != 0 || got.RecallWeight != cfg.HighRecallWeight {
		t.Fatalf("new feedback not counted: %#v", got)
	}

	if _, err := repo.ResetChunkFeedback(
		ctx, chunk.TenantID, "other-kb", chunk.ID, "", cfg,
	); !errors.Is(err, types.ErrChunkFeedbackNotFound) {
		t.Fatalf("cross-KB reset error = %v", err)
	}
}

func TestFeedbackRepositoryGovernanceJoinsFeedbackBySession(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	ctx := context.Background()
	if err := db.Exec(
		"INSERT INTO knowledges (id, tenant_id, knowledge_base_id, title) VALUES (?, ?, ?, ?)",
		chunk.KnowledgeID, chunk.TenantID, chunk.KnowledgeBaseID, "Session-safe handbook",
	).Error; err != nil {
		t.Fatalf("create knowledge: %v", err)
	}
	if _, err := repo.ApplyMessageFeedback(
		ctx,
		feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonIncorrect),
		types.DefaultChunkFeedbackConfig(),
	); err != nil {
		t.Fatalf("create target feedback: %v", err)
	}
	if err := db.Create(&types.MessageFeedback{
		SessionTenantID: 1,
		UserID:          "other-user",
		SessionID:       "other-session",
		MessageID:       message.ID,
		FeedbackType:    types.FeedbackTypeDislike,
		ReasonCode:      types.FeedbackReasonOutdated,
		FeedbackAt:      time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create unrelated feedback: %v", err)
	}

	detail, err := repo.GetChunkFeedbackDetail(ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.SessionCount != 1 || len(detail.ReasonCounts) != 1 ||
		detail.ReasonCounts[0].ReasonCode != types.FeedbackReasonIncorrect || detail.ReasonCounts[0].Count != 1 {
		t.Fatalf("cross-session feedback joined into governance data: %#v", detail)
	}
	query := &types.ChunkFeedbackListQuery{}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	items, total, err := repo.ListChunkFeedback(
		ctx, chunk.TenantID, chunk.KnowledgeBaseID, query, types.DefaultChunkFeedbackConfig(),
	)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].SessionCount != 1 {
		t.Fatalf("cross-session feedback joined into list: total=%d items=%#v", total, items)
	}
	if _, err := repo.ApplyMessageFeedback(
		ctx,
		feedbackMutation(message, types.FeedbackTypeLike, ""),
		types.DefaultChunkFeedbackConfig(),
	); err != nil {
		t.Fatalf("switch target feedback: %v", err)
	}
	got := loadFeedbackChunk(t, db, chunk.ID)
	if got.LikeCount != 1 || got.DislikeCount != 0 {
		t.Fatalf("cross-session feedback joined into aggregate: %#v", got)
	}
}

func TestFeedbackRepositoryResetBaselineCoversFutureFeedback(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	ctx := context.Background()
	cfg := types.DefaultChunkFeedbackConfig()
	if _, err := repo.ApplyMessageFeedback(
		ctx,
		feedbackMutation(message, types.FeedbackTypeDislike, types.FeedbackReasonIncorrect),
		cfg,
	); err != nil {
		t.Fatalf("create dislike: %v", err)
	}
	future := time.Now().UTC().Add(10*time.Minute + 789*time.Nanosecond)
	if err := db.Model(&types.MessageFeedback{}).
		Where("message_id = ?", message.ID).
		Update("feedback_at", future).Error; err != nil {
		t.Fatalf("move feedback into future: %v", err)
	}

	detail, err := repo.ResetChunkFeedback(ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID, "clock skew", cfg)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	resetChunk := loadFeedbackChunk(t, db, chunk.ID)
	if resetChunk.FeedbackResetAt == nil || !resetChunk.FeedbackResetAt.After(future) {
		t.Fatalf("reset baseline %v did not cover future feedback %v", resetChunk.FeedbackResetAt, future)
	}
	if detail.SessionCount != 1 || len(detail.ReasonCounts) != 0 {
		t.Fatalf("future feedback remained active in reset snapshot: %#v", detail)
	}
	after, err := repo.GetChunkFeedbackDetail(ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID)
	if err != nil {
		t.Fatalf("detail after reset: %v", err)
	}
	if after.SessionCount != 1 || len(after.ReasonCounts) != 0 {
		t.Fatalf("future feedback remained active after reset: %#v", after)
	}
}

func TestFeedbackRepositoryGovernanceSessionCountIncludesUnratedReferences(t *testing.T) {
	db, repo, message, chunk := setupFeedbackRepositoryTest(t)
	ctx := context.Background()
	query := &types.ChunkFeedbackListQuery{FeedbackStatus: types.ChunkFeedbackStatusUnrated}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&types.MessageChunkReference{
		SessionTenantID: 1,
		ChunkTenantID:   chunk.TenantID,
		SessionID:       message.SessionID,
		MessageID:       "message-2",
		ChunkID:         chunk.ID,
		KnowledgeBaseID: chunk.KnowledgeBaseID,
		KnowledgeID:     chunk.KnowledgeID,
	}).Error; err != nil {
		t.Fatalf("create second reference in same session: %v", err)
	}

	items, total, err := repo.ListChunkFeedback(
		ctx, chunk.TenantID, chunk.KnowledgeBaseID, query, types.DefaultChunkFeedbackConfig(),
	)
	if err != nil {
		t.Fatalf("list unrated governance: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].SessionCount != 1 {
		t.Fatalf("unrated list total=%d items=%#v", total, items)
	}

	detail, err := repo.GetChunkFeedbackDetail(ctx, chunk.TenantID, chunk.KnowledgeBaseID, chunk.ID)
	if err != nil {
		t.Fatalf("unrated detail: %v", err)
	}
	if detail.SessionCount != 1 || detail.LikeCount != 0 || detail.DislikeCount != 0 {
		t.Fatalf("unrated detail=%#v", detail)
	}
}

func float64Pointer(value float64) *float64 { return &value }

func timePointer(value time.Time) *time.Time { return &value }
