PRAGMA foreign_keys = ON;

CREATE TABLE evaluation_testsets (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    name TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('active','archived')),
    current_published_version_id TEXT, created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, archived_at DATETIME
);
CREATE INDEX idx_eval_testsets_scope ON evaluation_testsets(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_testset_versions (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    testset_id TEXT NOT NULL REFERENCES evaluation_testsets(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK(version_number > 0),
    status TEXT NOT NULL CHECK(status IN ('draft','published','archived')),
    profile_locked INTEGER NOT NULL DEFAULT 0,
    generation_profile_hash TEXT NOT NULL DEFAULT '',
    document_snapshots TEXT NOT NULL DEFAULT '{}', source_normalization_version TEXT NOT NULL DEFAULT '',
    offset_unit TEXT NOT NULL DEFAULT '', split_algorithm TEXT NOT NULL DEFAULT '', split_seed INTEGER NOT NULL DEFAULT 0,
    generator_model_snapshot TEXT NOT NULL DEFAULT '{}', generator_prompt_version TEXT NOT NULL DEFAULT '',
    generator_prompt_hash TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL,
    published_at DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(testset_id, version_number),
    CHECK((status = 'published') = (published_at IS NOT NULL))
);
CREATE INDEX idx_eval_versions_scope ON evaluation_testset_versions(tenant_id, knowledge_base_id, testset_id);

CREATE TABLE evaluation_cases (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    version_id TEXT NOT NULL REFERENCES evaluation_testset_versions(id) ON DELETE CASCADE,
    question TEXT NOT NULL, answerability TEXT NOT NULL CHECK(answerability IN ('answerable','unanswerable')),
    question_type TEXT NOT NULL CHECK(question_type IN ('single_evidence','multi_evidence','unanswerable')),
    difficulty TEXT NOT NULL CHECK(difficulty IN ('easy','medium','hard')),
    reference_answer TEXT NOT NULL DEFAULT '', reference_key_points TEXT NOT NULL DEFAULT '[]',
    expected_refusal TEXT NOT NULL DEFAULT '', unanswerable_reason TEXT NOT NULL DEFAULT '',
    checked_scope TEXT NOT NULL DEFAULT '{}', review_status TEXT NOT NULL CHECK(review_status IN ('pending','approved','rejected')),
    quality_status TEXT NOT NULL CHECK(quality_status IN ('pending','passed','failed')),
    quality_reason_codes TEXT NOT NULL DEFAULT '[]', split TEXT NOT NULL CHECK(split IN ('tuning','holdout')),
    enabled INTEGER NOT NULL DEFAULT 1, generation_provenance TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK((answerability='answerable' AND question_type IN ('single_evidence','multi_evidence') AND reference_answer<>'' AND expected_refusal='' AND unanswerable_reason='') OR
          (answerability='unanswerable' AND question_type='unanswerable' AND reference_answer='' AND expected_refusal<>'' AND unanswerable_reason<>''))
);
CREATE INDEX idx_eval_cases_scope ON evaluation_cases(tenant_id, knowledge_base_id, version_id);

CREATE TABLE evaluation_gold_evidences (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    case_id TEXT NOT NULL REFERENCES evaluation_cases(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK(ordinal IN (1,2)), source_document_id TEXT NOT NULL,
    source_document_hash TEXT NOT NULL, normalized_source_hash TEXT NOT NULL,
    source_normalization_version TEXT NOT NULL, offset_unit TEXT NOT NULL,
    start_at INTEGER NOT NULL CHECK(start_at >= 0), end_at INTEGER NOT NULL CHECK(end_at > start_at),
    evidence_text TEXT NOT NULL, evidence_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(case_id, ordinal)
);
CREATE INDEX idx_eval_evidence_scope ON evaluation_gold_evidences(tenant_id, knowledge_base_id, case_id);

CREATE TABLE evaluation_testset_generations (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    testset_id TEXT NOT NULL REFERENCES evaluation_testsets(id) ON DELETE CASCADE,
    version_id TEXT NOT NULL REFERENCES evaluation_testset_versions(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('pending','running','completed','partial','failed','canceled','blocked')),
    idempotency_key TEXT NOT NULL, profile_snapshot TEXT NOT NULL DEFAULT '{}',
    requested_cases INTEGER NOT NULL CHECK(requested_cases BETWEEN 1 AND 200), accepted_cases INTEGER NOT NULL DEFAULT 0,
    quality_counts TEXT NOT NULL DEFAULT '{}', error_code TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, started_at DATETIME, finished_at DATETIME,
    UNIQUE(tenant_id, idempotency_key)
);
CREATE INDEX idx_eval_generations_scope ON evaluation_testset_generations(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_runs (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    testset_version_id TEXT NOT NULL REFERENCES evaluation_testset_versions(id) ON DELETE RESTRICT,
    parent_run_id TEXT REFERENCES evaluation_runs(id) ON DELETE RESTRICT,
    trigger_type TEXT NOT NULL CHECK(trigger_type IN ('manual','schedule','retry','experiment')),
    status TEXT NOT NULL CHECK(status IN ('pending','running','completed','partial','failed','canceled','blocked')),
    stage TEXT NOT NULL, idempotency_key TEXT NOT NULL, snapshot TEXT NOT NULL, sampled_case_ids TEXT NOT NULL DEFAULT '[]',
    total_items INTEGER NOT NULL CHECK(total_items BETWEEN 1 AND 200), completed_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0, blocked_reason_code TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '', observability_status TEXT NOT NULL DEFAULT 'pending'
      CHECK(observability_status IN ('pending','available','partial','unavailable','disabled')),
    langfuse_trace_id TEXT NOT NULL DEFAULT '', estimated_calls INTEGER NOT NULL DEFAULT 0,
    created_by TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME, finished_at DATETIME, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, idempotency_key), CHECK(completed_items + failed_items <= total_items)
);
CREATE INDEX idx_eval_runs_scope ON evaluation_runs(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_run_items (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES evaluation_runs(id) ON DELETE CASCADE,
    case_id TEXT NOT NULL REFERENCES evaluation_cases(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK(status IN ('pending','running','completed','failed','canceled','blocked')),
    answerability_snapshot TEXT NOT NULL CHECK(answerability_snapshot IN ('answerable','unanswerable')),
    question_snapshot TEXT NOT NULL, reference_snapshot TEXT NOT NULL DEFAULT '{}', gold_evidence_snapshot TEXT NOT NULL DEFAULT '[]',
    retrieved_contexts TEXT NOT NULL DEFAULT '[]', generated_answer TEXT NOT NULL DEFAULT '', citations TEXT NOT NULL DEFAULT '[]',
    latency TEXT NOT NULL DEFAULT '{}', tokens TEXT NOT NULL DEFAULT '{}', cost TEXT NOT NULL DEFAULT '{}',
    trace_correlation_id TEXT NOT NULL, langfuse_trace_id TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME, finished_at DATETIME, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(run_id, case_id),
    CHECK((answerability_snapshot='answerable' AND reference_snapshot<>'{}' AND gold_evidence_snapshot<>'[]') OR
          (answerability_snapshot='unanswerable' AND reference_snapshot='{}' AND gold_evidence_snapshot='[]'))
);
CREATE INDEX idx_eval_run_items_scope ON evaluation_run_items(tenant_id, knowledge_base_id, run_id);

CREATE TABLE evaluation_metric_results (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES evaluation_runs(id) ON DELETE CASCADE,
    run_item_id TEXT NOT NULL REFERENCES evaluation_run_items(id) ON DELETE CASCADE,
    metric_name TEXT NOT NULL, metric_version TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('valid','invalid','abstain')), value REAL,
    reason_code TEXT NOT NULL DEFAULT '', details TEXT NOT NULL DEFAULT '{}', judge_execution TEXT NOT NULL DEFAULT '{}',
    langfuse_score_id TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(run_item_id, metric_name, metric_version),
    CHECK((status='valid' AND value IS NOT NULL AND value BETWEEN 0 AND 1) OR (status<>'valid' AND value IS NULL))
);
CREATE INDEX idx_eval_metrics_scope ON evaluation_metric_results(tenant_id, knowledge_base_id, run_id);

CREATE TABLE evaluation_schedules (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    name TEXT NOT NULL, cron_expression TEXT NOT NULL, timezone TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1, skip_if_active INTEGER NOT NULL DEFAULT 1, archived_at DATETIME,
    run_template TEXT NOT NULL, next_run_at DATETIME, last_run_status TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_eval_schedules_scope ON evaluation_schedules(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_schedule_slots (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    schedule_id TEXT NOT NULL REFERENCES evaluation_schedules(id) ON DELETE CASCADE,
    scheduled_at_utc DATETIME NOT NULL, status TEXT NOT NULL CHECK(status IN ('claimed','run_created','skipped_active','failed')),
    run_id TEXT REFERENCES evaluation_runs(id) ON DELETE SET NULL, reason_code TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, finished_at DATETIME, UNIQUE(schedule_id, scheduled_at_utc)
);

CREATE TABLE evaluation_judge_calibrations (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    version TEXT NOT NULL, metric_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('draft','running','passed','failed','canceled')),
    model_snapshot TEXT NOT NULL, prompt_version TEXT NOT NULL, prompt_hash TEXT NOT NULL, parser_version TEXT NOT NULL,
    report TEXT NOT NULL DEFAULT '{}', passed_at DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(tenant_id, knowledge_base_id, version, metric_name)
);
CREATE INDEX idx_eval_calibrations_scope ON evaluation_judge_calibrations(tenant_id, knowledge_base_id);

CREATE TABLE evaluation_failure_diagnoses (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL UNIQUE REFERENCES evaluation_runs(id) ON DELETE RESTRICT,
    version TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('pending','running','completed','failed')),
    classification TEXT NOT NULL CHECK(classification IN ('chunking_likely','non_chunking_likely','unknown')),
    reason_code TEXT NOT NULL, evidence_signals TEXT NOT NULL, target_k INTEGER NOT NULL CHECK(target_k > 0),
    valid_cases INTEGER NOT NULL DEFAULT 0, error_code TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_eval_diagnoses_scope ON evaluation_failure_diagnoses(tenant_id, knowledge_base_id);

CREATE TABLE evaluation_chunk_experiments (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL UNIQUE REFERENCES evaluation_runs(id) ON DELETE RESTRICT,
    diagnosis_id TEXT NOT NULL REFERENCES evaluation_failure_diagnoses(id) ON DELETE RESTRICT,
    testset_version_id TEXT NOT NULL REFERENCES evaluation_testset_versions(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK(status IN ('draft','running','completed','failed','canceled')),
    idempotency_key TEXT NOT NULL, fixed_snapshot TEXT NOT NULL, provisional_variant_id TEXT,
    estimated_calls INTEGER NOT NULL DEFAULT 0, cleanup_status TEXT NOT NULL CHECK(cleanup_status IN ('not_started','pending','completed','failed')),
    error_code TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, started_at DATETIME, finished_at DATETIME,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(tenant_id, idempotency_key)
);
CREATE INDEX idx_eval_experiments_scope ON evaluation_chunk_experiments(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_chunk_experiment_variants (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    experiment_id TEXT NOT NULL REFERENCES evaluation_chunk_experiments(id) ON DELETE CASCADE,
    variant_key TEXT NOT NULL, role TEXT NOT NULL CHECK(role IN ('baseline','candidate')),
    chunking_snapshot TEXT NOT NULL, config_hash TEXT NOT NULL, source_mapping TEXT NOT NULL,
    execution_knowledge_base_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL CHECK(status IN ('pending','building','running','completed','failed','canceled')),
    tuning_metrics TEXT NOT NULL DEFAULT '{}', holdout_metrics TEXT NOT NULL DEFAULT '{}', resource_metrics TEXT NOT NULL DEFAULT '{}',
    cleanup_status TEXT NOT NULL CHECK(cleanup_status IN ('not_started','pending','completed','failed')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(experiment_id, variant_key)
);
CREATE UNIQUE INDEX uq_eval_one_baseline ON evaluation_chunk_experiment_variants(experiment_id) WHERE role='baseline';
CREATE TRIGGER trg_eval_limit_experiment_variants
BEFORE INSERT ON evaluation_chunk_experiment_variants
WHEN (SELECT COUNT(*) FROM evaluation_chunk_experiment_variants WHERE experiment_id=NEW.experiment_id) >= 3
BEGIN
  SELECT RAISE(ABORT, 'an evaluation experiment may contain at most three variants');
END;

CREATE TABLE evaluation_chunk_recommendations (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
    experiment_id TEXT NOT NULL UNIQUE REFERENCES evaluation_chunk_experiments(id) ON DELETE CASCADE,
    policy_version TEXT NOT NULL, decision TEXT NOT NULL CHECK(decision IN ('recommend','no_better_strategy')),
    baseline_variant_id TEXT NOT NULL REFERENCES evaluation_chunk_experiment_variants(id) ON DELETE RESTRICT,
    candidate_variant_id TEXT REFERENCES evaluation_chunk_experiment_variants(id) ON DELETE RESTRICT,
    metric_deltas TEXT NOT NULL DEFAULT '{}', evidence TEXT NOT NULL DEFAULT '{}', limitations TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK((decision='recommend' AND candidate_variant_id IS NOT NULL) OR decision='no_better_strategy')
);
