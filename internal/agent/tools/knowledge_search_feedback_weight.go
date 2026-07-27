package tools

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type chunkRecallWeightReader interface {
	ListChunkRecallWeights(
		ctx context.Context,
		scopes []interfaces.ChunkFeedbackWeightScope,
	) ([]interfaces.ChunkRecallWeight, error)
}

func (t *KnowledgeSearchTool) applyFeedbackWeights(
	ctx context.Context,
	results []*searchResultWithMeta,
	searchTargets types.SearchTargets,
) {
	if t.feedbackWeightReader == nil || len(results) == 0 {
		return
	}

	tenantByKB := searchTargets.GetKBTenantMap()
	type candidate struct {
		result   *searchResultWithMeta
		tenantID uint64
		kbID     string
	}
	candidates := make([]candidate, 0, len(results))
	scopes := make([]interfaces.ChunkFeedbackWeightScope, 0, len(results))
	seenScopes := make(map[string]struct{})
	seenResults := make(map[*types.SearchResult]struct{})
	for _, result := range results {
		if result == nil || result.SearchResult == nil || result.FeedbackWeightApplied ||
			!agentFeedbackWeightEligible(result.SearchResult) {
			continue
		}
		if _, ok := seenResults[result.SearchResult]; ok {
			continue
		}
		seenResults[result.SearchResult] = struct{}{}
		kbID := result.KnowledgeBaseID
		if kbID == "" {
			kbID = result.SearchResult.KnowledgeBaseID
		}
		tenantID, ok := tenantByKB[kbID]
		if !ok || tenantID == 0 || kbID == "" {
			continue
		}
		candidates = append(candidates, candidate{result: result, tenantID: tenantID, kbID: kbID})
		key := agentFeedbackWeightKey(tenantID, kbID, result.ID)
		if _, ok := seenScopes[key]; ok {
			continue
		}
		seenScopes[key] = struct{}{}
		scopes = append(scopes, interfaces.ChunkFeedbackWeightScope{
			TenantID:        tenantID,
			KnowledgeBaseID: kbID,
			ChunkID:         result.ID,
		})
	}
	if len(scopes) == 0 {
		return
	}

	weights, err := t.feedbackWeightReader.ListChunkRecallWeights(ctx, scopes)
	if err != nil {
		logger.Warnf(ctx, "[Tool][KnowledgeSearch] Feedback weight query failed, preserving original ranking: %v", err)
		return
	}
	weightByScope := make(map[string]float64, len(weights))
	var feedbackConfig *types.ChunkFeedbackConfig
	if t.config != nil {
		feedbackConfig = t.config.Feedback
	}
	for _, aggregate := range weights {
		weight := aggregate.RecallWeight
		if feedbackConfig != nil {
			_, weight, _ = types.CalculateChunkFeedback(
				aggregate.LikeCount,
				aggregate.DislikeCount,
				feedbackConfig,
			)
		}
		if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
			continue
		}
		weightByScope[agentFeedbackWeightKey(
			aggregate.TenantID,
			aggregate.KnowledgeBaseID,
			aggregate.ChunkID,
		)] = weight
	}
	for _, item := range candidates {
		weight := weightByScope[agentFeedbackWeightKey(item.tenantID, item.kbID, item.result.ID)]
		if weight == 0 {
			weight = 1
		}
		weightedScore := item.result.Score * weight
		if !math.IsNaN(weightedScore) && !math.IsInf(weightedScore, 0) {
			item.result.Score = weightedScore
		}
		item.result.FeedbackWeightApplied = true
	}
	stableSortAgentSearchResults(results)
}

func agentFeedbackWeightEligible(result *types.SearchResult) bool {
	if result.ID == "" || result.MatchType == types.MatchTypeHistory || result.MatchType == types.MatchTypeWebSearch {
		return false
	}
	return !strings.EqualFold(result.ChunkType, string(types.ChunkTypeWebSearch)) &&
		!strings.EqualFold(result.KnowledgeSource, "web_search")
}

func stableSortAgentSearchResults(results []*searchResultWithMeta) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i] == nil || results[i].SearchResult == nil {
			return false
		}
		if results[j] == nil || results[j].SearchResult == nil {
			return true
		}
		iFinite := !math.IsNaN(results[i].Score) && !math.IsInf(results[i].Score, 0)
		jFinite := !math.IsNaN(results[j].Score) && !math.IsInf(results[j].Score, 0)
		if iFinite != jFinite {
			return iFinite
		}
		if !iFinite {
			return false
		}
		return results[i].Score > results[j].Score
	})
}

func agentFeedbackWeightKey(tenantID uint64, kbID, chunkID string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", tenantID, kbID, chunkID)
}
