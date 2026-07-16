-- RAG evaluation MVP. WeKnora remains the business source of truth; Langfuse
-- identifiers are optional mirrors only.
CREATE TABLE evaluation_testsets (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('active','archived')),
    current_published_version_id VARCHAR(36),
    created_by VARCHAR(36) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);
CREATE INDEX idx_eval_testsets_scope ON evaluation_testsets(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_testset_versions (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    testset_id VARCHAR(36) NOT NULL REFERENCES evaluation_testsets(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number > 0),
    status VARCHAR(32) NOT NULL CHECK (status IN ('draft','published','archived')),
    profile_locked BOOLEAN NOT NULL DEFAULT FALSE,
    generation_profile_hash VARCHAR(64) NOT NULL DEFAULT '',
    document_snapshots JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_normalization_version VARCHAR(64) NOT NULL DEFAULT '',
    offset_unit VARCHAR(32) NOT NULL DEFAULT '',
    split_algorithm VARCHAR(64) NOT NULL DEFAULT '',
    split_seed BIGINT NOT NULL DEFAULT 0,
    generator_model_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    generator_prompt_version VARCHAR(128) NOT NULL DEFAULT '',
    generator_prompt_hash VARCHAR(64) NOT NULL DEFAULT '',
    created_by VARCHAR(36) NOT NULL,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_eval_version_no UNIQUE(testset_id, version_number),
    CONSTRAINT ck_eval_version_published_at CHECK ((status = 'published') = (published_at IS NOT NULL))
);
CREATE INDEX idx_eval_versions_scope ON evaluation_testset_versions(tenant_id, knowledge_base_id, testset_id);
ALTER TABLE evaluation_testsets ADD CONSTRAINT fk_eval_current_version
    FOREIGN KEY (current_published_version_id) REFERENCES evaluation_testset_versions(id) ON DELETE SET NULL;

CREATE TABLE evaluation_cases (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    version_id VARCHAR(36) NOT NULL REFERENCES evaluation_testset_versions(id) ON DELETE CASCADE,
    question TEXT NOT NULL,
    answerability VARCHAR(32) NOT NULL CHECK (answerability IN ('answerable','unanswerable')),
    question_type VARCHAR(32) NOT NULL CHECK (question_type IN ('single_evidence','multi_evidence','unanswerable')),
    difficulty VARCHAR(32) NOT NULL CHECK (difficulty IN ('easy','medium','hard')),
    reference_answer TEXT NOT NULL DEFAULT '',
    reference_key_points JSONB NOT NULL DEFAULT '[]'::jsonb,
    expected_refusal TEXT NOT NULL DEFAULT '',
    unanswerable_reason TEXT NOT NULL DEFAULT '',
    checked_scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    review_status VARCHAR(32) NOT NULL CHECK (review_status IN ('pending','approved','rejected')),
    quality_status VARCHAR(32) NOT NULL CHECK (quality_status IN ('pending','passed','failed')),
    quality_reason_codes JSONB NOT NULL DEFAULT '[]'::jsonb,
    split VARCHAR(16) NOT NULL CHECK (split IN ('tuning','holdout')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    generation_provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_eval_case_answerability CHECK (
      (answerability = 'answerable' AND question_type IN ('single_evidence','multi_evidence') AND reference_answer <> '' AND expected_refusal = '' AND unanswerable_reason = '')
      OR
      (answerability = 'unanswerable' AND question_type = 'unanswerable' AND reference_answer = '' AND expected_refusal <> '' AND unanswerable_reason <> '')
    )
);
CREATE INDEX idx_eval_cases_scope ON evaluation_cases(tenant_id, knowledge_base_id, version_id);

CREATE TABLE evaluation_gold_evidences (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    case_id VARCHAR(36) NOT NULL REFERENCES evaluation_cases(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal IN (1,2)),
    source_document_id VARCHAR(36) NOT NULL,
    source_document_hash VARCHAR(64) NOT NULL,
    normalized_source_hash VARCHAR(64) NOT NULL,
    source_normalization_version VARCHAR(64) NOT NULL,
    offset_unit VARCHAR(32) NOT NULL,
    start_at INTEGER NOT NULL CHECK (start_at >= 0),
    end_at INTEGER NOT NULL,
    evidence_text TEXT NOT NULL,
    evidence_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_eval_evidence_ordinal UNIQUE(case_id, ordinal),
    CONSTRAINT ck_eval_evidence_interval CHECK (end_at > start_at)
);
CREATE INDEX idx_eval_evidence_scope ON evaluation_gold_evidences(tenant_id, knowledge_base_id, case_id);

CREATE TABLE evaluation_testset_generations (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    testset_id VARCHAR(36) NOT NULL REFERENCES evaluation_testsets(id) ON DELETE CASCADE,
    version_id VARCHAR(36) NOT NULL REFERENCES evaluation_testset_versions(id) ON DELETE CASCADE,
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending','running','completed','partial','failed','canceled','blocked')),
    idempotency_key VARCHAR(128) NOT NULL,
    profile_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    requested_cases INTEGER NOT NULL CHECK (requested_cases BETWEEN 1 AND 200),
    accepted_cases INTEGER NOT NULL DEFAULT 0 CHECK (accepted_cases >= 0),
    quality_counts JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    CONSTRAINT uq_eval_generation_idem UNIQUE(tenant_id, idempotency_key)
);
CREATE INDEX idx_eval_generations_scope ON evaluation_testset_generations(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_runs (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    testset_version_id VARCHAR(36) NOT NULL REFERENCES evaluation_testset_versions(id) ON DELETE RESTRICT,
    parent_run_id VARCHAR(36) REFERENCES evaluation_runs(id) ON DELETE RESTRICT,
    trigger_type VARCHAR(32) NOT NULL CHECK (trigger_type IN ('manual','schedule','retry','experiment')),
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending','running','completed','partial','failed','canceled','blocked')),
    stage VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    snapshot JSONB NOT NULL,
    sampled_case_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    total_items INTEGER NOT NULL CHECK (total_items BETWEEN 1 AND 200),
    completed_items INTEGER NOT NULL DEFAULT 0 CHECK (completed_items >= 0),
    failed_items INTEGER NOT NULL DEFAULT 0 CHECK (failed_items >= 0),
    blocked_reason_code VARCHAR(64) NOT NULL DEFAULT '',
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    observability_status VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (observability_status IN ('pending','available','partial','unavailable','disabled')),
    langfuse_trace_id VARCHAR(128) NOT NULL DEFAULT '',
    estimated_calls INTEGER NOT NULL DEFAULT 0 CHECK (estimated_calls >= 0),
    created_by VARCHAR(36) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_eval_run_idem UNIQUE(tenant_id, idempotency_key),
    CONSTRAINT ck_eval_run_progress CHECK (completed_items + failed_items <= total_items)
);
CREATE INDEX idx_eval_runs_scope ON evaluation_runs(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_run_items (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    run_id VARCHAR(36) NOT NULL REFERENCES evaluation_runs(id) ON DELETE CASCADE,
    case_id VARCHAR(36) NOT NULL REFERENCES evaluation_cases(id) ON DELETE RESTRICT,
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending','running','completed','failed','canceled','blocked')),
    answerability_snapshot VARCHAR(32) NOT NULL CHECK (answerability_snapshot IN ('answerable','unanswerable')),
    question_snapshot TEXT NOT NULL,
    reference_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    gold_evidence_snapshot JSONB NOT NULL DEFAULT '[]'::jsonb,
    retrieved_contexts JSONB NOT NULL DEFAULT '[]'::jsonb,
    generated_answer TEXT NOT NULL DEFAULT '',
    citations JSONB NOT NULL DEFAULT '[]'::jsonb,
    latency JSONB NOT NULL DEFAULT '{}'::jsonb,
    tokens JSONB NOT NULL DEFAULT '{}'::jsonb,
    cost JSONB NOT NULL DEFAULT '{}'::jsonb,
    trace_correlation_id VARCHAR(128) NOT NULL,
    langfuse_trace_id VARCHAR(128) NOT NULL DEFAULT '',
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_eval_run_case UNIQUE(run_id, case_id),
    CONSTRAINT ck_eval_item_snapshots CHECK (
      (answerability_snapshot = 'answerable' AND reference_snapshot <> '{}'::jsonb AND gold_evidence_snapshot <> '[]'::jsonb)
      OR
      (answerability_snapshot = 'unanswerable' AND reference_snapshot = '{}'::jsonb AND gold_evidence_snapshot = '[]'::jsonb)
    )
);
CREATE INDEX idx_eval_run_items_scope ON evaluation_run_items(tenant_id, knowledge_base_id, run_id);

CREATE TABLE evaluation_metric_results (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    run_id VARCHAR(36) NOT NULL REFERENCES evaluation_runs(id) ON DELETE CASCADE,
    run_item_id VARCHAR(36) NOT NULL REFERENCES evaluation_run_items(id) ON DELETE CASCADE,
    metric_name VARCHAR(64) NOT NULL,
    metric_version VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('valid','invalid','abstain')),
    value DOUBLE PRECISION,
    reason_code VARCHAR(64) NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    judge_execution JSONB NOT NULL DEFAULT '{}'::jsonb,
    langfuse_score_id VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_eval_item_metric UNIQUE(run_item_id, metric_name, metric_version),
    CONSTRAINT ck_eval_metric_value CHECK ((status = 'valid' AND value IS NOT NULL AND value BETWEEN 0 AND 1) OR (status <> 'valid' AND value IS NULL))
);
CREATE INDEX idx_eval_metrics_scope ON evaluation_metric_results(tenant_id, knowledge_base_id, run_id);

CREATE TABLE evaluation_schedules (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    cron_expression VARCHAR(128) NOT NULL,
    timezone VARCHAR(64) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    skip_if_active BOOLEAN NOT NULL DEFAULT TRUE,
    archived_at TIMESTAMPTZ,
    run_template JSONB NOT NULL,
    next_run_at TIMESTAMPTZ,
    last_run_status VARCHAR(32) NOT NULL DEFAULT '',
    created_by VARCHAR(36) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_eval_schedules_scope ON evaluation_schedules(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_schedule_slots (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    schedule_id VARCHAR(36) NOT NULL REFERENCES evaluation_schedules(id) ON DELETE CASCADE,
    scheduled_at_utc TIMESTAMPTZ NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('claimed','run_created','skipped_active','failed')),
    run_id VARCHAR(36) REFERENCES evaluation_runs(id) ON DELETE SET NULL,
    reason_code VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    CONSTRAINT uq_eval_schedule_slot UNIQUE(schedule_id, scheduled_at_utc)
);

CREATE TABLE evaluation_judge_calibrations (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    version VARCHAR(128) NOT NULL,
    metric_name VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('draft','running','passed','failed','canceled')),
    model_snapshot JSONB NOT NULL,
    prompt_version VARCHAR(128) NOT NULL,
    prompt_hash VARCHAR(64) NOT NULL,
    parser_version VARCHAR(64) NOT NULL,
    report JSONB NOT NULL DEFAULT '{}'::jsonb,
    passed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_eval_calibration_version UNIQUE(tenant_id, knowledge_base_id, version, metric_name)
);
CREATE INDEX idx_eval_calibrations_scope ON evaluation_judge_calibrations(tenant_id, knowledge_base_id);

CREATE TABLE evaluation_failure_diagnoses (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    source_run_id VARCHAR(36) NOT NULL UNIQUE REFERENCES evaluation_runs(id) ON DELETE RESTRICT,
    version VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending','running','completed','failed')),
    classification VARCHAR(32) NOT NULL CHECK (classification IN ('chunking_likely','non_chunking_likely','unknown')),
    reason_code VARCHAR(64) NOT NULL,
    evidence_signals JSONB NOT NULL,
    target_k INTEGER NOT NULL CHECK (target_k > 0),
    valid_cases INTEGER NOT NULL CHECK (valid_cases >= 0),
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_eval_diagnoses_scope ON evaluation_failure_diagnoses(tenant_id, knowledge_base_id);

CREATE TABLE evaluation_chunk_experiments (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    source_run_id VARCHAR(36) NOT NULL UNIQUE REFERENCES evaluation_runs(id) ON DELETE RESTRICT,
    diagnosis_id VARCHAR(36) NOT NULL REFERENCES evaluation_failure_diagnoses(id) ON DELETE RESTRICT,
    testset_version_id VARCHAR(36) NOT NULL REFERENCES evaluation_testset_versions(id) ON DELETE RESTRICT,
    status VARCHAR(32) NOT NULL CHECK (status IN ('draft','running','completed','failed','canceled')),
    idempotency_key VARCHAR(128) NOT NULL,
    fixed_snapshot JSONB NOT NULL,
    provisional_variant_id VARCHAR(36),
    estimated_calls INTEGER NOT NULL CHECK (estimated_calls >= 0),
    cleanup_status VARCHAR(32) NOT NULL CHECK (cleanup_status IN ('not_started','pending','completed','failed')),
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_by VARCHAR(36) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_eval_experiment_idem UNIQUE(tenant_id, idempotency_key)
);
CREATE INDEX idx_eval_experiments_scope ON evaluation_chunk_experiments(tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE evaluation_chunk_experiment_variants (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    experiment_id VARCHAR(36) NOT NULL REFERENCES evaluation_chunk_experiments(id) ON DELETE CASCADE,
    variant_key VARCHAR(32) NOT NULL,
    role VARCHAR(16) NOT NULL CHECK (role IN ('baseline','candidate')),
    chunking_snapshot JSONB NOT NULL,
    config_hash VARCHAR(64) NOT NULL,
    source_mapping JSONB NOT NULL,
    execution_knowledge_base_id VARCHAR(36) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending','building','running','completed','failed','canceled')),
    tuning_metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    holdout_metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    resource_metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    cleanup_status VARCHAR(32) NOT NULL CHECK (cleanup_status IN ('not_started','pending','completed','failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_eval_variant_key UNIQUE(experiment_id, variant_key)
);
CREATE UNIQUE INDEX uq_eval_one_baseline ON evaluation_chunk_experiment_variants(experiment_id) WHERE role = 'baseline';

CREATE FUNCTION evaluation_limit_experiment_variants() RETURNS trigger AS $$
BEGIN
  IF (SELECT COUNT(*) FROM evaluation_chunk_experiment_variants WHERE experiment_id = NEW.experiment_id) >= 3 THEN
    RAISE EXCEPTION 'an evaluation experiment may contain at most three variants';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_eval_limit_experiment_variants
  BEFORE INSERT ON evaluation_chunk_experiment_variants
  FOR EACH ROW EXECUTE FUNCTION evaluation_limit_experiment_variants();

CREATE TABLE evaluation_chunk_recommendations (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    experiment_id VARCHAR(36) NOT NULL UNIQUE REFERENCES evaluation_chunk_experiments(id) ON DELETE CASCADE,
    policy_version VARCHAR(64) NOT NULL,
    decision VARCHAR(32) NOT NULL CHECK (decision IN ('recommend','no_better_strategy')),
    baseline_variant_id VARCHAR(36) NOT NULL REFERENCES evaluation_chunk_experiment_variants(id) ON DELETE RESTRICT,
    candidate_variant_id VARCHAR(36) REFERENCES evaluation_chunk_experiment_variants(id) ON DELETE RESTRICT,
    metric_deltas JSONB NOT NULL DEFAULT '{}'::jsonb,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    limitations JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_eval_recommend_candidate CHECK ((decision = 'recommend' AND candidate_variant_id IS NOT NULL) OR decision = 'no_better_strategy')
);

ALTER TABLE evaluation_chunk_experiments ADD CONSTRAINT fk_eval_provisional_variant
  FOREIGN KEY (provisional_variant_id) REFERENCES evaluation_chunk_experiment_variants(id) ON DELETE SET NULL;
