package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestChunkFeedbackTriggerSourceSQLiteMigrationUpgradeAndRollback(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration test path")
	}
	migrationDir := filepath.Join(filepath.Dir(testFile), "..", "..", "migrations", "sqlite")
	execMigrationFile(t, db, migrationDir, "000000_init.up.sql")
	execMigrationFile(t, db, migrationDir, "000001_remove_wiki_log.up.sql")
	execMigrationFile(t, db, migrationDir, "000002_chunk_feedback.up.sql")

	_, err = db.Exec(`
		INSERT INTO chunk_feedback_audits
			(chunk_tenant_id, chunk_id, actor_tenant_id, actor_user_id, action, old_weight, new_weight)
		VALUES (1, 'legacy-chunk', 1, 'legacy-user', 'feedback_weight_changed', 1.0, 1.2)
	`)
	if err != nil {
		t.Fatal(err)
	}
	execMigrationFile(t, db, migrationDir, "000003_chunk_feedback_trigger_source.up.sql")

	var source string
	if err := db.QueryRow(
		"SELECT trigger_source FROM chunk_feedback_audits WHERE chunk_id = 'legacy-chunk'",
	).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "legacy" {
		t.Fatalf("legacy trigger source = %q, want legacy", source)
	}

	allowed := []string{"like", "dislike", "cancel", "admin_reset", "content_delete", "legacy"}
	for i, triggerSource := range allowed {
		if _, err := db.Exec(`
			INSERT INTO chunk_feedback_audits
				(chunk_tenant_id, chunk_id, actor_tenant_id, actor_user_id, action, trigger_source, old_weight, new_weight)
			VALUES (1, printf('chunk-%d', ?), 1, 'user', 'feedback_weight_changed', ?, 1.0, 1.0)
		`, i, triggerSource); err != nil {
			t.Fatalf("insert trigger source %q: %v", triggerSource, err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO chunk_feedback_audits
			(chunk_tenant_id, chunk_id, actor_tenant_id, actor_user_id, action, trigger_source, old_weight, new_weight)
		VALUES (1, 'invalid-chunk', 1, 'user', 'feedback_weight_changed', 'invalid', 1.0, 1.0)
	`); err == nil {
		t.Fatal("invalid trigger source unexpectedly passed the migration constraint")
	}

	execMigrationFile(t, db, migrationDir, "000003_chunk_feedback_trigger_source.down.sql")
	rows, err := db.Query("PRAGMA table_info(chunk_feedback_audits)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "trigger_source" {
			t.Fatal("down migration did not remove trigger_source")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func execMigrationFile(t *testing.T, db *sql.DB, migrationDir, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(migrationDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("execute %s: %v", name, err)
	}
}
