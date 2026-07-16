package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type feedbackService struct {
	repo        interfaces.FeedbackRepository
	sessionRepo interfaces.SessionRepository
	messageRepo interfaces.MessageRepository
	chunkRepo   interfaces.ChunkRepository
	config      *types.ChunkFeedbackConfig
}

// NewFeedbackService creates the answer-feedback and chunk-governance service.
func NewFeedbackService(
	repo interfaces.FeedbackRepository,
	sessionRepo interfaces.SessionRepository,
	messageRepo interfaces.MessageRepository,
	chunkRepo interfaces.ChunkRepository,
	cfg *config.Config,
) interfaces.FeedbackService {
	feedbackConfig := types.DefaultChunkFeedbackConfig()
	if cfg != nil && cfg.Feedback != nil {
		feedbackConfig = cfg.Feedback
	}
	return &feedbackService{
		repo:        repo,
		sessionRepo: sessionRepo,
		messageRepo: messageRepo,
		chunkRepo:   chunkRepo,
		config:      feedbackConfig,
	}
}

func (s *feedbackService) CompleteAssistantMessage(ctx context.Context, message *types.Message) error {
	sessionTenantID, err := feedbackAttributionTenant(ctx, message)
	if err != nil {
		return err
	}
	refs, err := s.buildMessageChunkReferences(ctx, sessionTenantID, message, nil)
	if err != nil {
		return err
	}
	return s.repo.CompleteAssistantMessage(ctx, message, refs)
}

func (s *feedbackService) PersistMessageChunkReferences(ctx context.Context, message *types.Message) error {
	sessionTenantID, err := feedbackAttributionTenant(ctx, message)
	if err != nil {
		return err
	}
	existing, err := s.repo.ListMessageChunkReferences(ctx, sessionTenantID, message.ID)
	if err != nil {
		return err
	}
	refs, err := s.buildMessageChunkReferences(ctx, sessionTenantID, message, existing)
	if err != nil {
		return err
	}
	return s.repo.CreateMessageChunkReferences(ctx, refs)
}

func feedbackAttributionTenant(ctx context.Context, message *types.Message) (uint64, error) {
	if message == nil || message.ID == "" || message.SessionID == "" {
		return 0, fmt.Errorf("persist feedback attribution: message identity is required")
	}
	if message.Role != "assistant" || !message.IsCompleted {
		return 0, types.ErrFeedbackMessageIncomplete
	}
	sessionTenantID, ok := types.SessionTenantIDFromContext(ctx)
	if !ok || sessionTenantID == 0 {
		return 0, fmt.Errorf("persist feedback attribution: session tenant is required")
	}
	return sessionTenantID, nil
}

func (s *feedbackService) buildMessageChunkReferences(
	ctx context.Context,
	sessionTenantID uint64,
	message *types.Message,
	existing []*types.MessageChunkReference,
) ([]*types.MessageChunkReference, error) {
	type referenceCandidate struct {
		chunkID        string
		rank           int
		retrievalScore float64
		matchType      types.MatchType
	}
	candidates := make([]referenceCandidate, 0, len(message.KnowledgeReferences))
	chunkIDs := make([]string, 0, len(message.KnowledgeReferences))
	seenIDs := make(map[string]struct{}, len(message.KnowledgeReferences))
	for rank, ref := range message.KnowledgeReferences {
		if !isFeedbackEligibleReference(ref) {
			continue
		}
		sourceIDs := make([]string, 0, len(ref.SubChunkID)+1)
		sourceIDs = append(sourceIDs, ref.ID)
		sourceIDs = append(sourceIDs, ref.SubChunkID...)
		for _, sourceID := range sourceIDs {
			sourceID = strings.TrimSpace(sourceID)
			if sourceID == "" {
				continue
			}
			if _, seen := seenIDs[sourceID]; seen {
				continue
			}
			seenIDs[sourceID] = struct{}{}
			chunkIDs = append(chunkIDs, sourceID)
			candidates = append(candidates, referenceCandidate{
				chunkID:        sourceID,
				rank:           rank,
				retrievalScore: ref.Score,
				matchType:      ref.MatchType,
			})
		}
	}
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	chunks, err := s.chunkRepo.ListChunksByIDOnly(ctx, chunkIDs)
	if err != nil {
		return nil, err
	}
	chunkByID := make(map[string]*types.Chunk, len(chunks))
	for _, chunk := range chunks {
		if chunk != nil {
			chunkByID[chunk.ID] = chunk
		}
	}

	existingChunkIDs := make(map[string]struct{}, len(existing))
	for _, ref := range existing {
		if ref != nil {
			existingChunkIDs[ref.ChunkID] = struct{}{}
		}
	}

	refs := make([]*types.MessageChunkReference, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := existingChunkIDs[candidate.chunkID]; ok {
			continue
		}
		chunk := chunkByID[candidate.chunkID]
		if chunk == nil || chunk.TenantID == 0 {
			continue
		}
		refs = append(refs, &types.MessageChunkReference{
			SessionTenantID: sessionTenantID,
			ChunkTenantID:   chunk.TenantID,
			SessionID:       message.SessionID,
			MessageID:       message.ID,
			ChunkID:         chunk.ID,
			KnowledgeBaseID: chunk.KnowledgeBaseID,
			KnowledgeID:     chunk.KnowledgeID,
			ReferenceRank:   candidate.rank,
			RetrievalScore:  candidate.retrievalScore,
			MatchType:       strconv.Itoa(int(candidate.matchType)),
		})
	}
	return refs, nil
}

func isFeedbackEligibleReference(ref *types.SearchResult) bool {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return false
	}
	if ref.MatchType == types.MatchTypeHistory || ref.MatchType == types.MatchTypeWebSearch {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(ref.KnowledgeSource))
	return source != "web_search" && source != "history"
}

func (s *feedbackService) SetMessageFeedback(
	ctx context.Context,
	sessionID string,
	messageID string,
	input *types.MessageFeedbackInput,
) (*types.MessageFeedbackState, error) {
	if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		return nil, types.ErrFeedbackUnauthorized
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	sessionTenantID, userID, message, err := s.authorizeMessage(ctx, sessionID, messageID)
	if err != nil {
		return nil, err
	}

	if err := s.PersistMessageChunkReferences(ctx, message); err != nil {
		return nil, fmt.Errorf("persist feedback attribution fallback: %w", err)
	}
	refs, err := s.repo.ListMessageChunkReferences(ctx, sessionTenantID, messageID)
	if err != nil {
		return nil, err
	}
	if err := authorizeFeedbackReferenceKnowledgeBases(ctx, refs); err != nil {
		return nil, fmt.Errorf("%w: %v", types.ErrFeedbackUnauthorized, err)
	}

	feedback, err := s.repo.ApplyMessageFeedback(ctx, types.MessageFeedbackMutation{
		SessionTenantID: sessionTenantID,
		UserID:          userID,
		SessionID:       sessionID,
		MessageID:       messageID,
		FeedbackType:    input.FeedbackType,
		ReasonCode:      input.ReasonCode,
		ReasonText:      input.ReasonText,
	}, s.config)
	if err != nil {
		return nil, err
	}
	return feedbackState(feedback), nil
}

func authorizeFeedbackReferenceKnowledgeBases(ctx context.Context, refs []*types.MessageChunkReference) error {
	kbIDs := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		kbID := strings.TrimSpace(ref.KnowledgeBaseID)
		if kbID == "" {
			continue
		}
		if _, ok := seen[kbID]; ok {
			continue
		}
		seen[kbID] = struct{}{}
		kbIDs = append(kbIDs, kbID)
	}
	return types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbIDs...)
}

func (s *feedbackService) GetMessageFeedback(
	ctx context.Context,
	sessionID string,
	messageID string,
) (*types.MessageFeedbackState, error) {
	sessionTenantID, userID, _, err := s.authorizeMessage(ctx, sessionID, messageID)
	if err != nil {
		return nil, err
	}
	feedback, err := s.repo.GetMessageFeedback(ctx, sessionTenantID, userID, messageID)
	if err != nil {
		return nil, err
	}
	return feedbackState(feedback), nil
}

func (s *feedbackService) authorizeMessage(
	ctx context.Context,
	sessionID string,
	messageID string,
) (uint64, string, *types.Message, error) {
	sessionTenantID, ok := types.SessionTenantIDFromContext(ctx)
	if !ok || sessionTenantID == 0 {
		return 0, "", nil, types.ErrFeedbackUnauthorized
	}
	userID := types.SessionOwnerIDFromContext(ctx)
	if userID == "" {
		return 0, "", nil, types.ErrFeedbackUnauthorized
	}
	if _, err := s.sessionRepo.Get(ctx, sessionTenantID, userID, sessionID); err != nil {
		if errors.Is(err, apperrors.ErrSessionNotFound) {
			return 0, "", nil, fmt.Errorf("%w: session", types.ErrFeedbackMessageNotFound)
		}
		return 0, "", nil, fmt.Errorf("authorize feedback session: %w", err)
	}
	message, err := s.messageRepo.GetMessage(ctx, sessionID, messageID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, "", nil, fmt.Errorf("%w: message", types.ErrFeedbackMessageNotFound)
		}
		return 0, "", nil, fmt.Errorf("authorize feedback message: %w", err)
	}
	if message.Role != "assistant" || !message.IsCompleted {
		return 0, "", nil, types.ErrFeedbackMessageIncomplete
	}
	return sessionTenantID, userID, message, nil
}

func feedbackState(feedback *types.MessageFeedback) *types.MessageFeedbackState {
	if feedback == nil {
		return nil
	}
	return &types.MessageFeedbackState{
		FeedbackType: feedback.FeedbackType,
		ReasonCode:   feedback.ReasonCode,
		ReasonText:   feedback.ReasonText,
		FeedbackAt:   feedback.FeedbackAt,
	}
}

func (s *feedbackService) ListChunkFeedback(
	ctx context.Context,
	kbID string,
	query *types.ChunkFeedbackListQuery,
) (*types.PageResult, error) {
	tenantID, err := chunkFeedbackScope(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err := query.Validate(); err != nil {
		return nil, err
	}
	items, total, err := s.repo.ListChunkFeedback(ctx, tenantID, strings.TrimSpace(kbID), query, s.config)
	if err != nil {
		return nil, err
	}
	return types.NewPageResult(total, query.Pagination(), items), nil
}

func (s *feedbackService) GetChunkFeedbackDetail(
	ctx context.Context,
	kbID string,
	chunkID string,
) (*types.ChunkFeedbackDetail, error) {
	tenantID, err := chunkFeedbackScope(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(chunkID) == "" {
		return nil, errors.New("chunk ID is required")
	}
	detail, err := s.repo.GetChunkFeedbackDetail(ctx, tenantID, strings.TrimSpace(kbID), strings.TrimSpace(chunkID))
	if err != nil {
		return nil, err
	}
	detail.ContentPreview = chunkFeedbackPreview(detail.Content)
	return detail, nil
}

func (s *feedbackService) ListChunkFeedbackWeightLogs(
	ctx context.Context,
	kbID string,
	chunkID string,
	page *types.Pagination,
) (*types.PageResult, error) {
	tenantID, err := chunkFeedbackScope(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(chunkID) == "" {
		return nil, errors.New("chunk ID is required")
	}
	if page == nil {
		page = &types.Pagination{Page: 1, PageSize: 20}
	}
	if err := types.ValidateChunkFeedbackPagination(page.Page, page.PageSize); err != nil {
		return nil, err
	}
	logs, total, err := s.repo.ListChunkFeedbackWeightLogs(
		ctx, tenantID, strings.TrimSpace(kbID), strings.TrimSpace(chunkID), page,
	)
	if err != nil {
		return nil, err
	}
	return types.NewPageResult(total, page, logs), nil
}

func (s *feedbackService) ResetChunkFeedback(
	ctx context.Context,
	kbID string,
	chunkID string,
	input *types.ChunkFeedbackResetInput,
) (*types.ChunkFeedbackDetail, error) {
	tenantID, err := chunkFeedbackScope(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(chunkID) == "" {
		return nil, errors.New("chunk ID is required")
	}
	if input == nil {
		input = &types.ChunkFeedbackResetInput{}
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	kbID = strings.TrimSpace(kbID)
	chunkID = strings.TrimSpace(chunkID)
	detail, err := s.repo.ResetChunkFeedback(ctx, tenantID, kbID, chunkID, input.Reason, s.config)
	if err != nil {
		return nil, err
	}
	detail.ContentPreview = chunkFeedbackPreview(detail.Content)
	return detail, nil
}

func chunkFeedbackScope(ctx context.Context, kbID string) (uint64, error) {
	if strings.TrimSpace(kbID) == "" {
		return 0, errors.New("knowledge base ID is required")
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return 0, types.ErrFeedbackUnauthorized
	}
	return tenantID, nil
}

func chunkFeedbackPreview(content string) string {
	const maxRunes = 200
	runes := []rune(strings.TrimSpace(content))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes])
}
