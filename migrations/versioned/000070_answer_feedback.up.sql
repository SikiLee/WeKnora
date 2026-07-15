ALTER TABLE chunks ADD COLUMN IF NOT EXISTS like_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN IF NOT EXISTS dislike_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN IF NOT EXISTS positive_rate DOUBLE PRECISION;
ALTER TABLE chunks ADD COLUMN IF NOT EXISTS recall_weight DOUBLE PRECISION NOT NULL DEFAULT 1.0;
ALTER TABLE chunks ADD COLUMN IF NOT EXISTS needs_optimization BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE chunks ADD COLUMN IF NOT EXISTS feedback_reset_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE chunks ADD COLUMN IF NOT EXISTS feedback_updated_at TIMESTAMP WITH TIME ZONE;

CREATE TABLE IF NOT EXISTS message_feedbacks (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_message_feedbacks_user_message
    ON message_feedbacks(session_tenant_id, user_id, message_id);
CREATE INDEX IF NOT EXISTS idx_message_feedbacks_session
    ON message_feedbacks(session_tenant_id, session_id, message_id);
CREATE INDEX IF NOT EXISTS idx_message_feedbacks_feedback_at
    ON message_feedbacks(feedback_at);

CREATE TABLE IF NOT EXISTS message_chunk_references (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_message_chunk_refs_message_chunk
    ON message_chunk_references(session_tenant_id, message_id, chunk_tenant_id, chunk_id);
CREATE INDEX IF NOT EXISTS idx_message_chunk_refs_chunk
    ON message_chunk_references(chunk_tenant_id, chunk_id, created_at);
CREATE INDEX IF NOT EXISTS idx_message_chunk_refs_kb
    ON message_chunk_references(chunk_tenant_id, knowledge_base_id, created_at);
CREATE INDEX IF NOT EXISTS idx_message_chunk_refs_session
    ON message_chunk_references(session_tenant_id, session_id, message_id);

CREATE TABLE IF NOT EXISTS chunk_feedback_weight_logs (
    id VARCHAR(36) PRIMARY KEY,
    chunk_tenant_id BIGINT NOT NULL,
    chunk_id VARCHAR(36) NOT NULL,
    old_weight DOUBLE PRECISION NOT NULL,
    new_weight DOUBLE PRECISION NOT NULL,
    source VARCHAR(64) NOT NULL,
    source_action VARCHAR(64) NOT NULL,
    source_message_id VARCHAR(36) NOT NULL DEFAULT '',
    source_feedback_id VARCHAR(36) NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chunk_feedback_weight_logs_chunk
    ON chunk_feedback_weight_logs(chunk_tenant_id, chunk_id, created_at);
CREATE INDEX IF NOT EXISTS idx_chunk_feedback_weight_logs_created_at
    ON chunk_feedback_weight_logs(created_at);
