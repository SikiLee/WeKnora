package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	sqlite3migrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestSQLiteAnswerFeedbackMigrationFreshAndVersionUpgrade(t *testing.T) {
	chdirRepoRoot(t)

	freshDB := filepath.Join(t.TempDir(), "fresh.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://fresh", MigrationOptions{SQLiteDBPath: freshDB}))
	assertSQLiteFeedbackSchema(t, freshDB, true)
	version, dirty := sqliteMigrationVersion(t, freshDB)
	require.Equal(t, uint(1), version)
	require.False(t, dirty)

	existingDB := filepath.Join(t.TempDir(), "existing.db")
	m, closeMigrator := newSQLiteMigrator(t, existingDB)
	require.NoError(t, m.Steps(1))
	closeMigrator()
	version, dirty = sqliteMigrationVersion(t, existingDB)
	require.Equal(t, uint(0), version)
	require.False(t, dirty)
	assertSQLiteFeedbackSchema(t, existingDB, false)

	require.NoError(t, RunMigrationsWithOptions("sqlite3://existing", MigrationOptions{SQLiteDBPath: existingDB}))
	assertSQLiteFeedbackSchema(t, existingDB, true)
	version, dirty = sqliteMigrationVersion(t, existingDB)
	require.Equal(t, uint(1), version)
	require.False(t, dirty)

	m, closeMigrator = newSQLiteMigrator(t, existingDB)
	require.NoError(t, m.Steps(-1))
	closeMigrator()
	assertSQLiteFeedbackSchema(t, existingDB, false)
	version, dirty = sqliteMigrationVersion(t, existingDB)
	require.Equal(t, uint(0), version)
	require.False(t, dirty)

	m, closeMigrator = newSQLiteMigrator(t, existingDB)
	require.NoError(t, m.Steps(1))
	closeMigrator()
	assertSQLiteFeedbackSchema(t, existingDB, true)
	version, dirty = sqliteMigrationVersion(t, existingDB)
	require.Equal(t, uint(1), version)
	require.False(t, dirty)
}

func chdirRepoRoot(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(previous))
	})
}

func newSQLiteMigrator(t *testing.T, dbPath string) (*migrate.Migrate, func()) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	driver, err := sqlite3migrate.WithInstance(db, &sqlite3migrate.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithDatabaseInstance("file://migrations/sqlite", "sqlite3", driver)
	require.NoError(t, err)
	return m, func() {
		sourceErr, databaseErr := m.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, databaseErr)
		require.NoError(t, db.Close())
	}
}

func sqliteMigrationVersion(t *testing.T, dbPath string) (uint, bool) {
	t.Helper()
	m, closeMigrator := newSQLiteMigrator(t, dbPath)
	defer closeMigrator()
	version, dirty, err := m.Version()
	require.NoError(t, err)
	return version, dirty
}

func assertSQLiteFeedbackSchema(t *testing.T, dbPath string, wantPresent bool) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	for _, column := range []string{
		"like_count",
		"dislike_count",
		"positive_rate",
		"recall_weight",
		"needs_optimization",
		"feedback_reset_at",
		"feedback_updated_at",
	} {
		require.Equal(t, wantPresent, sqliteColumnExists(t, db, "chunks", column), "column %s", column)
	}

	for _, table := range []string{
		"message_feedbacks",
		"message_chunk_references",
		"chunk_feedback_weight_logs",
	} {
		require.Equal(t, wantPresent, sqliteTableExists(t, db, table), "table %s", table)
	}
}

func sqliteColumnExists(t *testing.T, db *sql.DB, tableName, columnName string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + tableName + ")")
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		require.NoError(t, rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk))
		if name == columnName {
			return true
		}
	}
	require.NoError(t, rows.Err())
	return false
}

func sqliteTableExists(t *testing.T, db *sql.DB, tableName string) bool {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		tableName,
	).Scan(&count))
	return count == 1
}
