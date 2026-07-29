ALTER TABLE chunk_feedback_audits
    DROP CONSTRAINT IF EXISTS chk_chunk_feedback_audit_trigger_source;

ALTER TABLE chunk_feedback_audits
    DROP COLUMN IF EXISTS trigger_source;
