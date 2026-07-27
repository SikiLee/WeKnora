DO $$
DECLARE
    existing_object TEXT;
BEGIN
    SELECT object_name
    INTO existing_object
    FROM (
        SELECT column_name AS object_name
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'chunks'
          AND column_name IN (
              'like_count',
              'dislike_count',
              'positive_rate',
              'recall_weight',
              'needs_optimization',
              'feedback_reset_at',
              'feedback_updated_at'
          )
        UNION ALL
        SELECT target.object_name
        FROM (
            VALUES
                ('message_feedbacks'),
                ('idx_message_feedbacks_user_message'),
                ('idx_message_feedbacks_session'),
                ('idx_message_feedbacks_feedback_at'),
                ('message_chunk_references'),
                ('idx_message_chunk_refs_message_chunk'),
                ('idx_message_chunk_refs_chunk'),
                ('idx_message_chunk_refs_kb'),
                ('idx_message_chunk_refs_session'),
                ('chunk_feedback_weight_logs'),
                ('idx_chunk_feedback_weight_logs_chunk'),
                ('idx_chunk_feedback_weight_logs_created_at')
        ) AS target(object_name)
        WHERE to_regclass(format('%I.%I', current_schema(), target.object_name)) IS NOT NULL
    ) AS collisions
    LIMIT 1;

    IF existing_object IS NOT NULL THEN
        RAISE EXCEPTION 'answer feedback migration object already exists: %', existing_object
            USING ERRCODE = 'duplicate_object';
    END IF;
END
$$;

ALTER TABLE chunks ADD COLUMN like_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN dislike_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN positive_rate DOUBLE PRECISION;
ALTER TABLE chunks ADD COLUMN recall_weight DOUBLE PRECISION NOT NULL DEFAULT 1.0;
ALTER TABLE chunks ADD COLUMN needs_optimization BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE chunks ADD COLUMN feedback_reset_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE chunks ADD COLUMN feedback_updated_at TIMESTAMP WITH TIME ZONE;

CREATE TABLE message_feedbacks (
    id VARCHAR(36) PRIMARY KEY,
    session_tenant_id BIGINT NOT NULL,
    user_id VARCHAR(512) NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    message_id VARCHAR(36) NOT NULL,
    feedback_type VARCHAR(16) NOT NULL,
    reason_code VARCHAR(64) NOT NULL DEFAULT '',
    reason_text TEXT NOT NULL DEFAULT '',
    feedback_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_message_feedbacks_type CHECK (feedback_type IN ('like', 'dislike')),
    CONSTRAINT chk_message_feedbacks_reason_text_length CHECK (char_length(reason_text) <= 500)
);

CREATE UNIQUE INDEX idx_message_feedbacks_user_message
    ON message_feedbacks(session_tenant_id, user_id, message_id);
CREATE INDEX idx_message_feedbacks_session
    ON message_feedbacks(session_tenant_id, session_id, message_id);
CREATE INDEX idx_message_feedbacks_feedback_at
    ON message_feedbacks(feedback_at);

CREATE TABLE message_chunk_references (
    id VARCHAR(36) PRIMARY KEY,
    session_tenant_id BIGINT NOT NULL,
    chunk_tenant_id BIGINT NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    message_id VARCHAR(36) NOT NULL,
    chunk_id VARCHAR(36) NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL,
    reference_rank INTEGER NOT NULL DEFAULT 0,
    retrieval_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    match_type VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_message_chunk_refs_message_chunk
    ON message_chunk_references(session_tenant_id, message_id, chunk_tenant_id, chunk_id);
CREATE INDEX idx_message_chunk_refs_chunk
    ON message_chunk_references(chunk_tenant_id, chunk_id, created_at);
CREATE INDEX idx_message_chunk_refs_kb
    ON message_chunk_references(chunk_tenant_id, knowledge_base_id, created_at);
CREATE INDEX idx_message_chunk_refs_session
    ON message_chunk_references(session_tenant_id, session_id, message_id);

CREATE TABLE chunk_feedback_weight_logs (
    id VARCHAR(36) PRIMARY KEY,
    chunk_tenant_id BIGINT NOT NULL,
    chunk_id VARCHAR(36) NOT NULL,
    old_weight DOUBLE PRECISION NOT NULL,
    new_weight DOUBLE PRECISION NOT NULL,
    source VARCHAR(64) NOT NULL,
    source_action VARCHAR(64) NOT NULL,
    source_message_id VARCHAR(36) NOT NULL DEFAULT '',
    source_feedback_id VARCHAR(36) NOT NULL DEFAULT '',
    actor_tenant_id BIGINT NOT NULL DEFAULT 0,
    actor_user_id VARCHAR(512) NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_chunk_feedback_weight_logs_chunk
    ON chunk_feedback_weight_logs(chunk_tenant_id, chunk_id, created_at);
CREATE INDEX idx_chunk_feedback_weight_logs_created_at
    ON chunk_feedback_weight_logs(created_at);
