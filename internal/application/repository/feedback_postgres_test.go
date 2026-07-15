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
		if chunk.LikeCount != 5 || chunk.DislikeCount != 5 || chunk.PositiveRate == nil || *chunk.PositiveRate != 0.5 || chunk.RecallWeight != 1 {
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
			if index%3 == 1 {
				feedbackType = types.FeedbackTypeDislike
				reasonCode = types.FeedbackReasonIncorrect
			} else if index%3 == 2 {
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
