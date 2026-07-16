package database

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var feedbackMigrationColumns = []string{
	"like_count",
	"dislike_count",
	"positive_rate",
	"recall_weight",
	"needs_optimization",
	"feedback_reset_at",
	"feedback_updated_at",
}

var feedbackMigrationRelations = []string{
	"message_feedbacks",
	"idx_message_feedbacks_user_message",
	"idx_message_feedbacks_session",
	"idx_message_feedbacks_feedback_at",
	"message_chunk_references",
	"idx_message_chunk_refs_message_chunk",
	"idx_message_chunk_refs_chunk",
	"idx_message_chunk_refs_kb",
	"idx_message_chunk_refs_session",
	"chunk_feedback_weight_logs",
	"idx_chunk_feedback_weight_logs_chunk",
	"idx_chunk_feedback_weight_logs_created_at",
}

func TestPostgresAnswerFeedbackMigrationUpDownUp(t *testing.T) {
	db := newFeedbackPostgresMigrationDB(t)
	up := readFeedbackPostgresMigration(t, "up")
	down := readFeedbackPostgresMigration(t, "down")

	require.NoError(t, db.Exec(up).Error)
	assertPostgresFeedbackMigrationSchema(t, db, true)

	require.NoError(t, db.Exec(down).Error)
	assertPostgresFeedbackMigrationSchema(t, db, false)

	require.NoError(t, db.Exec(up).Error)
	assertPostgresFeedbackMigrationSchema(t, db, true)
}

func TestPostgresAnswerFeedbackMigrationRejectsPreexistingObjects(t *testing.T) {
	up := readFeedbackPostgresMigration(t, "up")

	t.Run("table", func(t *testing.T) {
		db := newFeedbackPostgresMigrationDB(t)
		require.NoError(t, db.Exec(
			"CREATE TABLE message_feedbacks (sentinel TEXT NOT NULL)",
		).Error)
		require.NoError(t, db.Exec(
			"INSERT INTO message_feedbacks (sentinel) VALUES ('keep-me')",
		).Error)

		require.Error(t, db.Exec(up).Error)

		var sentinel string
		require.NoError(t, db.Raw("SELECT sentinel FROM message_feedbacks").Scan(&sentinel).Error)
		require.Equal(t, "keep-me", sentinel)
		assertPostgresFeedbackMigrationNotPartiallyApplied(t, db, "", "message_feedbacks")
	})

	t.Run("column", func(t *testing.T) {
		db := newFeedbackPostgresMigrationDB(t)
		require.NoError(t, db.Exec(
			"ALTER TABLE chunks ADD COLUMN like_count TEXT NOT NULL DEFAULT 'sentinel'",
		).Error)

		require.Error(t, db.Exec(up).Error)

		require.Equal(t, "text", postgresColumnType(t, db, "chunks", "like_count"))
		assertPostgresFeedbackMigrationNotPartiallyApplied(t, db, "like_count", "")
	})

	t.Run("index", func(t *testing.T) {
		db := newFeedbackPostgresMigrationDB(t)
		require.NoError(t, db.Exec(`
			CREATE TABLE sentinel_index_owner (
				created_at TIMESTAMP WITH TIME ZONE,
				payload TEXT NOT NULL
			)
		`).Error)
		require.NoError(t, db.Exec(`
			CREATE INDEX idx_chunk_feedback_weight_logs_created_at
			ON sentinel_index_owner(created_at)
		`).Error)
		require.NoError(t, db.Exec(
			"INSERT INTO sentinel_index_owner (payload) VALUES ('keep-me')",
		).Error)

		require.Error(t, db.Exec(up).Error)

		var sentinel string
		require.NoError(t, db.Raw("SELECT payload FROM sentinel_index_owner").Scan(&sentinel).Error)
		require.Equal(t, "keep-me", sentinel)
		require.Equal(
			t,
			"sentinel_index_owner",
			postgresIndexTable(t, db, "idx_chunk_feedback_weight_logs_created_at"),
		)
		assertPostgresFeedbackMigrationNotPartiallyApplied(
			t,
			db,
			"",
			"idx_chunk_feedback_weight_logs_created_at",
		)
	})
}

func newFeedbackPostgresMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("WEKNORA_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("WEKNORA_TEST_POSTGRES_DSN is not set")
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)

	schema := "feedback_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)

	db, err := gorm.Open(postgres.Open(postgresMigrationDSNWithSearchPath(t, dsn, schema)), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
		require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
		require.NoError(t, adminSQL.Close())
	})

	require.NoError(t, db.Exec(`
		CREATE TABLE chunks (
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
		)
	`).Error)
	return db
}

func postgresMigrationDSNWithSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema
}

func readFeedbackPostgresMigration(t *testing.T, direction string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	path := filepath.Join(
		filepath.Dir(currentFile),
		"..",
		"..",
		"migrations",
		"versioned",
		"000070_answer_feedback."+direction+".sql",
	)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func assertPostgresFeedbackMigrationSchema(t *testing.T, db *gorm.DB, wantPresent bool) {
	t.Helper()
	for _, column := range feedbackMigrationColumns {
		require.Equal(
			t,
			wantPresent,
			postgresColumnExists(t, db, "chunks", column),
			"column %s",
			column,
		)
	}
	for _, relation := range feedbackMigrationRelations {
		require.Equal(
			t,
			wantPresent,
			postgresRelationExists(t, db, relation),
			"relation %s",
			relation,
		)
	}
}

func assertPostgresFeedbackMigrationNotPartiallyApplied(
	t *testing.T,
	db *gorm.DB,
	exceptColumn string,
	exceptRelation string,
) {
	t.Helper()
	for _, column := range feedbackMigrationColumns {
		if column == exceptColumn {
			continue
		}
		require.False(t, postgresColumnExists(t, db, "chunks", column), "column %s", column)
	}
	for _, relation := range feedbackMigrationRelations {
		if relation == exceptRelation {
			continue
		}
		require.False(t, postgresRelationExists(t, db, relation), "relation %s", relation)
	}
}

func postgresColumnExists(t *testing.T, db *gorm.DB, tableName, columnName string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = ?
			  AND column_name = ?
		)
	`, tableName, columnName).Scan(&exists).Error)
	return exists
}

func postgresColumnType(t *testing.T, db *gorm.DB, tableName, columnName string) string {
	t.Helper()
	var dataType string
	require.NoError(t, db.Raw(`
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = ?
		  AND column_name = ?
	`, tableName, columnName).Scan(&dataType).Error)
	return dataType
}

func postgresRelationExists(t *testing.T, db *gorm.DB, relationName string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.Raw(`
		SELECT to_regclass(format('%I.%I', current_schema(), CAST(? AS TEXT))) IS NOT NULL
	`, relationName).Scan(&exists).Error)
	return exists
}

func postgresIndexTable(t *testing.T, db *gorm.DB, indexName string) string {
	t.Helper()
	var tableName string
	require.NoError(t, db.Raw(`
		SELECT tablename
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname = ?
	`, indexName).Scan(&tableName).Error)
	return tableName
}
