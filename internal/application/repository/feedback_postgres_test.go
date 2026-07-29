package repository

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

const feedbackPostgresTestDSNEnv = "WEKNORA_TEST_POSTGRES_DSN"

type feedbackPostgresBarrier struct {
	reached     chan struct{}
	release     chan struct{}
	reachedOnce sync.Once
	releaseOnce sync.Once
}

func newFeedbackPostgresBarrier() *feedbackPostgresBarrier {
	return &feedbackPostgresBarrier{
		reached: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *feedbackPostgresBarrier) block() {
	b.reachedOnce.Do(func() { close(b.reached) })
	<-b.release
}

func (b *feedbackPostgresBarrier) signal() {
	b.reachedOnce.Do(func() { close(b.reached) })
}

func (b *feedbackPostgresBarrier) unblock() {
	b.releaseOnce.Do(func() { close(b.release) })
}

func waitForFeedbackPostgresSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForFeedbackPostgresError(t *testing.T, result <-chan error, description string) {
	t.Helper()
	select {
	case err := <-result:
		require.NoError(t, err, description)
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func isFeedbackPostgresChunkQuery(tx *gorm.DB) bool {
	if tx == nil || tx.Statement == nil {
		return false
	}
	if tx.Statement.Table == "chunks" ||
		(tx.Statement.Schema != nil && tx.Statement.Schema.Table == "chunks") {
		return true
	}
	return strings.Contains(strings.ToLower(tx.Statement.SQL.String()), `from "chunks"`)
}

func installFeedbackPostgresChunkQueryBarrier(
	t *testing.T,
	db *gorm.DB,
	position string,
	barrier *feedbackPostgresBarrier,
	block bool,
) {
	t.Helper()
	name := "feedback_postgres_" + position + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	callback := func(tx *gorm.DB) {
		if !isFeedbackPostgresChunkQuery(tx) {
			return
		}
		if block {
			barrier.block()
		} else {
			barrier.signal()
		}
	}
	var err error
	switch position {
	case "before":
		err = db.Callback().Query().Before("gorm:query").Register(name, callback)
	case "after":
		err = db.Callback().Query().After("gorm:query").Register(name, callback)
	default:
		t.Fatalf("unsupported callback position %q", position)
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove(name))
	})
}

func setupFeedbackPostgresTestDatabases(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv(feedbackPostgresTestDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set", feedbackPostgresTestDSNEnv)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)

	schema := "feedback_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec(`CREATE SCHEMA "`+schema+`"`).Error)

	openSchemaConnection := func() *gorm.DB {
		db, openErr := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, openErr)
		sqlDB, dbErr := db.DB()
		require.NoError(t, dbErr)
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		require.NoError(t, db.Exec(`SET search_path TO "`+schema+`"`).Error)
		return db
	}

	completionDB := openSchemaConnection()
	deletionDB := openSchemaConnection()
	completionSQL, err := completionDB.DB()
	require.NoError(t, err)
	deletionSQL, err := deletionDB.DB()
	require.NoError(t, err)

	var completionPID, deletionPID int
	require.NoError(t, completionDB.Raw("SELECT pg_backend_pid()").Scan(&completionPID).Error)
	require.NoError(t, deletionDB.Raw("SELECT pg_backend_pid()").Scan(&deletionPID).Error)
	require.NotEqual(t, completionPID, deletionPID, "the concurrency test requires two database connections")

	t.Cleanup(func() {
		require.NoError(t, completionSQL.Close())
		require.NoError(t, deletionSQL.Close())
		require.NoError(t, admin.Exec(`DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`).Error)
		require.NoError(t, adminSQL.Close())
	})

	require.NoError(t, completionDB.Exec(`
		CREATE TABLE sessions (
			id varchar(36) PRIMARY KEY,
			tenant_id bigint NOT NULL,
			user_id varchar(512) NOT NULL DEFAULT '',
			deleted_at timestamptz
		);
		CREATE TABLE messages (
			id varchar(36) PRIMARY KEY,
			session_id varchar(36) NOT NULL,
			content text,
			role varchar(16),
			knowledge_references jsonb,
			agent_steps jsonb,
			is_completed boolean NOT NULL DEFAULT false,
			is_fallback boolean NOT NULL DEFAULT false,
			created_at timestamptz,
			updated_at timestamptz,
			deleted_at timestamptz
		);
		CREATE TABLE chunks (
			id varchar(36) PRIMARY KEY,
			tenant_id bigint NOT NULL,
			knowledge_id varchar(36) NOT NULL,
			knowledge_base_id varchar(36) NOT NULL,
			content text,
			source_content text,
			is_enabled boolean NOT NULL DEFAULT true,
			created_at timestamptz,
			updated_at timestamptz,
			deleted_at timestamptz,
			like_count bigint NOT NULL DEFAULT 0,
			dislike_count bigint NOT NULL DEFAULT 0,
			positive_rate double precision,
			recall_weight double precision NOT NULL DEFAULT 1,
			feedback_reset_at timestamptz
		);
		CREATE TABLE message_chunk_references (
			id varchar(36) PRIMARY KEY,
			message_tenant_id bigint NOT NULL,
			chunk_tenant_id bigint NOT NULL,
			message_id varchar(36) NOT NULL,
			chunk_id varchar(36) NOT NULL,
			created_at timestamptz NOT NULL,
			UNIQUE (message_tenant_id, chunk_tenant_id, message_id, chunk_id)
		);
		CREATE TABLE message_feedbacks (
			id varchar(36) PRIMARY KEY,
			tenant_id bigint NOT NULL,
			user_id varchar(64) NOT NULL,
			session_id varchar(36) NOT NULL,
			message_id varchar(36) NOT NULL,
			feedback_type varchar(16) NOT NULL,
			reason_code varchar(16),
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			UNIQUE (tenant_id, user_id, message_id)
		);
		CREATE TABLE chunk_feedback_audits (
			id bigserial PRIMARY KEY,
			chunk_tenant_id bigint NOT NULL,
			chunk_id varchar(36) NOT NULL,
			actor_tenant_id bigint NOT NULL,
			actor_user_id varchar(64) NOT NULL,
			action varchar(32) NOT NULL,
			trigger_source varchar(16) NOT NULL DEFAULT 'legacy',
			old_weight double precision NOT NULL,
			new_weight double precision NOT NULL,
			created_at timestamptz NOT NULL
		);
	`).Error)

	return completionDB, deletionDB
}

func seedFeedbackPostgresTestCase(
	t *testing.T, db *gorm.DB, suffix string, chunkIDs ...string,
) (*types.Session, *types.Message, map[string]*types.Chunk) {
	t.Helper()
	session := &types.Session{
		ID:       "session-" + suffix,
		TenantID: 101,
		UserID:   "",
	}
	message := &types.Message{
		ID:        "message-" + suffix,
		SessionID: session.ID,
		Role:      "assistant",
		Content:   "final",
	}
	require.NoError(t, db.Exec(
		"INSERT INTO sessions (id, tenant_id, user_id) VALUES (?, ?, ?)",
		session.ID, session.TenantID, session.UserID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO messages (id, session_id, content, role) VALUES (?, ?, ?, ?)",
		message.ID, message.SessionID, "draft", message.Role,
	).Error)

	chunks := make(map[string]*types.Chunk, len(chunkIDs))
	for _, id := range chunkIDs {
		chunk := &types.Chunk{
			ID:              id + "-" + suffix,
			TenantID:        202,
			KnowledgeBaseID: "kb-" + suffix,
			KnowledgeID:     "knowledge-" + suffix,
			Content:         id,
			SourceContent:   id,
			RecallWeight:    1,
			IsEnabled:       true,
		}
		require.NoError(t, db.Exec(`
			INSERT INTO chunks (
				id, tenant_id, knowledge_id, knowledge_base_id, content, source_content,
				is_enabled, recall_weight, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`,
			chunk.ID, chunk.TenantID, chunk.KnowledgeID, chunk.KnowledgeBaseID,
			chunk.Content, chunk.SourceContent, chunk.IsEnabled, chunk.RecallWeight,
		).Error)
		chunks[id] = chunk
	}
	return session, message, chunks
}

func feedbackPostgresReferences(chunks ...*types.Chunk) types.References {
	refs := make(types.References, 0, len(chunks))
	for _, chunk := range chunks {
		refs = append(refs, &types.SearchResult{
			ID: chunk.ID, KnowledgeBaseID: chunk.KnowledgeBaseID, ChunkType: types.ChunkTypeText,
		})
	}
	return refs
}

func assertFeedbackPostgresNoDanglingReferences(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(`
		SELECT COUNT(*)
		FROM message_chunk_references AS r
		JOIN chunks AS c
			ON c.tenant_id = r.chunk_tenant_id
			AND c.id = r.chunk_id
		WHERE c.deleted_at IS NOT NULL
	`).Scan(&count).Error)
	assert.Zero(t, count)
}

func TestFeedbackPostgresCompletionAndChunkDeletionConcurrency(t *testing.T) {
	t.Run("completion locks chunk before deletion", func(t *testing.T) {
		completionDB, deletionDB := setupFeedbackPostgresTestDatabases(t)
		session, message, chunks := seedFeedbackPostgresTestCase(t, completionDB, "scenario-a", "b")
		completionLocked := newFeedbackPostgresBarrier()
		deletionAttempted := newFeedbackPostgresBarrier()
		installFeedbackPostgresChunkQueryBarrier(t, completionDB, "after", completionLocked, true)
		installFeedbackPostgresChunkQueryBarrier(t, deletionDB, "before", deletionAttempted, false)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		completionResult := make(chan error, 1)
		go func() {
			_, err := (&feedbackRepository{db: completionDB}).CompleteAssistantMessageWithReferences(
				ctx, session.TenantID, message, feedbackPostgresReferences(chunks["b"]),
			)
			completionResult <- err
		}()
		waitForFeedbackPostgresSignal(t, completionLocked.reached, "completion to lock chunk B")

		deletionResult := make(chan error, 1)
		go func() {
			deletionResult <- (&chunkRepository{db: deletionDB}).
				DeleteChunk(ctx, chunks["b"].TenantID, chunks["b"].ID)
		}()
		waitForFeedbackPostgresSignal(t, deletionAttempted.reached, "deletion to attempt locking chunk B")
		completionLocked.unblock()

		waitForFeedbackPostgresError(t, completionResult, "completion")
		waitForFeedbackPostgresError(t, deletionResult, "deletion")
		assertFeedbackPostgresNoDanglingReferences(t, completionDB)

		var referenceCount int64
		require.NoError(t, completionDB.Model(&types.MessageChunkReference{}).
			Where("chunk_id = ?", chunks["b"].ID).Count(&referenceCount).Error)
		assert.Zero(t, referenceCount)
	})

	t.Run("deletion locks chunk before completion", func(t *testing.T) {
		completionDB, deletionDB := setupFeedbackPostgresTestDatabases(t)
		session, message, chunks := seedFeedbackPostgresTestCase(t, completionDB, "scenario-b", "a", "b")
		deletionLocked := newFeedbackPostgresBarrier()
		completionAttempted := newFeedbackPostgresBarrier()
		installFeedbackPostgresChunkQueryBarrier(t, deletionDB, "after", deletionLocked, true)
		installFeedbackPostgresChunkQueryBarrier(t, completionDB, "before", completionAttempted, false)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		deletionResult := make(chan error, 1)
		go func() {
			deletionResult <- (&chunkRepository{db: deletionDB}).
				DeleteChunk(ctx, chunks["b"].TenantID, chunks["b"].ID)
		}()
		waitForFeedbackPostgresSignal(t, deletionLocked.reached, "deletion to lock chunk B")

		completionResult := make(chan error, 1)
		go func() {
			_, err := (&feedbackRepository{db: completionDB}).CompleteAssistantMessageWithReferences(
				ctx, session.TenantID, message,
				feedbackPostgresReferences(chunks["b"], chunks["a"]),
			)
			completionResult <- err
		}()
		waitForFeedbackPostgresSignal(t, completionAttempted.reached, "completion to attempt locking chunks")
		deletionLocked.unblock()

		waitForFeedbackPostgresError(t, deletionResult, "deletion")
		waitForFeedbackPostgresError(t, completionResult, "completion")
		assertFeedbackPostgresNoDanglingReferences(t, completionDB)

		var referenceIDs []string
		require.NoError(t, completionDB.Model(&types.MessageChunkReference{}).
			Where("message_id = ?", message.ID).
			Order("chunk_id").Pluck("chunk_id", &referenceIDs).Error)
		assert.Equal(t, []string{chunks["a"].ID}, referenceIDs)
		var completed bool
		require.NoError(t, completionDB.Table("messages").
			Select("is_completed").Where("id = ?", message.ID).Scan(&completed).Error)
		assert.True(t, completed)
	})

	t.Run("deleting one of three references preserves feedback on the others", func(t *testing.T) {
		completionDB, deletionDB := setupFeedbackPostgresTestDatabases(t)
		session, message, chunks := seedFeedbackPostgresTestCase(
			t, completionDB, "scenario-c", "a", "b", "c",
		)
		completionLocked := newFeedbackPostgresBarrier()
		deletionAttempted := newFeedbackPostgresBarrier()
		installFeedbackPostgresChunkQueryBarrier(t, completionDB, "after", completionLocked, true)
		installFeedbackPostgresChunkQueryBarrier(t, deletionDB, "before", deletionAttempted, false)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		completionResult := make(chan error, 1)
		go func() {
			_, err := (&feedbackRepository{db: completionDB}).CompleteAssistantMessageWithReferences(
				ctx, session.TenantID, message,
				feedbackPostgresReferences(chunks["c"], chunks["b"], chunks["a"]),
			)
			completionResult <- err
		}()
		waitForFeedbackPostgresSignal(t, completionLocked.reached, "completion to lock chunks A, B, and C")

		deletionResult := make(chan error, 1)
		go func() {
			deletionResult <- (&chunkRepository{db: deletionDB}).
				DeleteChunk(ctx, chunks["b"].TenantID, chunks["b"].ID)
		}()
		waitForFeedbackPostgresSignal(t, deletionAttempted.reached, "deletion to attempt locking chunk B")
		completionLocked.unblock()

		waitForFeedbackPostgresError(t, completionResult, "completion")
		waitForFeedbackPostgresError(t, deletionResult, "deletion")
		assertFeedbackPostgresNoDanglingReferences(t, completionDB)

		repo := &feedbackRepository{db: completionDB}
		for _, input := range []types.ApplyMessageFeedbackInput{
			{
				MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
				ActorUserID: "user-like", SessionID: session.ID, MessageID: message.ID,
				Type: types.FeedbackTypeLike,
			},
			{
				MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
				ActorUserID: "user-dislike", SessionID: session.ID, MessageID: message.ID,
				Type: types.FeedbackTypeDislike,
			},
			{
				MessageTenantID: session.TenantID, ActorTenantID: session.TenantID,
				ActorUserID: "user-like", SessionID: session.ID, MessageID: message.ID,
				Type: types.FeedbackTypeNone,
			},
		} {
			_, err := repo.ApplyMessageFeedback(ctx, input)
			require.NoError(t, err)
		}

		var referenceIDs []string
		require.NoError(t, completionDB.Model(&types.MessageChunkReference{}).
			Where("message_id = ?", message.ID).
			Order("chunk_id").Pluck("chunk_id", &referenceIDs).Error)
		assert.Equal(t, []string{chunks["a"].ID, chunks["c"].ID}, referenceIDs)

		for _, id := range []string{"a", "c"} {
			var chunk types.Chunk
			require.NoError(t, completionDB.First(&chunk, "id = ?", chunks[id].ID).Error)
			assert.Zero(t, chunk.LikeCount)
			assert.EqualValues(t, 1, chunk.DislikeCount)
			assert.Equal(t, 0.8, chunk.RecallWeight)
		}
		var deleted types.Chunk
		require.NoError(t, completionDB.Unscoped().First(&deleted, "id = ?", chunks["b"].ID).Error)
		assert.True(t, deleted.DeletedAt.Valid)
		assert.Zero(t, deleted.LikeCount)
		assert.Zero(t, deleted.DislikeCount)
		assert.Equal(t, 1.0, deleted.RecallWeight)

		var deletedChunkAuditCount int64
		require.NoError(t, completionDB.Model(&types.ChunkFeedbackAudit{}).
			Where("chunk_id = ?", chunks["b"].ID).Count(&deletedChunkAuditCount).Error)
		assert.Zero(t, deletedChunkAuditCount)
	})
}
