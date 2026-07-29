ALTER TABLE chunk_feedback_audits
    ADD COLUMN trigger_source VARCHAR(16) NOT NULL DEFAULT 'legacy'
    CHECK (trigger_source IN ('like', 'dislike', 'cancel', 'admin_reset', 'content_delete', 'legacy'));
