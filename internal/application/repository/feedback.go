package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type feedbackRepository struct {
	db *gorm.DB
}

func NewFeedbackRepository(db *gorm.DB) interfaces.FeedbackRepository {
	return &feedbackRepository{db: db}
}

func (r *feedbackRepository) CreateMessageChunkReferences(
	ctx context.Context,
	refs []*types.MessageChunkReference,
) error {
	if len(refs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&refs).Error
}

func (r *feedbackRepository) ListMessageChunkReferences(
	ctx context.Context,
	sessionTenantID uint64,
	messageID string,
) ([]*types.MessageChunkReference, error) {
	var refs []*types.MessageChunkReference
	err := r.db.WithContext(ctx).
		Where("session_tenant_id = ? AND message_id = ?", sessionTenantID, messageID).
		Order("chunk_tenant_id ASC, chunk_id ASC").
		Find(&refs).Error
	return refs, err
}

func (r *feedbackRepository) GetMessageFeedback(
	ctx context.Context,
	sessionTenantID uint64,
	userID string,
	messageID string,
) (*types.MessageFeedback, error) {
	var feedback types.MessageFeedback
	err := r.db.WithContext(ctx).
		Where("session_tenant_id = ? AND user_id = ? AND message_id = ?", sessionTenantID, userID, messageID).
		First(&feedback).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &feedback, nil
}

func (r *feedbackRepository) ListMessageFeedbacks(
	ctx context.Context,
	sessionTenantID uint64,
	userID string,
	messageIDs []string,
) ([]*types.MessageFeedback, error) {
	if len(messageIDs) == 0 || userID == "" {
		return nil, nil
	}
	var feedbacks []*types.MessageFeedback
	err := r.db.WithContext(ctx).
		Where("session_tenant_id = ? AND user_id = ? AND message_id IN ?", sessionTenantID, userID, messageIDs).
		Find(&feedbacks).Error
	return feedbacks, err
}

func (r *feedbackRepository) ApplyMessageFeedback(
	ctx context.Context,
	mutation types.MessageFeedbackMutation,
	cfg *types.ChunkFeedbackConfig,
) (*types.MessageFeedback, error) {
	if cfg == nil {
		cfg = types.DefaultChunkFeedbackConfig()
	}
	var result *types.MessageFeedback
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		refs, err := listMessageChunkReferencesTx(tx, mutation.SessionTenantID, mutation.MessageID)
		if err != nil {
			return err
		}
		chunks, err := lockReferencedChunks(tx, refs)
		if err != nil {
			return err
		}
		if err := lockFeedbackMessage(tx, mutation.SessionID, mutation.MessageID); err != nil {
			return err
		}

		existing, err := lockMessageFeedback(tx, mutation)
		if err != nil {
			return err
		}
		now := feedbackMutationTime(chunks, time.Now().UTC())
		action, feedbackID, aggregatesChanged, err := mutateMessageFeedback(tx, existing, mutation, now)
		if err != nil {
			return err
		}
		if mutation.FeedbackType == types.FeedbackTypeNone {
			result = nil
		} else if existing == nil {
			result, err = findMessageFeedbackTx(tx, mutation)
			if err != nil {
				return err
			}
		} else {
			result = existing
		}
		if !aggregatesChanged {
			return nil
		}

		for _, chunk := range chunks {
			if err := recalculateChunkFeedback(tx, chunk, cfg, now, action, mutation, feedbackID); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func feedbackMutationTime(chunks []*types.Chunk, now time.Time) time.Time {
	for _, chunk := range chunks {
		if chunk == nil || chunk.FeedbackResetAt == nil {
			continue
		}
		resetAt := chunk.FeedbackResetAt.Truncate(time.Microsecond)
		if !now.Truncate(time.Microsecond).After(resetAt) {
			now = resetAt.Add(time.Microsecond)
		}
	}
	return now
}

func lockFeedbackMessage(tx *gorm.DB, sessionID, messageID string) error {
	query := tx.Select("id").Where("session_id = ? AND id = ?", sessionID, messageID)
	if tx.Dialector.Name() != "sqlite" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var message types.Message
	return query.First(&message).Error
}

func listMessageChunkReferencesTx(
	tx *gorm.DB,
	sessionTenantID uint64,
	messageID string,
) ([]*types.MessageChunkReference, error) {
	var refs []*types.MessageChunkReference
	err := tx.Where("session_tenant_id = ? AND message_id = ?", sessionTenantID, messageID).
		Order("chunk_tenant_id ASC, chunk_id ASC").
		Find(&refs).Error
	return refs, err
}

func lockReferencedChunks(tx *gorm.DB, refs []*types.MessageChunkReference) ([]*types.Chunk, error) {
	type key struct {
		tenantID uint64
		chunkID  string
	}
	unique := make(map[key]struct{}, len(refs))
	keys := make([]key, 0, len(refs))
	for _, ref := range refs {
		if ref == nil || ref.ChunkTenantID == 0 || ref.ChunkID == "" {
			continue
		}
		k := key{tenantID: ref.ChunkTenantID, chunkID: ref.ChunkID}
		if _, ok := unique[k]; ok {
			continue
		}
		unique[k] = struct{}{}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].tenantID == keys[j].tenantID {
			return keys[i].chunkID < keys[j].chunkID
		}
		return keys[i].tenantID < keys[j].tenantID
	})

	conditions := make([]string, 0, len(keys))
	args := make([]interface{}, 0, len(keys)*2)
	for _, k := range keys {
		conditions = append(conditions, "(tenant_id = ? AND id = ?)")
		args = append(args, k.tenantID, k.chunkID)
	}
	query := tx.Where(strings.Join(conditions, " OR "), args...).Order("tenant_id ASC, id ASC")
	if tx.Dialector.Name() != "sqlite" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var chunks []*types.Chunk
	if err := query.Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

func lockMessageFeedback(
	tx *gorm.DB,
	mutation types.MessageFeedbackMutation,
) (*types.MessageFeedback, error) {
	query := tx.Where(
		"session_tenant_id = ? AND user_id = ? AND message_id = ?",
		mutation.SessionTenantID, mutation.UserID, mutation.MessageID,
	)
	if tx.Dialector.Name() != "sqlite" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var feedback types.MessageFeedback
	err := query.First(&feedback).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &feedback, nil
}

func mutateMessageFeedback(
	tx *gorm.DB,
	existing *types.MessageFeedback,
	mutation types.MessageFeedbackMutation,
	now time.Time,
) (action, feedbackID string, aggregatesChanged bool, err error) {
	if mutation.FeedbackType == types.FeedbackTypeNone {
		if existing == nil {
			return types.ChunkFeedbackLogActionCancel, "", false, nil
		}
		if err := tx.Delete(existing).Error; err != nil {
			return "", "", false, err
		}
		return types.ChunkFeedbackLogActionCancel, existing.ID, true, nil
	}

	if existing == nil {
		feedback := &types.MessageFeedback{
			SessionTenantID: mutation.SessionTenantID,
			UserID:          mutation.UserID,
			SessionID:       mutation.SessionID,
			MessageID:       mutation.MessageID,
			FeedbackType:    mutation.FeedbackType,
			ReasonCode:      mutation.ReasonCode,
			ReasonText:      mutation.ReasonText,
			FeedbackAt:      now,
		}
		if err := tx.Create(feedback).Error; err != nil {
			return "", "", false, err
		}
		return mutation.FeedbackType, feedback.ID, true, nil
	}

	if existing.FeedbackType == mutation.FeedbackType {
		if existing.ReasonCode == mutation.ReasonCode && existing.ReasonText == mutation.ReasonText {
			return mutation.FeedbackType, existing.ID, false, nil
		}
		if err := tx.Model(existing).Updates(map[string]interface{}{
			"reason_code": mutation.ReasonCode,
			"reason_text": mutation.ReasonText,
			"updated_at":  now,
		}).Error; err != nil {
			return "", "", false, err
		}
		existing.ReasonCode = mutation.ReasonCode
		existing.ReasonText = mutation.ReasonText
		existing.UpdatedAt = now
		return mutation.FeedbackType, existing.ID, false, nil
	}

	if err := tx.Model(existing).Updates(map[string]interface{}{
		"feedback_type": mutation.FeedbackType,
		"reason_code":   mutation.ReasonCode,
		"reason_text":   mutation.ReasonText,
		"feedback_at":   now,
		"updated_at":    now,
	}).Error; err != nil {
		return "", "", false, err
	}
	existing.FeedbackType = mutation.FeedbackType
	existing.ReasonCode = mutation.ReasonCode
	existing.ReasonText = mutation.ReasonText
	existing.FeedbackAt = now
	existing.UpdatedAt = now
	return mutation.FeedbackType, existing.ID, true, nil
}

func findMessageFeedbackTx(
	tx *gorm.DB,
	mutation types.MessageFeedbackMutation,
) (*types.MessageFeedback, error) {
	var feedback types.MessageFeedback
	err := tx.Where(
		"session_tenant_id = ? AND user_id = ? AND message_id = ?",
		mutation.SessionTenantID, mutation.UserID, mutation.MessageID,
	).First(&feedback).Error
	return &feedback, err
}

func recalculateChunkFeedback(
	tx *gorm.DB,
	chunk *types.Chunk,
	cfg *types.ChunkFeedbackConfig,
	now time.Time,
	action string,
	mutation types.MessageFeedbackMutation,
	feedbackID string,
) error {
	var counts struct {
		LikeCount    int64
		DislikeCount int64
	}
	query := tx.Table("message_feedbacks AS feedback").
		Select(
			"COALESCE(SUM(CASE WHEN feedback.feedback_type = ? THEN 1 ELSE 0 END), 0) AS like_count, "+
				"COALESCE(SUM(CASE WHEN feedback.feedback_type = ? THEN 1 ELSE 0 END), 0) AS dislike_count",
			types.FeedbackTypeLike, types.FeedbackTypeDislike,
		).
		Joins("JOIN message_chunk_references AS ref ON ref.session_tenant_id = feedback.session_tenant_id AND ref.message_id = feedback.message_id").
		Where("ref.chunk_tenant_id = ? AND ref.chunk_id = ?", chunk.TenantID, chunk.ID)
	if chunk.FeedbackResetAt != nil {
		query = query.Where("feedback.feedback_at > ?", *chunk.FeedbackResetAt)
	}
	if err := query.Scan(&counts).Error; err != nil {
		return err
	}

	positiveRate, recallWeight, needsOptimization := types.CalculateChunkFeedback(
		counts.LikeCount, counts.DislikeCount, cfg,
	)
	oldWeight := chunk.RecallWeight
	if err := tx.Model(&types.Chunk{}).
		Where("tenant_id = ? AND id = ?", chunk.TenantID, chunk.ID).
		Updates(map[string]interface{}{
			"like_count":          counts.LikeCount,
			"dislike_count":       counts.DislikeCount,
			"positive_rate":       positiveRate,
			"recall_weight":       recallWeight,
			"needs_optimization":  needsOptimization,
			"feedback_updated_at": now,
		}).Error; err != nil {
		return err
	}
	if oldWeight == recallWeight {
		return nil
	}

	reason := mutation.ReasonCode
	if mutation.ReasonText != "" {
		if reason != "" {
			reason += ": "
		}
		reason += mutation.ReasonText
	}
	logEntry := &types.ChunkFeedbackWeightLog{
		ChunkTenantID:    chunk.TenantID,
		ChunkID:          chunk.ID,
		OldWeight:        oldWeight,
		NewWeight:        recallWeight,
		Source:           types.ChunkFeedbackLogSourceUserFeedback,
		SourceAction:     action,
		SourceMessageID:  mutation.MessageID,
		SourceFeedbackID: feedbackID,
		Reason:           reason,
		CreatedAt:        now,
	}
	if err := tx.Create(logEntry).Error; err != nil {
		return fmt.Errorf("create chunk feedback weight log: %w", err)
	}
	return nil
}
