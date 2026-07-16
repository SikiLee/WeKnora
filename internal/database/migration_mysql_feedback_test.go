package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type mysqlFeedbackLog struct {
	ID           string
	SourceAction string
	OldWeight    float64
	NewWeight    float64
	CreatedAt    time.Time
}

func TestMySQLFeedbackFreshSchemaAndLogOrdering(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("WEKNORA_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("WEKNORA_TEST_MYSQL_DSN is not set")
	}

	adminConfig, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	adminConfig.DBName = ""
	adminConfig.MultiStatements = true
	adminDB, err := sql.Open("mysql", adminConfig.FormatDSN())
	require.NoError(t, err)
	require.NoError(t, adminDB.Ping())

	databaseName := "feedback_mysql_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, execMySQLIdentifierStatement(adminDB, "CREATE DATABASE ", databaseName))

	testConfig := adminConfig
	testConfig.DBName = databaseName
	testConfig.MultiStatements = true
	testConfig.ParseTime = true
	db, err := sql.Open("mysql", testConfig.FormatDSN())
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		require.NoError(t, execMySQLIdentifierStatement(adminDB, "DROP DATABASE ", databaseName))
		require.NoError(t, adminDB.Close())
	})

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	schemaPath := filepath.Join(
		filepath.Dir(currentFile),
		"..",
		"..",
		"migrations",
		"mysql",
		"00-init-db.sql",
	)
	schema, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	_, err = db.Exec(string(schema))
	require.NoError(t, err)

	expectedPrecision := map[string][]string{
		"chunks": {
			"feedback_reset_at",
			"feedback_updated_at",
		},
		"message_feedbacks": {
			"feedback_at",
			"created_at",
			"updated_at",
		},
		"message_chunk_references": {
			"created_at",
		},
		"chunk_feedback_weight_logs": {
			"created_at",
		},
	}
	for tableName, columns := range expectedPrecision {
		for _, columnName := range columns {
			require.Equal(
				t,
				int64(6),
				mysqlDatetimePrecision(t, db, databaseName, tableName, columnName),
				"%s.%s",
				tableName,
				columnName,
			)
		}
	}

	logs := []mysqlFeedbackLog{
		{ID: "z-like", SourceAction: "like", OldWeight: 1.0, NewWeight: 1.2},
		{ID: "a-dislike", SourceAction: "dislike", OldWeight: 1.2, NewWeight: 0.8},
		{ID: "z-none", SourceAction: "none", OldWeight: 0.8, NewWeight: 1.0},
		{ID: "a-reset", SourceAction: "reset", OldWeight: 1.0, NewWeight: 1.0},
	}
	for index := range logs {
		_, err := db.Exec(`
			INSERT INTO chunk_feedback_weight_logs (
				id,
				chunk_tenant_id,
				chunk_id,
				old_weight,
				new_weight,
				source,
				source_action
			) VALUES (?, 9, 'chunk-1', ?, ?, 'feedback-test', ?)
		`, logs[index].ID, logs[index].OldWeight, logs[index].NewWeight, logs[index].SourceAction)
		require.NoError(t, err)
		if index < len(logs)-1 {
			time.Sleep(2 * time.Millisecond)
		}
	}

	ordered := loadMySQLFeedbackLogs(t, db, 10, 0)
	require.Len(t, ordered, len(logs))
	for index := range ordered {
		expected := logs[len(logs)-1-index]
		require.Equal(t, expected.ID, ordered[index].ID)
		require.Equal(t, expected.SourceAction, ordered[index].SourceAction)
		require.InDelta(t, expected.OldWeight, ordered[index].OldWeight, 0.000001)
		require.InDelta(t, expected.NewWeight, ordered[index].NewWeight, 0.000001)
		if index > 0 {
			require.True(
				t,
				ordered[index-1].CreatedAt.After(ordered[index].CreatedAt),
				"timestamps must preserve write order: %#v",
				ordered,
			)
		}
	}
	require.Less(t, ordered[0].CreatedAt.Sub(ordered[len(ordered)-1].CreatedAt), time.Second)

	firstPage := loadMySQLFeedbackLogs(t, db, 2, 0)
	secondPage := loadMySQLFeedbackLogs(t, db, 2, 2)
	require.Equal(t, ordered, append(firstPage, secondPage...))
}

func execMySQLIdentifierStatement(db *sql.DB, prefix, identifier string) error {
	_, err := db.Exec(prefix + "`" + strings.ReplaceAll(identifier, "`", "``") + "`")
	return err
}

func mysqlDatetimePrecision(
	t *testing.T,
	db *sql.DB,
	databaseName string,
	tableName string,
	columnName string,
) int64 {
	t.Helper()
	var precision sql.NullInt64
	require.NoError(t, db.QueryRow(`
		SELECT DATETIME_PRECISION
		FROM information_schema.columns
		WHERE table_schema = ?
		  AND table_name = ?
		  AND column_name = ?
	`, databaseName, tableName, columnName).Scan(&precision))
	require.True(t, precision.Valid, "%s.%s has no datetime precision", tableName, columnName)
	return precision.Int64
}

func loadMySQLFeedbackLogs(t *testing.T, db *sql.DB, limit, offset int) []mysqlFeedbackLog {
	t.Helper()
	rows, err := db.Query(`
		SELECT id, source_action, old_weight, new_weight, created_at
		FROM chunk_feedback_weight_logs
		WHERE chunk_tenant_id = 9 AND chunk_id = 'chunk-1'
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	logs := make([]mysqlFeedbackLog, 0, limit)
	for rows.Next() {
		var log mysqlFeedbackLog
		require.NoError(t, rows.Scan(
			&log.ID,
			&log.SourceAction,
			&log.OldWeight,
			&log.NewWeight,
			&log.CreatedAt,
		))
		logs = append(logs, log)
	}
	require.NoError(t, rows.Err())
	return logs
}
