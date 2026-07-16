DROP TABLE chunk_feedback_weight_logs;
DROP TABLE message_chunk_references;
DROP TABLE message_feedbacks;

ALTER TABLE chunks DROP COLUMN feedback_updated_at;
ALTER TABLE chunks DROP COLUMN feedback_reset_at;
ALTER TABLE chunks DROP COLUMN needs_optimization;
ALTER TABLE chunks DROP COLUMN recall_weight;
ALTER TABLE chunks DROP COLUMN positive_rate;
ALTER TABLE chunks DROP COLUMN dislike_count;
ALTER TABLE chunks DROP COLUMN like_count;
