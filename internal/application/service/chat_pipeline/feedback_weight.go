package chatpipeline

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type chunkRecallWeightReader interface {
	ListChunkRecallWeights(
		ctx context.Context,
		scopes []interfaces.ChunkFeedbackWeightScope,
	) ([]interfaces.ChunkRecallWeight, error)
}

// PluginFeedbackWeight applies persisted answer-feedback weights after all
// reranking boosts and before chunks are merged or truncated.
type PluginFeedbackWeight struct {
	chunkRepo chunkRecallWeightReader
}

// NewPluginFeedbackWeight registers the recall-weight stage with the pipeline.
func NewPluginFeedbackWeight(
	eventManager *EventManager,
	chunkRepo interfaces.ChunkRepository,
) *PluginFeedbackWeight {
	plugin := &PluginFeedbackWeight{chunkRepo: chunkRepo}
	eventManager.Register(plugin)
	return plugin
}

// ActivationEvents returns the pipeline event handled by this plugin.
func (p *PluginFeedbackWeight) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_FEEDBACK_WEIGHT}
}

// OnEvent applies feedback weights and finalizes deferred MMR selection.
func (p *PluginFeedbackWeight) OnEvent(
	ctx context.Context,
	_ types.EventType,
	chatManage *types.ChatManage,
	next func() *PluginError,
) *PluginError {
	finish := func() *PluginError {
		finalizePendingRerankMMR(ctx, chatManage)
		return next()
	}
	if chatManage == nil || !chatManage.NeedsRetrieval() || p.chunkRepo == nil {
		return finish()
	}

	results := chatManage.RerankResult
	if len(results) == 0 {
		results = chatManage.SearchResult
	}
	if len(results) == 0 {
		return finish()
	}

	tenantByKB := chatManage.SearchTargets.GetKBTenantMap()
	fallbackTenantID, _ := types.TenantIDFromContext(ctx)
	if fallbackTenantID == 0 {
		fallbackTenantID = chatManage.TenantID
	}

	type candidate struct {
		result   *types.SearchResult
		tenantID uint64
	}
	candidates := make([]candidate, 0, len(results))
	scopes := make([]interfaces.ChunkFeedbackWeightScope, 0, len(results))
	seenScopes := make(map[string]struct{})
	seenResults := make(map[*types.SearchResult]struct{})
	for _, result := range results {
		if result == nil || result.FeedbackWeightApplied || !isFeedbackWeightEligible(result) {
			continue
		}
		if _, ok := seenResults[result]; ok {
			continue
		}
		seenResults[result] = struct{}{}
		tenantID, knownTarget := tenantByKB[result.KnowledgeBaseID]
		if len(tenantByKB) > 0 && !knownTarget {
			continue
		}
		if tenantID == 0 {
			tenantID = fallbackTenantID
		}
		if tenantID == 0 {
			continue
		}
		candidates = append(candidates, candidate{result: result, tenantID: tenantID})
		key := feedbackWeightKey(tenantID, result.KnowledgeBaseID, result.ID)
		if _, ok := seenScopes[key]; ok {
			continue
		}
		seenScopes[key] = struct{}{}
		scopes = append(scopes, interfaces.ChunkFeedbackWeightScope{
			TenantID:        tenantID,
			KnowledgeBaseID: result.KnowledgeBaseID,
			ChunkID:         result.ID,
		})
	}
	if len(scopes) == 0 {
		return finish()
	}

	weights, err := p.chunkRepo.ListChunkRecallWeights(ctx, scopes)
	if err != nil {
		pipelineWarn(ctx, "FeedbackWeight", "query_failed_fallback", map[string]interface{}{
			"error":         err.Error(),
			"candidate_cnt": len(candidates),
		})
		return finish()
	}

	weightByScope := make(map[string]float64, len(weights))
	for _, weight := range weights {
		if weight.RecallWeight <= 0 || math.IsNaN(weight.RecallWeight) || math.IsInf(weight.RecallWeight, 0) {
			continue
		}
		weightByScope[feedbackWeightKey(weight.TenantID, weight.KnowledgeBaseID, weight.ChunkID)] = weight.RecallWeight
	}

	for _, item := range candidates {
		weight := weightByScope[feedbackWeightKey(
			item.tenantID,
			item.result.KnowledgeBaseID,
			item.result.ID,
		)]
		if weight == 0 {
			weight = 1
		}
		weightedScore := item.result.Score * weight
		if !math.IsNaN(weightedScore) && !math.IsInf(weightedScore, 0) {
			item.result.Score = weightedScore
		}
		item.result.FeedbackWeightApplied = true
	}
	stableSortSearchResultsByScore(results)

	pipelineInfo(ctx, "FeedbackWeight", "applied", map[string]interface{}{
		"candidate_cnt": len(candidates),
		"weighted_cnt":  len(weightByScope),
	})
	return finish()
}

func finalizePendingRerankMMR(ctx context.Context, chatManage *types.ChatManage) {
	if chatManage == nil || !chatManage.RerankMMRPending {
		return
	}
	chatManage.RerankMMRPending = false
	results := chatManage.RerankResult
	if len(results) == 0 {
		return
	}
	k := min(len(results), max(1, chatManage.RerankTopK))
	chatManage.RerankResult = applyMMR(ctx, results, chatManage, k, 0.7)
}

func isFeedbackWeightEligible(result *types.SearchResult) bool {
	if result.ID == "" || result.KnowledgeBaseID == "" || result.MatchType == types.MatchTypeHistory ||
		result.MatchType == types.MatchTypeWebSearch {
		return false
	}
	return !strings.EqualFold(result.ChunkType, string(types.ChunkTypeWebSearch)) &&
		!strings.EqualFold(result.KnowledgeSource, "web_search")
}

func feedbackWeightKey(tenantID uint64, kbID, chunkID string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", tenantID, kbID, chunkID)
}
