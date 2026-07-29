package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSearchableChunkSkipsUnsynchronizedEdits(t *testing.T) {
	service := &knowledgeBaseService{}
	for _, status := range []string{"processing", "failed"} {
		chunk := &types.Chunk{ChunkType: types.ChunkTypeText, IndexStatus: status, IsEnabled: true}
		if service.isSearchableChunk(chunk) {
			t.Fatalf("chunk with index status %q should not be searchable", status)
		}
	}
	for _, status := range []string{"", "ready"} {
		chunk := &types.Chunk{ChunkType: types.ChunkTypeText, IndexStatus: status, IsEnabled: true}
		if !service.isSearchableChunk(chunk) {
			t.Fatalf("chunk with index status %q should be searchable", status)
		}
	}
}

func TestIsSearchableChunkSkipsDisabledChunk(t *testing.T) {
	service := &knowledgeBaseService{}
	chunk := &types.Chunk{
		ChunkType:   types.ChunkTypeFAQ,
		IndexStatus: "ready",
		IsEnabled:   false,
	}
	if service.isSearchableChunk(chunk) {
		t.Fatal("disabled FAQ chunk should never be searchable")
	}
}

func TestParentEnrichmentKeepsInheritedScoreAndWeightTogether(t *testing.T) {
	service := &knowledgeBaseService{}
	child := &types.Chunk{
		ID: "child", KnowledgeID: "knowledge", ParentChunkID: "parent",
		ChunkType: types.ChunkTypeText, IsEnabled: true, IndexStatus: "ready",
		RecallWeight: 1.2,
	}
	parent := &types.Chunk{
		ID: "parent", KnowledgeID: "knowledge", ChunkType: types.ChunkTypeText,
		IsEnabled: true, IndexStatus: "ready", RecallWeight: 0.8,
	}
	input := []*types.IndexWithScore{{
		ChunkID: child.ID, KnowledgeID: child.KnowledgeID, Score: 0.9,
	}}
	index := service.buildChunkIndex(input)
	require.Equal(t, []string{"parent"}, service.collectEnrichmentChunkIDs(
		t.Context(), []*types.Chunk{child}, index,
	))

	results := service.assembleSearchResults(
		t.Context(),
		input,
		map[string]*types.Chunk{child.ID: child, parent.ID: parent},
		map[string]*types.Knowledge{"knowledge": {
			ID: "knowledge", KnowledgeBaseID: "kb", Title: "document",
		}},
		index,
		false,
	)

	var parentResult *types.SearchResult
	for _, result := range results {
		if result.ID == parent.ID {
			parentResult = result
			break
		}
	}
	require.NotNil(t, parentResult)
	assert.Equal(t, 0.9, parentResult.Score)
	assert.Equal(t, 1.2, parentResult.RecallWeight,
		"the parent result inherits both score and recall weight from the child hit")
}
