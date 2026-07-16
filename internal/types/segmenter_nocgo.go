//go:build !cgo

package types

import (
	"strings"
	"unicode"
)

type runeSegmenter struct{}

func newChineseSegmenter() ChineseSegmenter { return runeSegmenter{} }

func (runeSegmenter) Cut(text string, _ bool) []string { return splitSearchRunes(text) }

func (runeSegmenter) CutForSearch(text string, _ bool) []string { return splitSearchRunes(text) }

func splitSearchRunes(text string) []string {
	result := make([]string, 0, len([]rune(text)))
	var latin strings.Builder
	flush := func() {
		if latin.Len() > 0 {
			result = append(result, latin.String())
			latin.Reset()
		}
	}
	for _, current := range text {
		switch {
		case unicode.Is(unicode.Han, current):
			flush()
			result = append(result, string(current))
		case unicode.IsLetter(current) || unicode.IsDigit(current):
			latin.WriteRune(current)
		default:
			flush()
		}
	}
	flush()
	return result
}
