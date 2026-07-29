ALTER TABLE chunk_feedback_audits
    ADD COLUMN IF NOT EXISTS trigger_source VARCHAR(16) NOT NULL DEFAULT 'legacy';

ALTER TABLE chunk_feedback_audits
    ADD CONSTRAINT chk_chunk_feedback_audit_trigger_source CHECK (
        trigger_source IN ('like', 'dislike', 'cancel', 'admin_reset', 'content_delete', 'legacy')
    );
