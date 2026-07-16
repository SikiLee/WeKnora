"""Executable SQLite contract test for the RAG evaluation migrations."""

from pathlib import Path
import sqlite3


ROOT = Path(__file__).resolve().parents[1]


def expect_integrity_error(conn: sqlite3.Connection, sql: str, params: tuple) -> None:
    try:
        conn.execute(sql, params)
    except sqlite3.IntegrityError:
        return
    raise AssertionError(f"statement unexpectedly succeeded: {sql}")


def main() -> None:
    conn = sqlite3.connect(":memory:")
    conn.execute("PRAGMA foreign_keys=ON")
    for migration in (
        ROOT / "migrations/sqlite/000000_init.up.sql",
        ROOT / "migrations/sqlite/000001_rag_evaluation_mvp.up.sql",
    ):
        conn.executescript(migration.read_text(encoding="utf-8"))

    tables = conn.execute(
        "SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'evaluation_%'"
    ).fetchone()[0]
    assert tables == 15, tables

    conn.execute(
        "INSERT INTO evaluation_testsets(id,tenant_id,knowledge_base_id,name,status,created_by) VALUES(?,?,?,?,?,?)",
        ("ts", 1, "kb", "test", "active", "user"),
    )
    conn.execute(
        """INSERT INTO evaluation_testset_versions
        (id,tenant_id,knowledge_base_id,testset_id,version_number,status,profile_locked,document_snapshots,generator_model_snapshot,created_by)
        VALUES(?,?,?,?,?,'draft',1,'{}','{}',?)""",
        ("version", 1, "kb", "ts", 1, "user"),
    )
    conn.execute(
        """INSERT INTO evaluation_cases
        (id,tenant_id,knowledge_base_id,version_id,question,answerability,question_type,difficulty,reference_answer,
         reference_key_points,checked_scope,review_status,quality_status,quality_reason_codes,split,generation_provenance)
        VALUES(?,?,?,?,?,'answerable','single_evidence','medium','answer','[]','{}','approved','passed','[]','holdout','{}')""",
        ("case", 1, "kb", "version", "question"),
    )
    conn.execute(
        """INSERT INTO evaluation_gold_evidences
        (id,tenant_id,knowledge_base_id,case_id,ordinal,source_document_id,source_document_hash,normalized_source_hash,
         source_normalization_version,offset_unit,start_at,end_at,evidence_text,evidence_hash)
        VALUES(?,?,?,?,1,'doc','dh','nh','v1','unicode_codepoint',0,3,'abc','eh')""",
        ("evidence", 1, "kb", "case"),
    )
    conn.execute(
        """INSERT INTO evaluation_runs
        (id,tenant_id,knowledge_base_id,testset_version_id,trigger_type,status,stage,idempotency_key,snapshot,sampled_case_ids,
         total_items,observability_status,created_by)
        VALUES(?,?,?,?,?,'pending','queued',?,'{}','["case"]',1,'pending','user')""",
        ("run", 1, "kb", "version", "manual", "idem"),
    )
    conn.execute(
        """INSERT INTO evaluation_run_items
        (id,tenant_id,knowledge_base_id,run_id,case_id,status,answerability_snapshot,question_snapshot,reference_snapshot,
         gold_evidence_snapshot,retrieved_contexts,citations,latency,tokens,cost,trace_correlation_id)
        VALUES(?,?,?,?,?,'pending','answerable','question','{"answer":"answer"}','[{"id":"evidence"}]','[]','[]','{}','{}','{}','trace')""",
        ("item", 1, "kb", "run", "case"),
    )
    expect_integrity_error(
        conn,
        """INSERT INTO evaluation_metric_results
        (id,tenant_id,knowledge_base_id,run_id,run_item_id,metric_name,metric_version,status,value,details,judge_execution)
        VALUES(?,?,?,?,?,'hit@5','v1','invalid',0,'{}','{}')""",
        ("bad-metric", 1, "kb", "run", "item"),
    )

    conn.execute(
        "INSERT INTO evaluation_schedules(id,tenant_id,knowledge_base_id,name,cron_expression,timezone,run_template,created_by) VALUES(?,?,?,?,?,?,?,?)",
        ("schedule", 1, "kb", "nightly", "0 0 2 * * *", "UTC", "{}", "user"),
    )
    conn.execute(
        "INSERT INTO evaluation_schedule_slots(id,tenant_id,knowledge_base_id,schedule_id,scheduled_at_utc,status) VALUES(?,?,?,?,?,'claimed')",
        ("slot-1", 1, "kb", "schedule", "2026-07-16T02:00:00Z"),
    )
    expect_integrity_error(
        conn,
        "INSERT INTO evaluation_schedule_slots(id,tenant_id,knowledge_base_id,schedule_id,scheduled_at_utc,status) VALUES(?,?,?,?,?,'claimed')",
        ("slot-2", 1, "kb", "schedule", "2026-07-16T02:00:00Z"),
    )

    conn.execute(
        """INSERT INTO evaluation_failure_diagnoses
        (id,tenant_id,knowledge_base_id,source_run_id,version,status,classification,reason_code,evidence_signals,target_k,valid_cases)
        VALUES('diagnosis',1,'kb','run','v1','completed','chunking_likely','CHUNK_BOUNDARY_SPLIT','{}',5,30)"""
    )
    conn.execute(
        """INSERT INTO evaluation_chunk_experiments
        (id,tenant_id,knowledge_base_id,source_run_id,diagnosis_id,testset_version_id,status,idempotency_key,fixed_snapshot,estimated_calls,cleanup_status,created_by)
        VALUES('experiment',1,'kb','run','diagnosis','version','draft','experiment-idem','{}',100,'not_started','user')"""
    )
    for idx, role in enumerate(("baseline", "candidate", "candidate"), start=1):
        conn.execute(
            """INSERT INTO evaluation_chunk_experiment_variants
            (id,tenant_id,knowledge_base_id,experiment_id,variant_key,role,chunking_snapshot,config_hash,source_mapping,status,cleanup_status)
            VALUES(?,?,?,?,?,?, '{}',?, '{}','pending','not_started')""",
            (f"variant-{idx}", 1, "kb", "experiment", f"v{idx}", role, f"hash-{idx}"),
        )
    expect_integrity_error(
        conn,
        """INSERT INTO evaluation_chunk_experiment_variants
        (id,tenant_id,knowledge_base_id,experiment_id,variant_key,role,chunking_snapshot,config_hash,source_mapping,status,cleanup_status)
        VALUES('variant-4',1,'kb','experiment','v4','candidate','{}','hash-4','{}','pending','not_started')""",
        (),
    )

    conn.executescript((ROOT / "migrations/sqlite/000001_rag_evaluation_mvp.down.sql").read_text(encoding="utf-8"))
    remaining = conn.execute(
        "SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'evaluation_%'"
    ).fetchone()[0]
    assert remaining == 0, remaining
    print("rag evaluation sqlite migration contract: PASS")


if __name__ == "__main__":
    main()
