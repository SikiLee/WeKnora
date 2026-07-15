package chatpipeline

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRecallWeightReader struct {
	weights []interfaces.ChunkRecallWeight
	err     error
	calls   int
	scopes  []interfaces.ChunkFeedbackWeightScope
}

func (f *fakeRecallWeightReader) ListChunkRecallWeights(
	_ context.Context,
	scopes []interfaces.ChunkFeedbackWeightScope,
) ([]interfaces.ChunkRecallWeight, error) {
	f.calls++
	f.scopes = append([]interfaces.ChunkFeedbackWeightScope(nil), scopes...)
	return f.weights, f.err
}

func TestFeedbackWeightAppliesOnceWithStableSortAndSkipsSyntheticResults(t *testing.T) {
	reader := &fakeRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{TenantID: 9, KnowledgeBaseID: "shared-kb", ChunkID: "shared", RecallWeight: 2},
		{TenantID: 1, KnowledgeBaseID: "owned-kb", ChunkID: "owned", RecallWeight: 1},
	}}
	plugin := &PluginFeedbackWeight{chunkRepo: reader}
	shared := &types.SearchResult{ID: "shared", KnowledgeBaseID: "shared-kb", Score: 0.25}
	owned := &types.SearchResult{ID: "owned", KnowledgeBaseID: "owned-kb", Score: 0.5}
	web := &types.SearchResult{
		ID: "https://example.com", KnowledgeBaseID: "owned-kb", Score: 0.65,
		MatchType: types.MatchTypeWebSearch, KnowledgeSource: "web_search",
	}
	history := &types.SearchResult{
		ID: "history", KnowledgeBaseID: "owned-kb", Score: 0.45, MatchType: types.MatchTypeHistory,
	}
	unknownTarget := &types.SearchResult{ID: "unknown", KnowledgeBaseID: "unknown-kb", Score: 0.4}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{SearchTargets: types.SearchTargets{
			{KnowledgeBaseID: "owned-kb", TenantID: 1},
			{KnowledgeBaseID: "shared-kb", TenantID: 9},
		}},
		PipelineState: types.PipelineState{RerankResult: []*types.SearchResult{shared, owned, web, history, unknownTarget}},
	}
	nextCalls := 0
	next := func() *PluginError { nextCalls++; return nil }

	require.Nil(t, plugin.OnEvent(context.Background(), types.CHUNK_FEEDBACK_WEIGHT, chatManage, next))
	require.Equal(t, 1, reader.calls)
	require.Len(t, reader.scopes, 2)
	assert.ElementsMatch(t, []interfaces.ChunkFeedbackWeightScope{
		{TenantID: 9, KnowledgeBaseID: "shared-kb", ChunkID: "shared"},
		{TenantID: 1, KnowledgeBaseID: "owned-kb", ChunkID: "owned"},
	}, reader.scopes)
	assert.Equal(t, []string{"https://example.com", "shared", "owned", "history", "unknown"}, resultIDs(chatManage.RerankResult))
	assert.Equal(t, 0.5, shared.Score)
	assert.Equal(t, 0.5, owned.Score)
	assert.Equal(t, 0.65, web.Score)
	assert.Equal(t, 0.45, history.Score)
	assert.True(t, shared.FeedbackWeightApplied)
	assert.True(t, owned.FeedbackWeightApplied)
	assert.False(t, web.FeedbackWeightApplied)
	assert.False(t, history.FeedbackWeightApplied)
	assert.False(t, unknownTarget.FeedbackWeightApplied)

	require.Nil(t, plugin.OnEvent(context.Background(), types.CHUNK_FEEDBACK_WEIGHT, chatManage, next))
	assert.Equal(t, 1, reader.calls, "a duplicated stage must not query or multiply again")
	assert.Equal(t, 0.5, shared.Score)
	assert.Equal(t, 0.5, owned.Score)
	assert.Equal(t, 2, nextCalls)
}

func TestFeedbackWeightFallsBackToSearchResultsAndDefaultsMissingOrInvalidWeights(t *testing.T) {
	reader := &fakeRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{TenantID: 4, KnowledgeBaseID: "kb", ChunkID: "invalid", RecallWeight: math.Inf(1)},
		{TenantID: 4, KnowledgeBaseID: "kb", ChunkID: "boosted", RecallWeight: 1.2},
	}}
	plugin := &PluginFeedbackWeight{chunkRepo: reader}
	missing := &types.SearchResult{ID: "missing", KnowledgeBaseID: "kb", Score: 0.8}
	invalid := &types.SearchResult{ID: "invalid", KnowledgeBaseID: "kb", Score: 0.7}
	boosted := &types.SearchResult{ID: "boosted", KnowledgeBaseID: "kb", Score: 0.6}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{TenantID: 4},
		PipelineState:   types.PipelineState{SearchResult: []*types.SearchResult{missing, invalid, boosted}},
	}

	require.Nil(t, plugin.OnEvent(context.Background(), types.CHUNK_FEEDBACK_WEIGHT, chatManage, func() *PluginError { return nil }))
	assert.Equal(t, []string{"missing", "boosted", "invalid"}, resultIDs(chatManage.SearchResult))
	assert.Equal(t, 0.8, missing.Score)
	assert.InDelta(t, 0.72, boosted.Score, 1e-12)
	assert.Equal(t, 0.7, invalid.Score)
	assert.True(t, missing.FeedbackWeightApplied)
	assert.True(t, invalid.FeedbackWeightApplied)
}

func TestFeedbackWeightQueryFailurePreservesScoresOrderAndMarkers(t *testing.T) {
	reader := &fakeRecallWeightReader{err: errors.New("database unavailable")}
	plugin := &PluginFeedbackWeight{chunkRepo: reader}
	first := &types.SearchResult{ID: "first", KnowledgeBaseID: "kb", Score: 0.2}
	second := &types.SearchResult{ID: "second", KnowledgeBaseID: "kb", Score: 0.9}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{TenantID: 3},
		PipelineState:   types.PipelineState{RerankResult: []*types.SearchResult{first, second}},
	}
	nextCalled := false

	require.Nil(t, plugin.OnEvent(context.Background(), types.CHUNK_FEEDBACK_WEIGHT, chatManage, func() *PluginError {
		nextCalled = true
		return nil
	}))
	assert.True(t, nextCalled)
	assert.Equal(t, []string{"first", "second"}, resultIDs(chatManage.RerankResult))
	assert.Equal(t, 0.2, first.Score)
	assert.Equal(t, 0.9, second.Score)
	assert.False(t, first.FeedbackWeightApplied)
	assert.False(t, second.FeedbackWeightApplied)
}

func TestFeedbackWeightHandlesDuplicatePointersAndNilResults(t *testing.T) {
	reader := &fakeRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "chunk", RecallWeight: 1.2},
	}}
	plugin := &PluginFeedbackWeight{chunkRepo: reader}
	result := &types.SearchResult{ID: "chunk", KnowledgeBaseID: "kb", Score: 0.5}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{TenantID: 1},
		PipelineState:   types.PipelineState{RerankResult: []*types.SearchResult{nil, result, result}},
	}

	require.Nil(t, plugin.OnEvent(context.Background(), types.CHUNK_FEEDBACK_WEIGHT, chatManage, func() *PluginError { return nil }))
	assert.InDelta(t, 0.6, result.Score, 1e-12)
	assert.Same(t, result, chatManage.RerankResult[0])
	assert.Same(t, result, chatManage.RerankResult[1])
	assert.Nil(t, chatManage.RerankResult[2])
	assert.Equal(t, 1, reader.calls)
	assert.Len(t, reader.scopes, 1)
}

func TestFeedbackWeightUsesContextTenantForLegacyZeroTenantTarget(t *testing.T) {
	reader := &fakeRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{TenantID: 7, KnowledgeBaseID: "kb", ChunkID: "chunk", RecallWeight: 1.2},
	}}
	plugin := &PluginFeedbackWeight{chunkRepo: reader}
	result := &types.SearchResult{ID: "chunk", KnowledgeBaseID: "kb", Score: 0.5}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{SearchTargets: types.SearchTargets{
			{KnowledgeBaseID: "kb", TenantID: 0},
		}},
		PipelineState: types.PipelineState{RerankResult: []*types.SearchResult{result}},
	}

	require.Nil(t, plugin.OnEvent(ctx, types.CHUNK_FEEDBACK_WEIGHT, chatManage, func() *PluginError { return nil }))
	require.Len(t, reader.scopes, 1)
	assert.Equal(t, uint64(7), reader.scopes[0].TenantID)
	assert.InDelta(t, 0.6, result.Score, 1e-12)
}

func TestFeedbackWeightSortsNonFiniteScoresDeterministically(t *testing.T) {
	reader := &fakeRecallWeightReader{}
	plugin := &PluginFeedbackWeight{chunkRepo: reader}
	finiteLow := &types.SearchResult{ID: "low", KnowledgeBaseID: "kb", Score: 0.1}
	nanResult := &types.SearchResult{ID: "nan", KnowledgeBaseID: "kb", Score: math.NaN()}
	finiteHigh := &types.SearchResult{ID: "high", KnowledgeBaseID: "kb", Score: 0.9}
	infResult := &types.SearchResult{ID: "inf", KnowledgeBaseID: "kb", Score: math.Inf(1)}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{TenantID: 1},
		PipelineState: types.PipelineState{RerankResult: []*types.SearchResult{
			finiteLow, nanResult, finiteHigh, infResult,
		}},
	}

	require.Nil(t, plugin.OnEvent(context.Background(), types.CHUNK_FEEDBACK_WEIGHT, chatManage, func() *PluginError { return nil }))
	assert.Equal(t, []string{"high", "low", "nan", "inf"}, resultIDs(chatManage.RerankResult))
}

func TestFeedbackWeightSurvivesMergeAndFinalTopK(t *testing.T) {
	manager := NewEventManager()
	reader := &fakeRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
		{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "high", RecallWeight: 2},
		{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "mid", RecallWeight: 1},
		{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "low", RecallWeight: 0.1},
	}}
	manager.Register(&PluginFeedbackWeight{chunkRepo: reader})
	manager.Register(&PluginMerge{})
	manager.Register(&PluginFilterTopK{})
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{TenantID: 1, RerankTopK: 2},
		PipelineState: types.PipelineState{RerankResult: []*types.SearchResult{
			{ID: "low", KnowledgeBaseID: "kb", KnowledgeID: "knowledge-low", Content: "low content", Score: 0.9, StartAt: 0, EndAt: 10},
			{ID: "high", KnowledgeBaseID: "kb", KnowledgeID: "knowledge-high", Content: "high content", Score: 0.5, StartAt: 0, EndAt: 12},
			{ID: "mid", KnowledgeBaseID: "kb", KnowledgeID: "knowledge-mid", Content: "middle content", Score: 0.6, StartAt: 0, EndAt: 14},
		}},
	}

	require.Nil(t, manager.Trigger(context.Background(), types.CHUNK_FEEDBACK_WEIGHT, chatManage))
	require.Nil(t, manager.Trigger(context.Background(), types.CHUNK_MERGE, chatManage))
	require.Len(t, chatManage.MergeResult, 3)
	assert.Equal(t, []string{"high", "mid", "low"}, resultIDs(chatManage.MergeResult))
	require.Nil(t, manager.Trigger(context.Background(), types.FILTER_TOP_K, chatManage))
	require.Len(t, chatManage.MergeResult, 2)
	assert.Equal(t, []string{"high", "mid"}, resultIDs(chatManage.MergeResult))
	assert.InDelta(t, 1.0, chatManage.MergeResult[0].Score, 1e-12)
	assert.InDelta(t, 0.6, chatManage.MergeResult[1].Score, 1e-12)
}

func TestFeedbackWeightStableTieSurvivesCrossDocumentMerge(t *testing.T) {
	for iteration := 0; iteration < 10; iteration++ {
		manager := NewEventManager()
		reader := &fakeRecallWeightReader{weights: []interfaces.ChunkRecallWeight{
			{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "first", RecallWeight: 2},
			{TenantID: 1, KnowledgeBaseID: "kb", ChunkID: "second", RecallWeight: 1},
		}}
		manager.Register(&PluginFeedbackWeight{chunkRepo: reader})
		manager.Register(&PluginMerge{})
		chatManage := &types.ChatManage{
			PipelineRequest: types.PipelineRequest{TenantID: 1},
			PipelineState: types.PipelineState{RerankResult: []*types.SearchResult{
				{ID: "first", KnowledgeBaseID: "kb", KnowledgeID: "knowledge-a", Content: "first content", Score: 0.5, StartAt: 0, EndAt: 10},
				{ID: "second", KnowledgeBaseID: "kb", KnowledgeID: "knowledge-b", Content: "second content", Score: 1, StartAt: 0, EndAt: 12},
			}},
		}

		require.Nil(t, manager.Trigger(context.Background(), types.CHUNK_FEEDBACK_WEIGHT, chatManage))
		require.Nil(t, manager.Trigger(context.Background(), types.CHUNK_MERGE, chatManage))
		assert.Equal(t, []string{"first", "second"}, resultIDs(chatManage.MergeResult))
	}
}

func resultIDs(results []*types.SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}
