package tools

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAgentRecallWeightReader struct {
	weights []interfaces.ChunkRecallWeight
	err     error
	calls   int
	scopes  []interfaces.ChunkFeedbackWeightScope
}

func (f *fakeAgentRecallWeightReader) ListChunkRecallWeights(
	_ context.Context,
	scopes []interfaces.ChunkFeedbackWeightScope,
) ([]interfaces.ChunkRecallWeight, error) {
	f.calls++
	f.scopes = append([]interfaces.ChunkFeedbackWeightScope(nil), scopes...)
	return f.weights, f.err
}

func TestKnowledgeSearchFeedbackWeightsUseOwningTenantAndApplyOnce(t *testing.T) {
	reader := &fakeAgentRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{TenantID: 8, KnowledgeBaseID: "shared-kb", ChunkID: "shared", RecallWeight: 1.2},
		{TenantID: 1, KnowledgeBaseID: "owned-kb", ChunkID: "owned", RecallWeight: 0.8},
	}}
	tool := &KnowledgeSearchTool{feedbackWeightReader: reader}
	shared := agentSearchResult("shared", "shared-kb", 0.5)
	owned := agentSearchResult("owned", "owned-kb", 0.7)
	web := agentSearchResult("web", "owned-kb", 0.9)
	web.MatchType = types.MatchTypeWebSearch
	results := []*searchResultWithMeta{owned, web, shared}
	targets := types.SearchTargets{
		{KnowledgeBaseID: "owned-kb", TenantID: 1},
		{KnowledgeBaseID: "shared-kb", TenantID: 8},
	}

	tool.applyFeedbackWeights(context.Background(), results, targets)
	require.Equal(t, 1, reader.calls)
	assert.ElementsMatch(t, []interfaces.ChunkFeedbackWeightScope{
		{TenantID: 1, KnowledgeBaseID: "owned-kb", ChunkID: "owned"},
		{TenantID: 8, KnowledgeBaseID: "shared-kb", ChunkID: "shared"},
	}, reader.scopes)
	assert.Equal(t, []string{"web", "shared", "owned"}, agentResultIDs(results))
	assert.InDelta(t, 0.6, shared.Score, 1e-12)
	assert.InDelta(t, 0.56, owned.Score, 1e-12)
	assert.False(t, web.FeedbackWeightApplied)

	tool.applyFeedbackWeights(context.Background(), results, targets)
	assert.Equal(t, 1, reader.calls)
	assert.InDelta(t, 0.6, shared.Score, 1e-12)
	assert.InDelta(t, 0.56, owned.Score, 1e-12)
}

func TestKnowledgeSearchFeedbackWeightFailurePreservesResults(t *testing.T) {
	reader := &fakeAgentRecallWeightReader{err: errors.New("database unavailable")}
	tool := &KnowledgeSearchTool{feedbackWeightReader: reader}
	first := agentSearchResult("first", "kb", 0.2)
	second := agentSearchResult("second", "kb", 0.9)
	results := []*searchResultWithMeta{first, second}

	tool.applyFeedbackWeights(context.Background(), results, types.SearchTargets{
		{KnowledgeBaseID: "kb", TenantID: 3},
	})
	assert.Equal(t, []string{"first", "second"}, agentResultIDs(results))
	assert.Equal(t, 0.2, first.Score)
	assert.Equal(t, 0.9, second.Score)
	assert.False(t, first.FeedbackWeightApplied)
	assert.False(t, second.FeedbackWeightApplied)
}

func TestKnowledgeSearchFeedbackWeightDoesNotMultiplySharedResultTwice(t *testing.T) {
	reader := &fakeAgentRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "chunk", RecallWeight: 1.2},
	}}
	tool := &KnowledgeSearchTool{feedbackWeightReader: reader}
	sharedResult := &types.SearchResult{ID: "chunk", KnowledgeBaseID: "kb", Score: 0.5}
	results := []*searchResultWithMeta{
		{SearchResult: sharedResult, KnowledgeBaseID: "kb"},
		{SearchResult: sharedResult, KnowledgeBaseID: "kb"},
	}

	tool.applyFeedbackWeights(context.Background(), results, types.SearchTargets{
		{KnowledgeBaseID: "kb", TenantID: 1},
	})
	assert.InDelta(t, 0.6, sharedResult.Score, 1e-12)
	assert.Equal(t, 1, reader.calls)
	assert.Len(t, reader.scopes, 1)
}

func TestKnowledgeSearchFeedbackWeightPreservesStableScoreTies(t *testing.T) {
	reader := &fakeAgentRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "first", RecallWeight: 1.2},
		{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "second", RecallWeight: 1},
	}}
	tool := &KnowledgeSearchTool{feedbackWeightReader: reader}
	first := agentSearchResult("first", "kb", 0.5)
	first.KnowledgeID = "z-knowledge"
	second := agentSearchResult("second", "kb", 0.6)
	second.KnowledgeID = "a-knowledge"
	results := []*searchResultWithMeta{first, second}

	tool.applyFeedbackWeights(context.Background(), results, types.SearchTargets{
		{KnowledgeBaseID: "kb", TenantID: 1},
	})

	assert.Equal(t, []string{"first", "second"}, agentResultIDs(results))
	assert.InDelta(t, first.Score, second.Score, 1e-12)
}

func TestStableSortAgentSearchResultsPreservesPriorOrderForScoreTies(t *testing.T) {
	first := agentSearchResult("first", "kb", 0.6)
	first.KnowledgeID = "z-knowledge"
	second := agentSearchResult("second", "kb", 0.6)
	second.KnowledgeID = "a-knowledge"
	results := []*searchResultWithMeta{first, second}

	stableSortAgentSearchResults(results)

	assert.Equal(t, []string{"first", "second"}, agentResultIDs(results))
}

func TestAgentFeedbackCandidatePoolAllowsBoostInAndPenaltyOut(t *testing.T) {
	for _, withRerank := range []bool{false, true} {
		name := "without_rerank"
		if withRerank {
			name = "with_rerank"
		}
		t.Run(name, func(t *testing.T) {
			testAgentFeedbackCandidateSelection(t, withRerank)
		})
	}
}

func testAgentFeedbackCandidateSelection(t *testing.T, withRerank bool) {
	cfg := types.DefaultChunkFeedbackConfig()
	cfg.MinimumSampleCount = 1
	reader := &fakeAgentRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{
			TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "rank-5",
			DislikeCount: 5, RecallWeight: 99,
		},
		{
			TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "rank-6",
			LikeCount: 5, RecallWeight: 0.01,
		},
	}}
	tool := &KnowledgeSearchTool{
		feedbackWeightReader: reader,
		config:               &config.Config{Feedback: cfg},
	}
	results := []*searchResultWithMeta{
		agentSearchResult("rank-1", "kb", 0.90),
		agentSearchResult("rank-2", "kb", 0.80),
		agentSearchResult("rank-3", "kb", 0.70),
		agentSearchResult("rank-4", "kb", 0.60),
		agentSearchResult("rank-5", "kb", 0.50),
		agentSearchResult("rank-6", "kb", 0.45),
	}
	for i, result := range results {
		result.Content = result.ID + " unique content"
		result.ChunkIndex = i
	}

	if withRerank {
		rankResults := []rerank.RankResult{
			{Index: 0, RelevanceScore: 0.90},
			{Index: 1, RelevanceScore: 0.80},
			{Index: 2, RelevanceScore: 0.70},
			{Index: 3, RelevanceScore: 0.60},
			{Index: 4, RelevanceScore: 0.50},
			{Index: 5, RelevanceScore: 0.49},
		}
		results = tool.applyModelRerankScores(results, rankResults, 0.3, false)
	}

	tool.applyFeedbackWeights(context.Background(), results, types.SearchTargets{
		{KnowledgeBaseID: "kb", TenantID: 1},
	})
	results = tool.applyMMR(context.Background(), results, 5, 1)

	finalIDs := agentResultIDs(results)
	assert.Contains(t, finalIDs, "rank-6")
	assert.NotContains(t, finalIDs, "rank-5")
	assert.Equal(t, 15, agentFeedbackCandidateK(5))
}

func TestAgentFeedbackCandidatePoolReachesEveryMultiQuerySearch(t *testing.T) {
	service := &stubKnowledgeBaseService{
		kbs: []*types.KnowledgeBase{{
			ID:               "kb",
			EmbeddingModelID: "embedding-model",
			IndexingStrategy: types.DefaultIndexingStrategy(),
		}},
	}
	tool := &KnowledgeSearchTool{knowledgeBaseService: service}
	targets := types.SearchTargets{{
		Type:            types.SearchTargetTypeKnowledgeBase,
		KnowledgeBaseID: "kb",
		TenantID:        1,
	}}

	tool.concurrentSearchByTargets(
		context.Background(),
		[]string{"first query", "second query"},
		targets,
		agentFeedbackCandidateK(5),
		0.6,
		0.5,
		map[string]string{"kb": types.KnowledgeBaseTypeDocument},
	)

	service.searchMu.Lock()
	params := append([]types.SearchParams(nil), service.searchParams...)
	service.searchMu.Unlock()
	require.Len(t, params, 2)
	sort.Slice(params, func(i, j int) bool { return params[i].QueryText < params[j].QueryText })
	assert.Equal(t, "first query", params[0].QueryText)
	assert.Equal(t, "second query", params[1].QueryText)
	for _, searchParams := range params {
		assert.Equal(t, 15, searchParams.MatchCount)
	}
}

func TestAgentFeedbackUsesCurrentPolicyInsteadOfPersistedWeight(t *testing.T) {
	cfg := types.DefaultChunkFeedbackConfig()
	cfg.MinimumSampleCount = 1
	cfg.HighRateThreshold = 0.9
	reader := &fakeAgentRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{
			TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "chunk",
			LikeCount: 8, DislikeCount: 2, RecallWeight: 9,
		},
	}}
	tool := &KnowledgeSearchTool{
		feedbackWeightReader: reader,
		config:               &config.Config{Feedback: cfg},
	}
	result := agentSearchResult("chunk", "kb", 0.5)

	tool.applyFeedbackWeights(context.Background(), []*searchResultWithMeta{result}, types.SearchTargets{
		{KnowledgeBaseID: "kb", TenantID: 1},
	})

	assert.InDelta(t, 0.5, result.Score, 1e-12)
}

func agentSearchResult(id, kbID string, score float64) *searchResultWithMeta {
	return &searchResultWithMeta{
		SearchResult:    &types.SearchResult{ID: id, KnowledgeBaseID: kbID, Score: score},
		KnowledgeBaseID: kbID,
	}
}

func agentResultIDs(results []*searchResultWithMeta) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}
