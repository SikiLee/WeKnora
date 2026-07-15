package tools

import (
	"context"
	"errors"
	"testing"

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
