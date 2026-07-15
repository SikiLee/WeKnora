package types

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrFeedbackUnauthorized      = errors.New("feedback caller is not authorized")
	ErrFeedbackMessageNotFound   = errors.New("feedback message was not found")
	ErrFeedbackMessageIncomplete = errors.New("only completed assistant messages can be rated")
	ErrChunkFeedbackNotFound     = errors.New("chunk feedback target was not found")
)

const (
	FeedbackTypeLike    = "like"
	FeedbackTypeDislike = "dislike"
	FeedbackTypeNone    = "none"

	FeedbackReasonIncorrect  = "incorrect"
	FeedbackReasonOutdated   = "outdated"
	FeedbackReasonIrrelevant = "irrelevant"
	FeedbackReasonIncomplete = "incomplete"
	FeedbackReasonUnclear    = "unclear"
	FeedbackReasonOther      = "other"

	ChunkFeedbackLogSourceUserFeedback = "user_feedback"
	ChunkFeedbackLogSourceAdminReset   = "admin_reset"

	ChunkFeedbackLogActionLike    = "like"
	ChunkFeedbackLogActionDislike = "dislike"
	ChunkFeedbackLogActionCancel  = "cancel"
	ChunkFeedbackLogActionReset   = "reset"

	ChunkFeedbackStatusAll     = "all"
	ChunkFeedbackStatusRated   = "rated"
	ChunkFeedbackStatusHigh    = "high"
	ChunkFeedbackStatusNormal  = "normal"
	ChunkFeedbackStatusLow     = "low"
	ChunkFeedbackStatusUnrated = "unrated"
)

// ChunkFeedbackConfig controls how answer feedback changes chunk recall weight.
type ChunkFeedbackConfig struct {
	HighRateThreshold     float64 `yaml:"high_rate_threshold"     json:"high_rate_threshold"`
	LowRateThreshold      float64 `yaml:"low_rate_threshold"      json:"low_rate_threshold"`
	OptimizationThreshold float64 `yaml:"optimization_threshold" json:"optimization_threshold"`
	HighRecallWeight      float64 `yaml:"high_recall_weight"      json:"high_recall_weight"`
	NormalRecallWeight    float64 `yaml:"normal_recall_weight"    json:"normal_recall_weight"`
	LowRecallWeight       float64 `yaml:"low_recall_weight"       json:"low_recall_weight"`
}

func DefaultChunkFeedbackConfig() *ChunkFeedbackConfig {
	return &ChunkFeedbackConfig{
		HighRateThreshold:     0.80,
		LowRateThreshold:      0.50,
		OptimizationThreshold: 0.20,
		HighRecallWeight:      1.20,
		NormalRecallWeight:    1.00,
		LowRecallWeight:       0.80,
	}
}

func (c *ChunkFeedbackConfig) Validate() error {
	if c == nil {
		return nil
	}
	if !isFinite(c.HighRateThreshold) || !isFinite(c.LowRateThreshold) || !isFinite(c.OptimizationThreshold) ||
		!isFinite(c.HighRecallWeight) || !isFinite(c.NormalRecallWeight) || !isFinite(c.LowRecallWeight) {
		return fmt.Errorf("feedback thresholds and recall weights must be finite")
	}
	if c.HighRateThreshold < 0 || c.HighRateThreshold > 1 ||
		c.LowRateThreshold < 0 || c.LowRateThreshold > 1 ||
		c.OptimizationThreshold < 0 || c.OptimizationThreshold > 1 {
		return fmt.Errorf("feedback thresholds must be between 0 and 1")
	}
	if !(c.HighRateThreshold > c.LowRateThreshold && c.LowRateThreshold > c.OptimizationThreshold) {
		return fmt.Errorf("feedback thresholds must satisfy high_rate_threshold > low_rate_threshold > optimization_threshold")
	}
	if c.HighRecallWeight <= 0 || c.NormalRecallWeight <= 0 || c.LowRecallWeight <= 0 {
		return fmt.Errorf("feedback recall weights must be positive")
	}
	if !(c.HighRecallWeight >= c.NormalRecallWeight && c.NormalRecallWeight >= c.LowRecallWeight) {
		return fmt.Errorf("feedback recall weights must satisfy high_recall_weight >= normal_recall_weight >= low_recall_weight")
	}
	return nil
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// CalculateChunkFeedback derives aggregate rate, recall weight, and optimization flag.
func CalculateChunkFeedback(likeCount, dislikeCount int64, cfg *ChunkFeedbackConfig) (positiveRate *float64, recallWeight float64, needsOptimization bool) {
	if cfg == nil {
		cfg = DefaultChunkFeedbackConfig()
	}
	total := likeCount + dislikeCount
	if total <= 0 {
		return nil, cfg.NormalRecallWeight, false
	}

	rate := float64(likeCount) / float64(total)
	positiveRate = &rate
	recallWeight = cfg.NormalRecallWeight
	switch {
	case rate >= cfg.HighRateThreshold:
		recallWeight = cfg.HighRecallWeight
	case rate < cfg.LowRateThreshold:
		recallWeight = cfg.LowRecallWeight
	}
	needsOptimization = rate <= cfg.OptimizationThreshold
	return positiveRate, recallWeight, needsOptimization
}

// MessageFeedback stores the active user rating for a completed assistant message.
type MessageFeedback struct {
	ID              string `json:"id"                gorm:"type:varchar(36);primaryKey"`
	SessionTenantID uint64 `json:"session_tenant_id" gorm:"index;not null"`
	UserID          string `json:"user_id"           gorm:"type:varchar(512);not null"`
	SessionID       string `json:"session_id"        gorm:"type:varchar(36);index;not null"`
	MessageID       string `json:"message_id"        gorm:"type:varchar(36);index;not null"`
	FeedbackType    string `json:"feedback_type"     gorm:"type:varchar(16);not null"`
	ReasonCode      string `json:"reason_code"       gorm:"type:varchar(64);not null;default:''"`
	ReasonText      string `json:"reason_text"       gorm:"type:text;not null;default:''"`
	FeedbackAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// MessageFeedbackInput is the caller-controlled state transition for one answer.
type MessageFeedbackInput struct {
	FeedbackType string `json:"feedback_type"`
	ReasonCode   string `json:"reason_code,omitempty"`
	ReasonText   string `json:"reason_text,omitempty"`
}

func (i *MessageFeedbackInput) Validate() error {
	if i == nil {
		return errors.New("feedback input is required")
	}
	i.FeedbackType = strings.TrimSpace(i.FeedbackType)
	i.ReasonCode = strings.TrimSpace(i.ReasonCode)
	i.ReasonText = strings.TrimSpace(i.ReasonText)
	if utf8.RuneCountInString(i.ReasonText) > 500 {
		return errors.New("feedback reason_text must not exceed 500 characters")
	}

	switch i.FeedbackType {
	case FeedbackTypeLike, FeedbackTypeNone:
		if i.ReasonCode != "" || i.ReasonText != "" {
			return errors.New("feedback reasons are only valid for dislike")
		}
	case FeedbackTypeDislike:
		switch i.ReasonCode {
		case FeedbackReasonIncorrect, FeedbackReasonOutdated, FeedbackReasonIrrelevant,
			FeedbackReasonIncomplete, FeedbackReasonUnclear:
			if i.ReasonText != "" {
				return errors.New("reason_text is only valid with reason_code=other")
			}
		case FeedbackReasonOther:
			// Free text is optional for "other", but is length-bounded above.
		default:
			return errors.New("invalid dislike reason_code")
		}
	default:
		return errors.New("feedback_type must be like, dislike, or none")
	}
	return nil
}

// MessageFeedbackState is the safe feedback shape attached to message history.
type MessageFeedbackState struct {
	FeedbackType string    `json:"feedback_type"`
	ReasonCode   string    `json:"reason_code,omitempty"`
	ReasonText   string    `json:"reason_text,omitempty"`
	FeedbackAt   time.Time `json:"feedback_at"`
}

// MessageFeedbackMutation contains the trusted fields used by the repository transaction.
type MessageFeedbackMutation struct {
	SessionTenantID uint64
	UserID          string
	SessionID       string
	MessageID       string
	FeedbackType    string
	ReasonCode      string
	ReasonText      string
}

func (m *MessageFeedback) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return nil
}

// MessageChunkReference is the durable attribution from an assistant answer to retrieved chunks.
type MessageChunkReference struct {
	ID              string  `json:"id"                gorm:"type:varchar(36);primaryKey"`
	SessionTenantID uint64  `json:"session_tenant_id" gorm:"index;not null"`
	ChunkTenantID   uint64  `json:"chunk_tenant_id"   gorm:"index;not null"`
	SessionID       string  `json:"session_id"        gorm:"type:varchar(36);index;not null"`
	MessageID       string  `json:"message_id"        gorm:"type:varchar(36);index;not null"`
	ChunkID         string  `json:"chunk_id"          gorm:"type:varchar(36);index;not null"`
	KnowledgeBaseID string  `json:"knowledge_base_id" gorm:"type:varchar(36);index;not null"`
	KnowledgeID     string  `json:"knowledge_id"      gorm:"type:varchar(36);index;not null"`
	ReferenceRank   int     `json:"reference_rank"    gorm:"not null;default:0"`
	RetrievalScore  float64 `json:"retrieval_score"   gorm:"not null;default:0"`
	MatchType       string  `json:"match_type"        gorm:"type:varchar(64);not null;default:''"`
	CreatedAt       time.Time
}

func (m *MessageChunkReference) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return nil
}

// ChunkFeedbackWeightLog records every governance-visible recall weight change.
type ChunkFeedbackWeightLog struct {
	ID               string  `json:"id"                 gorm:"type:varchar(36);primaryKey"`
	ChunkTenantID    uint64  `json:"chunk_tenant_id"    gorm:"index;not null"`
	ChunkID          string  `json:"chunk_id"           gorm:"type:varchar(36);index;not null"`
	OldWeight        float64 `json:"old_weight"         gorm:"not null"`
	NewWeight        float64 `json:"new_weight"         gorm:"not null"`
	Source           string  `json:"source"             gorm:"type:varchar(64);not null"`
	SourceAction     string  `json:"source_action"      gorm:"type:varchar(64);not null"`
	SourceMessageID  string  `json:"source_message_id"  gorm:"type:varchar(36);not null;default:''"`
	SourceFeedbackID string  `json:"source_feedback_id" gorm:"type:varchar(36);not null;default:''"`
	Reason           string  `json:"reason"             gorm:"type:text;not null;default:''"`
	CreatedAt        time.Time
}

func (c *ChunkFeedbackWeightLog) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	return nil
}

// ChunkFeedbackListQuery controls the governance list. Sort fields are
// validated before reaching the repository so they can be safely mapped to
// fixed SQL expressions.
type ChunkFeedbackListQuery struct {
	Page              int    `form:"page"`
	PageSize          int    `form:"page_size"`
	Keyword           string `form:"keyword"`
	FeedbackStatus    string `form:"feedback_status"`
	NeedsOptimization *bool  `form:"needs_optimization"`
	SortBy            string `form:"sort_by"`
	SortOrder         string `form:"sort_order"`
}

func (q *ChunkFeedbackListQuery) Validate() error {
	if q == nil {
		return errors.New("chunk feedback query is required")
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 20
	}
	if err := ValidateChunkFeedbackPagination(q.Page, q.PageSize); err != nil {
		return err
	}
	q.Keyword = strings.TrimSpace(q.Keyword)
	q.FeedbackStatus = strings.ToLower(strings.TrimSpace(q.FeedbackStatus))
	if q.FeedbackStatus == "" {
		q.FeedbackStatus = ChunkFeedbackStatusAll
	}
	switch q.FeedbackStatus {
	case ChunkFeedbackStatusAll, ChunkFeedbackStatusRated, ChunkFeedbackStatusHigh,
		ChunkFeedbackStatusNormal, ChunkFeedbackStatusLow, ChunkFeedbackStatusUnrated:
	default:
		return errors.New("feedback_status must be all, rated, high, normal, low, or unrated")
	}
	q.SortBy = strings.ToLower(strings.TrimSpace(q.SortBy))
	if q.SortBy == "" {
		q.SortBy = "feedback_updated_at"
	}
	switch q.SortBy {
	case "feedback_updated_at", "like_count", "dislike_count", "positive_rate", "recall_weight", "chunk_index":
	default:
		return errors.New("invalid chunk feedback sort_by")
	}
	q.SortOrder = strings.ToLower(strings.TrimSpace(q.SortOrder))
	if q.SortOrder == "" {
		q.SortOrder = "desc"
	}
	if q.SortOrder != "asc" && q.SortOrder != "desc" {
		return errors.New("sort_order must be asc or desc")
	}
	return nil
}

func ValidateChunkFeedbackPagination(page, pageSize int) error {
	if page < 1 {
		return errors.New("page must be a positive integer")
	}
	if pageSize < 1 || pageSize > 100 {
		return errors.New("page_size must be between 1 and 100")
	}
	maxInt := int(^uint(0) >> 1)
	if page > 1 && page-1 > maxInt/pageSize {
		return errors.New("page is too large")
	}
	return nil
}

func (q *ChunkFeedbackListQuery) Pagination() *Pagination {
	return &Pagination{Page: q.Page, PageSize: q.PageSize}
}

// ChunkFeedbackListItem is the governance-safe projection of a chunk. It is
// separate from Chunk because aggregate fields remain hidden on legacy APIs.
type ChunkFeedbackListItem struct {
	ChunkID           string     `json:"chunk_id"`
	KnowledgeID       string     `json:"knowledge_id"`
	KnowledgeTitle    string     `json:"knowledge_title"`
	ChunkIndex        int        `json:"chunk_index"`
	ChunkType         ChunkType  `json:"chunk_type"`
	ContentPreview    string     `json:"content_preview"`
	LikeCount         int64      `json:"like_count"`
	DislikeCount      int64      `json:"dislike_count"`
	SessionCount      int64      `json:"session_count"`
	PositiveRate      *float64   `json:"positive_rate"`
	RecallWeight      float64    `json:"recall_weight"`
	NeedsOptimization bool       `json:"needs_optimization"`
	FeedbackResetAt   *time.Time `json:"feedback_reset_at"`
	FeedbackUpdatedAt *time.Time `json:"feedback_updated_at"`
	Content           string     `json:"-"`
}

type ChunkFeedbackReasonCount struct {
	ReasonCode string `json:"reason_code"`
	Count      int64  `json:"count"`
}

type ChunkFeedbackDetail struct {
	ChunkFeedbackListItem
	Content      string                      `json:"content"`
	ReasonCounts []*ChunkFeedbackReasonCount `json:"reason_counts"`
}

type ChunkFeedbackWeightLogItem struct {
	ID               string    `json:"id"`
	OldWeight        float64   `json:"old_weight"`
	NewWeight        float64   `json:"new_weight"`
	Source           string    `json:"source"`
	SourceAction     string    `json:"source_action"`
	SourceMessageID  string    `json:"source_message_id,omitempty"`
	SourceFeedbackID string    `json:"source_feedback_id,omitempty"`
	Reason           string    `json:"reason,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type ChunkFeedbackResetInput struct {
	Reason string `json:"reason"`
}

func (i *ChunkFeedbackResetInput) Validate() error {
	if i == nil {
		return nil
	}
	i.Reason = strings.TrimSpace(i.Reason)
	if utf8.RuneCountInString(i.Reason) > 500 {
		return errors.New("reset reason must not exceed 500 characters")
	}
	return nil
}
