package rageval_test

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/rageval"
)

func TestGoldEvidenceRoundTripAcrossLineEndingsAndUnicode(t *testing.T) {
	rawCRLF := "# 标题\r\n咖啡😀\r\nCafe\u0301\r尾声"
	rawLF := "# 标题\n咖啡😀\nCafé\n尾声"
	sourceA := rageval.NormalizeSource(rawCRLF)
	sourceB := rageval.NormalizeSource(rawLF)
	if sourceA.Text != sourceB.Text || sourceA.ContentHash != sourceB.ContentHash {
		t.Fatalf("equivalent source normalization diverged: %q != %q", sourceA.Text, sourceB.Text)
	}

	start := strings.Index(sourceA.Text, "咖啡")
	if start < 0 {
		t.Fatal("evidence not found")
	}
	// strings.Index is bytes; convert the prefix to the domain's rune offset.
	start = len([]rune(sourceA.Text[:start]))
	end := start + len([]rune("咖啡😀"))
	evidence, err := rageval.NewGoldEvidence("source-doc", sourceA, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Text != "咖啡😀" {
		t.Fatalf("evidence=%q", evidence.Text)
	}
	if err := evidence.ValidateRoundTrip(sourceB); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestDirectSourceExecutionMapping(t *testing.T) {
	source := rageval.NormalizeSource("alpha\r\n证据😀\r\nomega")
	execution := rageval.NormalizeSource("alpha\n证据😀\nomega")
	mapping, err := rageval.NewDirectMapping("source-1", "execution-9", source, execution)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := mapping.MapExecutionInterval("execution-9", 6, 9)
	if err != nil {
		t.Fatal(err)
	}
	if interval.SourceDocumentID != "source-1" || interval.SourceContentHash != source.ContentHash {
		t.Fatalf("unexpected mapped interval: %+v", interval)
	}

	changed := rageval.NormalizeSource("alpha\n不同\nomega")
	if _, err := rageval.NewDirectMapping("source-1", "execution-x", source, changed); err == nil {
		t.Fatal("expected changed execution source to be rejected")
	}
}
