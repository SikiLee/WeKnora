ALTER TABLE chunks ADD COLUMN like_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN dislike_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN positive_rate REAL;
ALTER TABLE chunks ADD COLUMN recall_weight REAL NOT NULL DEFAULT 1.0;
ALTER TABLE chunks ADD COLUMN needs_optimization BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN feedback_reset_at DATETIME;
ALTER TABLE chunks ADD COLUMN feedback_updated_at DATETIME;

CREATE TABLE IF NOT EXISTS message_feedbacks (
    id VARCHAR(36) PRIMARY KEY,
    session_tenant_id INTEGER NOT NULL,
    user_id VARCHAR(512) NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    message_id VARCHAR(36) NOT NULL,
    feedback_type VARCHAR(16) NOT NULL,
    reason_code VARCHAR(64) NOT NULL DEFAULT '',
    reason_text TEXT NOT NULL DEFAULT '',
    feedback_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (feedback_type IN ('like', 'dislike')),
    CHECK (length(reason_text) <= 500)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_message_feedbacks_user_message
    ON message_feedbacks(session_tenant_id, user_id, message_id);
CREATE INDEX IF NOT EXISTS idx_message_feedbacks_session
    ON message_feedbacks(session_tenant_id, session_id, message_id);
CREATE INDEX IF NOT EXISTS idx_message_feedbacks_feedback_at
    ON message_feedbacks(feedback_at);

CREATE TABLE IF NOT EXISTS message_chunk_references (
    id VARCHAR(36) PRIMARY KEY,
    session_tenant_id INTEGER NOT NULL,
    chunk_tenant_id INTEGER NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    message_id VARCHAR(36) NOT NULL,
    chunk_id VARCHAR(36) NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL,
    reference_rank INTEGER NOT NULL DEFAULT 0,
    retrieval_score REAL NOT NULL DEFAULT 0,
    match_type VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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
    chunk_tenant_id INTEGER NOT NULL,
    chunk_id VARCHAR(36) NOT NULL,
    old_weight REAL NOT NULL,
    new_weight REAL NOT NULL,
    source VARCHAR(64) NOT NULL,
    source_action VARCHAR(64) NOT NULL,
    source_message_id VARCHAR(36) NOT NULL DEFAULT '',
    source_feedback_id VARCHAR(36) NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chunk_feedback_weight_logs_chunk
    ON chunk_feedback_weight_logs(chunk_tenant_id, chunk_id, created_at);
CREATE INDEX IF NOT EXISTS idx_chunk_feedback_weight_logs_created_at
    ON chunk_feedback_weight_logs(created_at);
