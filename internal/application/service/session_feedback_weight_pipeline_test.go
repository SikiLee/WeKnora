package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestRAGPipelinePlacesFeedbackWeightAfterRerankAndWebFetch(t *testing.T) {
	withWeb := buildRAGPipeline(true, true, true)
	assertStageOrder(t, withWeb, types.CHUNK_RERANK, types.WEB_FETCH)
	assertStageOrder(t, withWeb, types.WEB_FETCH, types.CHUNK_FEEDBACK_WEIGHT)
	assertStageOrder(t, withWeb, types.CHUNK_FEEDBACK_WEIGHT, types.CHUNK_MERGE)
	assertStageOrder(t, withWeb, types.CHUNK_FEEDBACK_WEIGHT, types.FILTER_TOP_K)

	withoutWeb := buildRAGPipeline(false, false, false)
	assert.NotContains(t, withoutWeb, types.WEB_FETCH)
	assertStageOrder(t, withoutWeb, types.CHUNK_RERANK, types.CHUNK_FEEDBACK_WEIGHT)
	assertStageOrder(t, withoutWeb, types.CHUNK_FEEDBACK_WEIGHT, types.CHUNK_MERGE)
}

func TestSearchKnowledgePipelineIncludesFeedbackWeightBeforeMergeAndTopK(t *testing.T) {
	pipeline := buildSearchKnowledgePipeline()
	assertStageOrder(t, pipeline, types.CHUNK_RERANK, types.CHUNK_FEEDBACK_WEIGHT)
	assertStageOrder(t, pipeline, types.CHUNK_FEEDBACK_WEIGHT, types.CHUNK_MERGE)
	assertStageOrder(t, pipeline, types.CHUNK_FEEDBACK_WEIGHT, types.FILTER_TOP_K)
}

func TestStaticRAGPipelineIncludesFeedbackWeightBeforeMerge(t *testing.T) {
	pipeline := types.Pipeline["rag"]
	assertStageOrder(t, pipeline, types.CHUNK_RERANK, types.CHUNK_FEEDBACK_WEIGHT)
	assertStageOrder(t, pipeline, types.CHUNK_FEEDBACK_WEIGHT, types.CHUNK_MERGE)
}

func assertStageOrder(t *testing.T, pipeline []types.EventType, before, after types.EventType) {
	t.Helper()
	positions := make(map[types.EventType]int, len(pipeline))
	for i, stage := range pipeline {
		positions[stage] = i
	}
	assert.Contains(t, positions, before)
	assert.Contains(t, positions, after)
	assert.Less(t, positions[before], positions[after])
}
