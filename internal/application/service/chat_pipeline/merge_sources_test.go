package chatpipeline

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMergeOverlappingChunksFlattensNestedSources(t *testing.T) {
	plugin := &PluginMerge{}
	base := &types.SearchResult{
		ID: "a", Content: "abcdefghij", StartAt: 0, EndAt: 10,
		SubChunkID: []string{"base-source"},
	}
	overlap := &types.SearchResult{
		ID: "b", Content: "hijklmnop", StartAt: 7, EndAt: 16,
		SubChunkID: []string{"c", "d", "c", ""},
	}
	contained := &types.SearchResult{
		ID: "e", Content: "ijk", StartAt: 8, EndAt: 11,
		SubChunkID: []string{"f", "d"},
	}

	got := plugin.mergeOverlappingChunks(
		context.Background(), "knowledge-1",
		[]*types.SearchResult{base, overlap, contained},
	)
	if len(got) != 1 {
		t.Fatalf("merged results = %#v", got)
	}
	wantSources := []string{"base-source", "b", "c", "d", "e", "f"}
	if !reflect.DeepEqual(got[0].SubChunkID, wantSources) {
		t.Fatalf("sources = %#v, want %#v", got[0].SubChunkID, wantSources)
	}
}

func TestMergeChunkContextPiecesTracksTruncationParticipation(t *testing.T) {
	t.Run("partial neighbor counts", func(t *testing.T) {
		content, ids := mergeChunkContextPieces([]chunkContextPiece{
			{id: "previous", content: strings.Repeat("a", 800)},
			{id: "base", content: strings.Repeat("b", 100)},
			{id: "next", content: strings.Repeat("c", 100)},
		}, 850)
		if runeLen(content) != 850 {
			t.Fatalf("content length = %d", runeLen(content))
		}
		if !reflect.DeepEqual(ids, []string{"previous", "base"}) {
			t.Fatalf("included IDs = %#v", ids)
		}
	})

	t.Run("fully truncated neighbor does not count", func(t *testing.T) {
		content, ids := mergeChunkContextPieces([]chunkContextPiece{
			{id: "previous", content: strings.Repeat("a", 850)},
			{id: "base", content: strings.Repeat("b", 100)},
		}, 850)
		if runeLen(content) != 850 {
			t.Fatalf("content length = %d", runeLen(content))
		}
		if !reflect.DeepEqual(ids, []string{"previous"}) {
			t.Fatalf("included IDs = %#v", ids)
		}
	})
}

func TestAppendMergedResultSourcesIsStableAndDeduplicated(t *testing.T) {
	got := appendMergedResultSources(
		[]string{"existing", "nested"},
		&types.SearchResult{
			ID:         "merged",
			SubChunkID: []string{"nested", "leaf", "", "merged"},
		},
	)
	want := []string{"existing", "nested", "merged", "leaf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %#v, want %#v", got, want)
	}
}

func TestExpandShortContextWithNeighborsTracksActualSources(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf(
		"file:merge-sources-%d?mode=memory&cache=shared", time.Now().UnixNano(),
	)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&types.Chunk{}); err != nil {
		t.Fatalf("migrate chunks: %v", err)
	}

	t.Run("multi-level expansion", func(t *testing.T) {
		chunks := []*types.Chunk{
			{
				ID: "prev-2", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "doc",
				Content: strings.Repeat("a", 50), ChunkType: types.ChunkTypeText, NextChunkID: "prev-1",
			},
			{
				ID: "prev-1", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "doc",
				Content: strings.Repeat("b", 50), ChunkType: types.ChunkTypeText,
				PreChunkID: "prev-2", NextChunkID: "base",
			},
			{
				ID: "base", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "doc",
				Content: strings.Repeat("c", 50), ChunkType: types.ChunkTypeText,
				PreChunkID: "prev-1", NextChunkID: "next-1",
			},
			{
				ID: "next-1", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "doc",
				Content: strings.Repeat("d", 50), ChunkType: types.ChunkTypeText,
				PreChunkID: "base", NextChunkID: "next-2",
			},
			{
				ID: "next-2", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "doc",
				Content: strings.Repeat("e", 50), ChunkType: types.ChunkTypeText, PreChunkID: "next-1",
			},
		}
		if err := db.Create(chunks).Error; err != nil {
			t.Fatalf("create chunks: %v", err)
		}
		plugin := &PluginMerge{chunkRepo: repository.NewChunkRepository(db)}
		ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
		results := plugin.expandShortContextWithNeighbors(ctx, &types.ChatManage{}, []*types.SearchResult{{
			ID: "base", KnowledgeID: "doc", Content: strings.Repeat("c", 50),
			ChunkType: string(types.ChunkTypeText),
		}})
		if len(results) != 1 {
			t.Fatalf("results = %#v", results)
		}
		want := []string{"prev-2", "prev-1", "next-1", "next-2"}
		if !reflect.DeepEqual(results[0].SubChunkID, want) {
			t.Fatalf("sources = %#v, want %#v", results[0].SubChunkID, want)
		}
	})

	t.Run("fully truncated next neighbor is omitted", func(t *testing.T) {
		if err := db.Where("id IN ?", []string{"truncate-prev", "truncate-base", "truncate-next"}).
			Delete(&types.Chunk{}).Error; err != nil {
			t.Fatalf("clear truncate chunks: %v", err)
		}
		chunks := []*types.Chunk{
			{
				ID: "truncate-prev", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "truncate-doc",
				Content: strings.Repeat("p", 750), ChunkType: types.ChunkTypeText, NextChunkID: "truncate-base",
			},
			{
				ID: "truncate-base", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "truncate-doc",
				Content: strings.Repeat("b", 100), ChunkType: types.ChunkTypeText,
				PreChunkID: "truncate-prev", NextChunkID: "truncate-next",
			},
			{
				ID: "truncate-next", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "truncate-doc",
				Content: strings.Repeat("n", 100), ChunkType: types.ChunkTypeText, PreChunkID: "truncate-base",
			},
		}
		if err := db.Create(chunks).Error; err != nil {
			t.Fatalf("create truncate chunks: %v", err)
		}
		plugin := &PluginMerge{chunkRepo: repository.NewChunkRepository(db)}
		ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
		results := plugin.expandShortContextWithNeighbors(ctx, &types.ChatManage{}, []*types.SearchResult{{
			ID: "truncate-base", KnowledgeID: "truncate-doc", Content: strings.Repeat("b", 100),
			ChunkType: string(types.ChunkTypeText),
		}})
		if len(results) != 1 {
			t.Fatalf("results = %#v", results)
		}
		if !reflect.DeepEqual(results[0].SubChunkID, []string{"truncate-prev"}) {
			t.Fatalf("sources = %#v, want only contributing previous chunk", results[0].SubChunkID)
		}
		if strings.Contains(results[0].Content, "n") {
			t.Fatal("fully truncated next chunk content entered final context")
		}
	})
}

func TestResolveParentChunksRecordsContextSource(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf(
		"file:parent-sources-%d?mode=memory&cache=shared", time.Now().UnixNano(),
	)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&types.Chunk{}); err != nil {
		t.Fatalf("migrate chunks: %v", err)
	}
	parent := &types.Chunk{
		ID: "parent", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "doc",
		Content: "parent context", ChunkType: types.ChunkTypeParentText, StartAt: 0, EndAt: 14,
	}
	if err := db.Create(parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}
	plugin := &PluginMerge{chunkRepo: repository.NewChunkRepository(db)}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	results := plugin.resolveParentChunks(ctx, &types.ChatManage{}, []*types.SearchResult{{
		ID: "child", ParentChunkID: parent.ID, KnowledgeID: "doc",
		Content: "context", ChunkType: string(types.ChunkTypeText),
		SubChunkID: []string{"nested-source"},
	}})
	if len(results) != 1 || !reflect.DeepEqual(results[0].SubChunkID, []string{"nested-source", "parent"}) {
		t.Fatalf("parent sources = %#v", results)
	}
}
