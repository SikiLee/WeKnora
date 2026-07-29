package types

import "time"

type FeedbackType string

const (
	FeedbackTypeLike    FeedbackType = "like"
	FeedbackTypeDislike FeedbackType = "dislike"
	FeedbackTypeNone    FeedbackType = "none"
)

type FeedbackReasonCode string

const (
	FeedbackReasonInaccurate FeedbackReasonCode = "inaccurate"
	FeedbackReasonIrrelevant FeedbackReasonCode = "irrelevant"
	FeedbackReasonIncomplete FeedbackReasonCode = "incomplete"
	FeedbackReasonOutdated   FeedbackReasonCode = "outdated"
	FeedbackReasonOther      FeedbackReasonCode = "other"
)

type MessageFeedback struct {
	ID               string              `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64              `json:"tenant_id" gorm:"not null;uniqueIndex:idx_message_feedback_actor"`
	UserID           string              `json:"user_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_message_feedback_actor"`
	SessionID        string              `json:"session_id" gorm:"type:varchar(36);not null;index:idx_message_feedback_session"`
	MessageID        string              `json:"message_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_message_feedback_actor;index:idx_message_feedback_message"`
	FeedbackType     FeedbackType        `json:"type" gorm:"column:feedback_type;type:varchar(16);not null"`
	ReasonCode       *FeedbackReasonCode `json:"reason_code,omitempty" gorm:"type:varchar(16)"`
	FeedbackRevision int64               `json:"feedback_revision" gorm:"not null;default:0"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

func (MessageFeedback) TableName() string { return "message_feedbacks" }

type MessageChunkReference struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	MessageTenantID uint64    `json:"message_tenant_id" gorm:"not null;uniqueIndex:idx_message_chunk_reference;index:idx_message_reference_message"`
	ChunkTenantID   uint64    `json:"chunk_tenant_id" gorm:"not null;uniqueIndex:idx_message_chunk_reference;index:idx_message_reference_chunk"`
	MessageID       string    `json:"message_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_message_chunk_reference;index:idx_message_reference_message"`
	ChunkID         string    `json:"chunk_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_message_chunk_reference;index:idx_message_reference_chunk"`
	CreatedAt       time.Time `json:"created_at"`
}

func (MessageChunkReference) TableName() string { return "message_chunk_references" }

type ChunkFeedbackAudit struct {
	ID            uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ChunkTenantID uint64    `json:"chunk_tenant_id" gorm:"not null;index:idx_chunk_feedback_audit_chunk"`
	ChunkID       string    `json:"chunk_id" gorm:"type:varchar(36);not null;index:idx_chunk_feedback_audit_chunk"`
	ActorTenantID uint64    `json:"-" gorm:"not null"`
	ActorUserID   string    `json:"-" gorm:"type:varchar(64);not null"`
	Action        string    `json:"action" gorm:"type:varchar(32);not null"`
	OldWeight     float64   `json:"old_weight" gorm:"not null"`
	NewWeight     float64   `json:"new_weight" gorm:"not null"`
	CreatedAt     time.Time `json:"created_at"`
}

func (ChunkFeedbackAudit) TableName() string { return "chunk_feedback_audits" }

type MessageFeedbackState struct {
	Type       FeedbackType        `json:"type"`
	ReasonCode *FeedbackReasonCode `json:"reason_code,omitempty"`
}

type ApplyMessageFeedbackInput struct {
	MessageTenantID uint64
	ActorTenantID   uint64
	ActorUserID     string
	SessionID       string
	MessageID       string
	Type            FeedbackType
	ReasonCode      *FeedbackReasonCode
}

type ResetChunkFeedbackInput struct {
	ChunkTenantID   uint64
	ActorTenantID   uint64
	ActorUserID     string
	KnowledgeBaseID string
	ChunkID         string
}

type ChunkFeedbackDetails struct {
	ReasonCounts map[FeedbackReasonCode]int64 `json:"reason_counts"`
	Audits       []*ChunkFeedbackAudit        `json:"audits"`
}
