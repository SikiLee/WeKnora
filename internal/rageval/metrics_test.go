package rageval

import (
	"math"
	"testing"
)

func metricValue(t *testing.T, metrics map[string]MetricResult, name string) float64 {
	t.Helper()
	metric, ok := metrics[name]
	if !ok || metric.Status != MetricValid || metric.Value == nil {
		t.Fatalf("metric %s is not valid: %+v", name, metric)
	}
	return *metric.Value
}

func assertClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.12f want %.12f", got, want)
	}
}

func TestRetrievalMetricSetFixedExample(t *testing.T) {
	gold := []GoldEvidence{
		{SourceDocumentID: "doc-A", SourceContentHash: "hash", NormalizationVersion: "v1", OffsetUnit: "rune", StartAt: 100, EndAt: 140},
		{SourceDocumentID: "doc-A", SourceContentHash: "hash", NormalizationVersion: "v1", OffsetUnit: "rune", StartAt: 300, EndAt: 340},
	}
	contexts := []RetrievedContext{
		{ExecutionDocumentID: "exec-A", SourceInterval: SourceInterval{SourceDocumentID: "doc-A", SourceContentHash: "hash", NormalizationVersion: "v1", OffsetUnit: "rune", StartAt: 90, EndAt: 130}, Rank: 1},
		{ExecutionDocumentID: "exec-B", SourceInterval: SourceInterval{SourceDocumentID: "doc-B", SourceContentHash: "other", NormalizationVersion: "v1", OffsetUnit: "rune", StartAt: 0, EndAt: 100}, Rank: 2},
		{ExecutionDocumentID: "exec-A", SourceInterval: SourceInterval{SourceDocumentID: "doc-A", SourceContentHash: "hash", NormalizationVersion: "v1", OffsetUnit: "rune", StartAt: 300, EndAt: 320}, Rank: 3},
	}
	metrics := RetrievalMetricSet(Answerable, gold, contexts, 3)
	assertClose(t, metricValue(t, metrics, "hit@3"), 1)
	assertClose(t, metricValue(t, metrics, "evidence_recall@3"), 1)
	assertClose(t, metricValue(t, metrics, "mrr@3"), 1)
	assertClose(t, metricValue(t, metrics, "context_precision@3"), 5.0/6.0)
	assertClose(t, metricValue(t, metrics, "gold_coverage@3"), 0.625)
	assertClose(t, metricValue(t, metrics, "chunk_redundancy@3"), 0)
}

func TestChunkRedundancyUsesSourceIntervalUnion(t *testing.T) {
	gold := []GoldEvidence{{SourceDocumentID: "doc", SourceContentHash: "h", NormalizationVersion: "v", OffsetUnit: "rune", StartAt: 0, EndAt: 100}}
	contexts := []RetrievedContext{
		{ExecutionDocumentID: "exec", SourceInterval: SourceInterval{SourceDocumentID: "doc", SourceContentHash: "h", NormalizationVersion: "v", OffsetUnit: "rune", StartAt: 0, EndAt: 40}, Rank: 1},
		{ExecutionDocumentID: "exec", SourceInterval: SourceInterval{SourceDocumentID: "doc", SourceContentHash: "h", NormalizationVersion: "v", OffsetUnit: "rune", StartAt: 20, EndAt: 60}, Rank: 2},
	}
	metrics := RetrievalMetricSet(Answerable, gold, contexts, 2)
	// total=80, union=60, redundancy=1-60/80=0.25
	assertClose(t, metricValue(t, metrics, "chunk_redundancy@2"), 0.25)
}

func TestUnanswerableGoldDependentMetricsAbstain(t *testing.T) {
	metrics := RetrievalMetricSet(Unanswerable, nil, nil, 5)
	for name, metric := range metrics {
		if metric.Status != MetricAbstain || metric.Value != nil || metric.ReasonCode != ReasonNotApplicableUnanswerable {
			t.Fatalf("%s: unexpected metric %+v", name, metric)
		}
	}
}

func TestAnswerableMissingGoldIsInvalidNotZero(t *testing.T) {
	metrics := RetrievalMetricSet(Answerable, nil, nil, 5)
	for name, metric := range metrics {
		if metric.Status != MetricInvalid || metric.Value != nil || metric.ReasonCode != ReasonGoldEvidenceInvalid {
			t.Fatalf("%s: unexpected metric %+v", name, metric)
		}
	}
}

func TestValidNoHitIsLegitimateZero(t *testing.T) {
	gold := []GoldEvidence{{SourceDocumentID: "doc", SourceContentHash: "h", NormalizationVersion: "v", OffsetUnit: "rune", StartAt: 0, EndAt: 10}}
	contexts := []RetrievedContext{{ExecutionDocumentID: "exec", SourceInterval: SourceInterval{SourceDocumentID: "other", SourceContentHash: "x", NormalizationVersion: "v", OffsetUnit: "rune", StartAt: 0, EndAt: 5}, Rank: 1}}
	metrics := RetrievalMetricSet(Answerable, gold, contexts, 1)
	for _, name := range []string{"hit@1", "evidence_recall@1", "mrr@1", "context_precision@1", "gold_coverage@1"} {
		if got := metricValue(t, metrics, name); got != 0 {
			t.Fatalf("%s=%v want zero", name, got)
		}
	}
}

func TestMissingSourceMappingInvalidatesMetrics(t *testing.T) {
	gold := []GoldEvidence{{SourceDocumentID: "doc", SourceContentHash: "h", NormalizationVersion: "v", OffsetUnit: "rune", StartAt: 0, EndAt: 10}}
	contexts := []RetrievedContext{{ExecutionDocumentID: "exec", SourceInterval: SourceInterval{StartAt: 0, EndAt: 5}, Rank: 1}}
	metrics := RetrievalMetricSet(Answerable, gold, contexts, 1)
	for name, metric := range metrics {
		if metric.Status != MetricInvalid || metric.Value != nil || metric.ReasonCode != ReasonSourceMappingInvalid {
			t.Fatalf("%s: unexpected metric %+v", name, metric)
		}
	}
}
