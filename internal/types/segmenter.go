package types

// ChineseSegmenter is the small surface used by retrieval and legacy metrics.
// The normal build uses Jieba; no-CGO builds use a deterministic conservative
// fallback so unrelated packages remain buildable.
type ChineseSegmenter interface {
	Cut(string, bool) []string
	CutForSearch(string, bool) []string
}

var Jieba ChineseSegmenter = newChineseSegmenter()
