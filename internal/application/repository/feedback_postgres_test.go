package repository

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

func postgresDSNWithSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("parse PostgreSQL DSN: %v", err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema
}

func setupFeedbackPostgresTest(t *testing.T) (*gorm.DB, *feedbackRepository) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("WEKNORA_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("WEKNORA_TEST_POSTGRES_DSN is not set")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL admin connection: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatalf("get PostgreSQL admin DB: %v", err)
	}
	schema := "feedback_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create PostgreSQL test schema: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		_ = adminSQL.Close()
	})

	db, err := gorm.Open(postgres.Open(postgresDSNWithSearchPath(t, dsn, schema)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open schema-scoped PostgreSQL connection: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get schema-scoped PostgreSQL DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(12)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.Exec(`CREATE TABLE chunks (
		id VARCHAR(36) PRIMARY KEY,
		tenant_id BIGINT NOT NULL,
		knowledge_base_id VARCHAR(36) NOT NULL,
		knowledge_id VARCHAR(36) NOT NULL,
		content TEXT NOT NULL DEFAULT '',
		chunk_index INTEGER NOT NULL DEFAULT 0,
		chunk_type VARCHAR(20) NOT NULL DEFAULT 'text',
		created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMP WITH TIME ZONE
	)`).Error; err != nil {
		t.Fatalf("create PostgreSQL chunks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE messages (
		id VARCHAR(36) PRIMARY KEY,
		session_id VARCHAR(36) NOT NULL,
		role VARCHAR(16) NOT NULL,
		is_completed BOOLEAN NOT NULL DEFAULT false,
		deleted_at TIMESTAMP WITH TIME ZONE
	)`).Error; err != nil {
		t.Fatalf("create PostgreSQL messages table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE knowledges (
		id VARCHAR(36) PRIMARY KEY,
		tenant_id BIGINT NOT NULL,
		knowledge_base_id VARCHAR(36) NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		deleted_at TIMESTAMP WITH TIME ZONE
	)`).Error; err != nil {
		t.Fatalf("create PostgreSQL knowledges table: %v", err)
	}
	migrationPath := filepath.Join("..", "..", "..", "migrations", "versioned", "000070_answer_feedback.up.sql")
	migration, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read feedback migration: %v", err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatalf("apply feedback migration: %v", err)
	}
	return db, &feedbackRepository{db: db}
}

func TestFeedbackRepositoryPostgresConcurrentTransactions(t *testing.T) {
	db, repo := setupFeedbackPostgresTest(t)
	const transactionCount = 10
	for _, chunkID := range []string{"chunk-a", "chunk-b"} {
		if err := db.Exec(`INSERT INTO chunks
			(id, tenant_id, knowledge_base_id, knowledge_id, content)
			VALUES (?, 9, 'kb-1', 'knowledge-1', 'content')`, chunkID).Error; err != nil {
			t.Fatalf("insert chunk %s: %v", chunkID, err)
		}
	}
	for index := 0; index < transactionCount; index++ {
		messageID := fmt.Sprintf("message-%d", index)
		sessionID := fmt.Sprintf("session-%d", index)
		if err := db.Exec(
			"INSERT INTO messages (id, session_id, role, is_completed) VALUES (?, ?, 'assistant', true)",
			messageID, sessionID,
		).Error; err != nil {
			t.Fatalf("insert message %d: %v", index, err)
		}
		chunkIDs := []string{"chunk-a", "chunk-b"}
		if index%2 == 1 {
			chunkIDs[0], chunkIDs[1] = chunkIDs[1], chunkIDs[0]
		}
		for rank, chunkID := range chunkIDs {
			if err := db.Create(&types.MessageChunkReference{
				SessionTenantID: 1,
				ChunkTenantID:   9,
				SessionID:       sessionID,
				MessageID:       messageID,
				ChunkID:         chunkID,
				KnowledgeBaseID: "kb-1",
				KnowledgeID:     "knowledge-1",
				ReferenceRank:   rank,
			}).Error; err != nil {
				t.Fatalf("insert reference %d/%s: %v", index, chunkID, err)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, transactionCount)
	var wg sync.WaitGroup
	for index := 0; index < transactionCount; index++ {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			feedbackType := types.FeedbackTypeLike
			reasonCode := ""
			if index%2 == 1 {
				feedbackType = types.FeedbackTypeDislike
				reasonCode = types.FeedbackReasonIncorrect
			}
			_, err := repo.ApplyMessageFeedback(ctx, types.MessageFeedbackMutation{
				SessionTenantID: 1,
				UserID:          fmt.Sprintf("user-%d", index),
				SessionID:       fmt.Sprintf("session-%d", index),
				MessageID:       fmt.Sprintf("message-%d", index),
				FeedbackType:    feedbackType,
				ReasonCode:      reasonCode,
			}, types.DefaultChunkFeedbackConfig())
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent PostgreSQL feedback transaction: %v", err)
		}
	}

	for _, chunkID := range []string{"chunk-a", "chunk-b"} {
		var chunk types.Chunk
		if err := db.Where("tenant_id = ? AND id = ?", 9, chunkID).First(&chunk).Error; err != nil {
			t.Fatalf("load chunk %s: %v", chunkID, err)
		}
		if chunk.LikeCount != 5 || chunk.DislikeCount != 5 || chunk.PositiveRate == nil ||
			*chunk.PositiveRate != 0.5 || chunk.RecallWeight != 1 {
			t.Fatalf("chunk %s aggregate drifted: %#v", chunkID, chunk)
		}
	}
}

func TestFeedbackRepositoryPostgresConcurrentSameMessageTransitions(t *testing.T) {
	db, repo := setupFeedbackPostgresTest(t)
	if err := db.Exec(`INSERT INTO chunks
		(id, tenant_id, knowledge_base_id, knowledge_id, content)
		VALUES ('chunk-1', 9, 'kb-1', 'knowledge-1', 'content')`).Error; err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
	if err := db.Exec(`INSERT INTO messages
		(id, session_id, role, is_completed)
		VALUES ('message-1', 'session-1', 'assistant', true)`).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if err := db.Create(&types.MessageChunkReference{
		SessionTenantID: 1,
		ChunkTenantID:   9,
		SessionID:       "session-1",
		MessageID:       "message-1",
		ChunkID:         "chunk-1",
		KnowledgeBaseID: "kb-1",
		KnowledgeID:     "knowledge-1",
	}).Error; err != nil {
		t.Fatalf("insert reference: %v", err)
	}

	const transactionCount = 20
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, transactionCount)
	var wg sync.WaitGroup
	for index := 0; index < transactionCount; index++ {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			feedbackType := types.FeedbackTypeLike
			reasonCode := ""
			switch index % 3 {
			case 1:
				feedbackType = types.FeedbackTypeDislike
				reasonCode = types.FeedbackReasonIncorrect
			case 2:
				feedbackType = types.FeedbackTypeNone
			}
			_, err := repo.ApplyMessageFeedback(ctx, types.MessageFeedbackMutation{
				SessionTenantID: 1,
				UserID:          "user-1",
				SessionID:       "session-1",
				MessageID:       "message-1",
				FeedbackType:    feedbackType,
				ReasonCode:      reasonCode,
			}, types.DefaultChunkFeedbackConfig())
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("same-message PostgreSQL transition: %v", err)
		}
	}

	if _, err := repo.ApplyMessageFeedback(ctx, types.MessageFeedbackMutation{
		SessionTenantID: 1,
		UserID:          "user-1",
		SessionID:       "session-1",
		MessageID:       "message-1",
		FeedbackType:    types.FeedbackTypeLike,
	}, types.DefaultChunkFeedbackConfig()); err != nil {
		t.Fatalf("deterministic final transition: %v", err)
	}
	var feedbackCount int64
	if err := db.Model(&types.MessageFeedback{}).Count(&feedbackCount).Error; err != nil {
		t.Fatalf("count feedback rows: %v", err)
	}
	if feedbackCount != 1 {
		t.Fatalf("feedback rows = %d, want 1", feedbackCount)
	}
	var chunk types.Chunk
	if err := db.Where("tenant_id = ? AND id = ?", 9, "chunk-1").First(&chunk).Error; err != nil {
		t.Fatalf("load chunk: %v", err)
	}
	if chunk.LikeCount != 1 || chunk.DislikeCount != 0 || chunk.RecallWeight != 1.2 {
		t.Fatalf("final same-message aggregate = %#v", chunk)
	}
}

func TestFeedbackRepositoryPostgresGovernanceQueriesAndReset(t *testing.T) {
	db, repo := setupFeedbackPostgresTest(t)
	ctx := context.Background()
	cfg := types.DefaultChunkFeedbackConfig()
	if err := db.Exec(`INSERT INTO knowledges
		(id, tenant_id, knowledge_base_id, title)
		VALUES ('knowledge-1', 9, 'kb-1', 'PostgreSQL handbook')`).Error; err != nil {
		t.Fatalf("insert knowledge: %v", err)
	}
	if err := db.Exec(`INSERT INTO chunks
		(id, tenant_id, knowledge_base_id, knowledge_id, content, chunk_index, chunk_type)
		VALUES ('chunk-1', 9, 'kb-1', 'knowledge-1', 'governance content', 4, 'text')`).Error; err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
	if err := db.Exec(`INSERT INTO messages
		(id, session_id, role, is_completed)
		VALUES ('message-1', 'session-1', 'assistant', true)`).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if err := db.Create(&types.MessageChunkReference{
		SessionTenantID: 1,
		ChunkTenantID:   9,
		SessionID:       "session-1",
		MessageID:       "message-1",
		ChunkID:         "chunk-1",
		KnowledgeBaseID: "kb-1",
		KnowledgeID:     "knowledge-1",
	}).Error; err != nil {
		t.Fatalf("insert reference: %v", err)
	}
	if _, err := repo.ApplyMessageFeedback(ctx, types.MessageFeedbackMutation{
		SessionTenantID: 1,
		UserID:          "user-1",
		SessionID:       "session-1",
		MessageID:       "message-1",
		FeedbackType:    types.FeedbackTypeDislike,
		ReasonCode:      types.FeedbackReasonIncorrect,
	}, cfg); err != nil {
		t.Fatalf("apply feedback: %v", err)
	}

	query := &types.ChunkFeedbackListQuery{FeedbackStatus: types.ChunkFeedbackStatusLow}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	items, total, err := repo.ListChunkFeedback(ctx, 9, "kb-1", query, cfg)
	if err != nil {
		t.Fatalf("list governance: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].SessionCount != 1 || items[0].KnowledgeTitle != "PostgreSQL handbook" {
		t.Fatalf("list total=%d items=%#v", total, items)
	}
	detail, err := repo.GetChunkFeedbackDetail(ctx, 9, "kb-1", "chunk-1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(detail.ReasonCounts) != 1 || detail.ReasonCounts[0].ReasonCode != types.FeedbackReasonIncorrect {
		t.Fatalf("detail reasons=%#v", detail.ReasonCounts)
	}

	futureFeedbackAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
	if err := db.Model(&types.MessageFeedback{}).
		Where("message_id = ?", "message-1").
		Update("feedback_at", futureFeedbackAt).Error; err != nil {
		t.Fatalf("move PostgreSQL feedback into future: %v", err)
	}
	if _, err := repo.ResetChunkFeedback(ctx, 9, "kb-1", "chunk-1", "fixed", cfg); err != nil {
		t.Fatalf("reset: %v", err)
	}
	var resetChunk types.Chunk
	if err := db.Where("tenant_id = ? AND id = ?", 9, "chunk-1").First(&resetChunk).Error; err != nil {
		t.Fatalf("load reset chunk: %v", err)
	}
	if resetChunk.FeedbackResetAt == nil || !resetChunk.FeedbackResetAt.After(futureFeedbackAt) {
		t.Fatalf("PostgreSQL reset baseline %v did not pass feedback %v", resetChunk.FeedbackResetAt, futureFeedbackAt)
	}
	detail, err = repo.GetChunkFeedbackDetail(ctx, 9, "kb-1", "chunk-1")
	if err != nil {
		t.Fatalf("detail after reset: %v", err)
	}
	if detail.LikeCount != 0 || detail.DislikeCount != 0 || detail.SessionCount != 0 || len(detail.ReasonCounts) != 0 {
		t.Fatalf("detail after reset=%#v", detail)
	}
	logs, logTotal, err := repo.ListChunkFeedbackWeightLogs(
		ctx, 9, "kb-1", "chunk-1", &types.Pagination{Page: 1, PageSize: 20},
	)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if logTotal != 2 || len(logs) != 2 || logs[0].Source != types.ChunkFeedbackLogSourceAdminReset {
		t.Fatalf("logs total=%d logs=%#v", logTotal, logs)
	}
	var rawFeedbacks int64
	if err := db.Model(&types.MessageFeedback{}).Count(&rawFeedbacks).Error; err != nil {
		t.Fatal(err)
	}
	if rawFeedbacks != 1 {
		t.Fatalf("reset removed raw feedback: %d", rawFeedbacks)
	}
}

func TestFeedbackRepositoryPostgresResetSerializesWithFeedback(t *testing.T) {
	db, repo := setupFeedbackPostgresTest(t)
	cfg := types.DefaultChunkFeedbackConfig()
	if err := db.Exec(`INSERT INTO chunks
		(id, tenant_id, knowledge_base_id, knowledge_id, content)
		VALUES ('chunk-1', 9, 'kb-1', 'knowledge-1', 'content')`).Error; err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
	if err := db.Exec(`INSERT INTO messages
		(id, session_id, role, is_completed)
		VALUES ('message-1', 'session-1', 'assistant', true)`).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if err := db.Create(&types.MessageChunkReference{
		SessionTenantID: 1, ChunkTenantID: 9, SessionID: "session-1", MessageID: "message-1",
		ChunkID: "chunk-1", KnowledgeBaseID: "kb-1", KnowledgeID: "knowledge-1",
	}).Error; err != nil {
		t.Fatalf("insert reference: %v", err)
	}
	baseMutation := types.MessageFeedbackMutation{
		SessionTenantID: 1, UserID: "user-1", SessionID: "session-1", MessageID: "message-1",
		FeedbackType: types.FeedbackTypeDislike, ReasonCode: types.FeedbackReasonIncorrect,
	}
	if _, err := repo.ApplyMessageFeedback(context.Background(), baseMutation, cfg); err != nil {
		t.Fatalf("initial dislike: %v", err)
	}
	if err := db.Exec(`
		CREATE FUNCTION pause_feedback_reset_update() RETURNS trigger AS $$
		BEGIN
			IF NEW.feedback_reset_at IS NOT NULL
				AND NEW.like_count = 0 AND NEW.dislike_count = 0 THEN
				PERFORM pg_sleep(4);
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql
	`).Error; err != nil {
		t.Fatalf("install reset pause function: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER pause_feedback_reset_update
			BEFORE UPDATE ON chunks
			FOR EACH ROW EXECUTE FUNCTION pause_feedback_reset_update()
	`).Error; err != nil {
		t.Fatalf("install reset pause trigger: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resetDone := make(chan error, 1)
	go func() {
		_, err := repo.ResetChunkFeedback(ctx, 9, "kb-1", "chunk-1", "concurrent reset", cfg)
		resetDone <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var sleeping int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_stat_activity
			WHERE wait_event = 'PgSleep' AND query LIKE 'UPDATE "chunks"%'`).Scan(&sleeping).Error; err != nil {
			t.Fatalf("inspect reset lock contention: %v", err)
		}
		if sleeping > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reset transaction never reached the deterministic pause trigger")
		}
		time.Sleep(20 * time.Millisecond)
	}

	feedbackDone := make(chan error, 1)
	go func() {
		like := baseMutation
		like.FeedbackType = types.FeedbackTypeLike
		like.ReasonCode = ""
		_, err := repo.ApplyMessageFeedback(ctx, like, cfg)
		feedbackDone <- err
	}()
	lockDeadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE 'SELECT %FROM "chunks"%FOR UPDATE'`).Scan(&waiting).Error; err != nil {
			t.Fatalf("inspect feedback row-lock wait: %v", err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(lockDeadline) {
			t.Fatal("feedback transaction did not enter a PostgreSQL row-lock wait")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := <-resetDone; err != nil {
		t.Fatalf("concurrent reset: %v", err)
	}
	if err := <-feedbackDone; err != nil {
		t.Fatalf("feedback after blocked reset: %v", err)
	}

	var chunk types.Chunk
	if err := db.Where("tenant_id = ? AND id = ?", 9, "chunk-1").First(&chunk).Error; err != nil {
		t.Fatalf("load chunk: %v", err)
	}
	var feedback types.MessageFeedback
	if err := db.First(&feedback).Error; err != nil {
		t.Fatalf("load feedback: %v", err)
	}
	if chunk.FeedbackResetAt == nil {
		t.Fatal("reset baseline was not stored")
	}
	if feedback.FeedbackAt.After(*chunk.FeedbackResetAt) {
		if chunk.LikeCount != 1 || chunk.DislikeCount != 0 || chunk.RecallWeight != cfg.HighRecallWeight {
			t.Fatalf("post-reset feedback disagrees with aggregate: feedback=%#v chunk=%#v", feedback, chunk)
		}
	} else if chunk.LikeCount != 0 || chunk.DislikeCount != 0 || chunk.RecallWeight != cfg.NormalRecallWeight {
		t.Fatalf("pre-reset feedback survived aggregate reset: feedback=%#v chunk=%#v", feedback, chunk)
	}
}
