package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginFilterTopKSortsMergeResultsBeforeTruncation(t *testing.T) {
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankTopK: 3},
		PipelineState: types.PipelineState{
			MergeResult: []*types.SearchResult{
				{ID: "low", KnowledgeID: "doc-c", Score: 0.2},
				{ID: "high", KnowledgeID: "doc-a", Score: 0.9},
				{ID: "medium", KnowledgeID: "doc-b", Score: 0.5},
				{ID: "second", KnowledgeID: "doc-d", Score: 0.8},
			},
		},
	}

	plugin := &PluginFilterTopK{}
	err := plugin.OnEvent(
		context.Background(),
		types.FILTER_TOP_K,
		chatManage,
		func() *PluginError { return nil },
	)

	require.Nil(t, err)
	require.Len(t, chatManage.MergeResult, 3)
	assert.Equal(t, []string{"high", "second", "medium"}, searchResultIDs(chatManage.MergeResult))
}

func TestPluginFilterTopKUsesDeterministicTieBreakers(t *testing.T) {
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankTopK: 10},
		PipelineState: types.PipelineState{
			MergeResult: []*types.SearchResult{
				{ID: "chunk-b", KnowledgeID: "doc-b", ChunkType: "text", StartAt: 10, EndAt: 20, Score: 0.8},
				{ID: "chunk-c", KnowledgeID: "doc-a", ChunkType: "summary", StartAt: 0, EndAt: 10, Score: 0.8},
				{ID: "chunk-a", KnowledgeID: "doc-a", ChunkType: "text", StartAt: 0, EndAt: 10, Score: 0.8},
			},
		},
	}

	plugin := &PluginFilterTopK{}
	err := plugin.OnEvent(
		context.Background(),
		types.FILTER_TOP_K,
		chatManage,
		func() *PluginError { return nil },
	)

	require.Nil(t, err)
	assert.Equal(t, []string{"chunk-c", "chunk-a", "chunk-b"}, searchResultIDs(chatManage.MergeResult))
}

func TestPluginFilterTopKAppliesRecallWeightOnlyAtFinalCutoff(t *testing.T) {
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankTopK: 2},
		PipelineState: types.PipelineState{
			MergeResult: []*types.SearchResult{
				{ID: "neutral", KnowledgeID: "doc-a", Score: 0.90, RecallWeight: 1},
				{ID: "promoted", KnowledgeID: "doc-b", Score: 0.80, RecallWeight: 1.2},
				{ID: "demoted", KnowledgeID: "doc-c", Score: 0.95, RecallWeight: 0.8},
			},
		},
	}

	err := (&PluginFilterTopK{}).OnEvent(
		context.Background(), types.FILTER_TOP_K, chatManage,
		func() *PluginError { return nil },
	)

	require.Nil(t, err)
	assert.Equal(t, []string{"promoted", "neutral"}, searchResultIDs(chatManage.MergeResult))
	assert.Equal(t, 0.80, chatManage.MergeResult[0].Score, "raw score must remain observable")
}

func TestPluginFilterTopKZeroWeightPreservesLegacyOrder(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "b", KnowledgeID: "doc-b", Score: 0.7},
		{ID: "a", KnowledgeID: "doc-a", Score: 0.7},
		{ID: "high", KnowledgeID: "doc-z", Score: 0.9},
	}
	sortSearchResultsDeterministically(results)
	assert.Equal(t, []string{"high", "a", "b"}, searchResultIDs(results))
	assert.False(t, hasFeedbackWeight(results))
}

func TestHasFeedbackWeightRecognizesBoundedProjection(t *testing.T) {
	assert.True(t, hasFeedbackWeight([]*types.SearchResult{{RecallWeight: 1.2}}))
	assert.True(t, hasFeedbackWeight([]*types.SearchResult{{RecallWeight: 0.8}}))
	assert.False(t, hasFeedbackWeight([]*types.SearchResult{{RecallWeight: 1}}))
	assert.False(t, hasFeedbackWeight([]*types.SearchResult{{RecallWeight: 0}}))
}

func searchResultIDs(results []*types.SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}
