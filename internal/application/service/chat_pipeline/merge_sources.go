package chatpipeline

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// appendStableSourceIDs appends non-empty IDs while preserving first-seen order.
func appendStableSourceIDs(dst []string, ids ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(ids))
	out := make([]string, 0, len(dst)+len(ids))
	appendID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range dst {
		appendID(id)
	}
	for _, id := range ids {
		appendID(id)
	}
	return out
}

// appendMergedResultSources records the merged result and all sources it had
// already accumulated, flattening nested merge provenance.
func appendMergedResultSources(dst []string, source *types.SearchResult) []string {
	if source == nil {
		return appendStableSourceIDs(dst)
	}
	dst = appendStableSourceIDs(dst, source.ID)
	return appendStableSourceIDs(dst, source.SubChunkID...)
}

type chunkContextPiece struct {
	id      string
	content string
}

// mergeChunkContextPieces concatenates ordered chunks with overlap removal and
// returns only IDs that contributed at least one rune before maxLen truncation.
func mergeChunkContextPieces(pieces []chunkContextPiece, maxLen int) (string, []string) {
	if maxLen <= 0 {
		return "", nil
	}

	var content string
	var included []string
	for _, piece := range pieces {
		if piece.content == "" {
			continue
		}
		before := runeLen(content)
		combined := concatNoOverlap(content, piece.content)
		after := runeLen(combined)
		if after > before && before < maxLen {
			included = appendStableSourceIDs(included, piece.id)
		}
		content = combined
		if after >= maxLen {
			break
		}
	}

	runes := []rune(content)
	if len(runes) > maxLen {
		runes = runes[:maxLen]
	}
	return string(runes), included
}
