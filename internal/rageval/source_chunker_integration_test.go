//go:build cgo

package rageval_test

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/rageval"
)

// The repository's chunker currently reaches a cgo-backed SQL validation
// dependency through its logger path. Normal CI/Linux cgo builds execute this
// integration check; the Windows no-cgo developer environment still runs the
// pure normalization and mapping tests in source_test.go.
func TestExistingChunkerPreservesNormalizedRuneOffsets(t *testing.T) {
	source := rageval.NormalizeSource("# 标题\r\n第一段😀。\r\n\r\n## 子标题\r\n第二段 Café。")
	chunks := chunker.Split(source.Text, chunker.SplitterConfig{
		Strategy:     chunker.StrategyHeading,
		ChunkSize:    12,
		ChunkOverlap: 2,
	})
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	runes := []rune(source.Text)
	for i, c := range chunks {
		if c.Start < 0 || c.End > len(runes) || c.End <= c.Start {
			t.Fatalf("chunk %d invalid interval [%d,%d)", i, c.Start, c.End)
		}
		if got := string(runes[c.Start:c.End]); got != c.Content {
			t.Fatalf("chunk %d round trip mismatch: %q != %q", i, got, c.Content)
		}
	}
}
