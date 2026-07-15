package rageval

import (
	"fmt"
	"sort"
)

type Answerability string

const (
	Answerable   Answerability = "answerable"
	Unanswerable Answerability = "unanswerable"
)

type MetricStatus string

const (
	MetricValid   MetricStatus = "valid"
	MetricInvalid MetricStatus = "invalid"
	MetricAbstain MetricStatus = "abstain"
)

const (
	ReasonNotApplicableUnanswerable = "NOT_APPLICABLE_UNANSWERABLE"
	ReasonGoldEvidenceInvalid       = "GOLD_EVIDENCE_INVALID"
	ReasonSourceMappingInvalid      = "SOURCE_IDENTITY_MAPPING_INVALID"
)

type MetricResult struct {
	Name       string         `json:"metric_name"`
	Status     MetricStatus   `json:"status"`
	Value      *float64       `json:"value"`
	ReasonCode string         `json:"reason_code,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

type RetrievedContext struct {
	ExecutionDocumentID string `json:"execution_document_id"`
	ExecutionChunkID    string `json:"execution_chunk_id"`
	SourceInterval
	Rank      int     `json:"rank"`
	Score     float64 `json:"score"`
	MatchType string  `json:"match_type,omitempty"`
}

// RetrievalMetricSet computes all source-grounded metrics for one K. For
// unanswerable cases they are explicitly inapplicable, not successful zeros.
func RetrievalMetricSet(
	answerability Answerability,
	gold []GoldEvidence,
	contexts []RetrievedContext,
	k int,
) map[string]MetricResult {
	names := []string{
		fmt.Sprintf("hit@%d", k),
		fmt.Sprintf("evidence_recall@%d", k),
		fmt.Sprintf("mrr@%d", k),
		fmt.Sprintf("context_precision@%d", k),
		fmt.Sprintf("gold_coverage@%d", k),
		fmt.Sprintf("chunk_redundancy@%d", k),
	}
	results := make(map[string]MetricResult, len(names))
	if answerability == Unanswerable {
		for _, name := range names {
			results[name] = nonNumericMetric(name, MetricAbstain, ReasonNotApplicableUnanswerable)
		}
		return results
	}
	if answerability != Answerable || len(gold) == 0 || !validGold(gold) {
		for _, name := range names {
			results[name] = nonNumericMetric(name, MetricInvalid, ReasonGoldEvidenceInvalid)
		}
		return results
	}
	if k < 1 {
		for _, name := range names {
			results[name] = nonNumericMetric(name, MetricInvalid, "INVALID_METRIC_K")
		}
		return results
	}

	top := topK(contexts, k)
	if !validContexts(top) {
		for _, name := range names {
			results[name] = nonNumericMetric(name, MetricInvalid, ReasonSourceMappingInvalid)
		}
		return results
	}

	relevant := make([]bool, len(top))
	hitGold := make([]bool, len(gold))
	firstRank := 0
	relevantCount := 0
	precisionSum := 0.0
	for i, ctx := range top {
		for j, evidence := range gold {
			if overlaps(ctx.SourceInterval, evidence) {
				relevant[i] = true
				hitGold[j] = true
			}
		}
		if relevant[i] {
			relevantCount++
			if firstRank == 0 {
				firstRank = i + 1
			}
			precisionSum += float64(relevantCount) / float64(i+1)
		}
	}

	hit := 0.0
	if firstRank > 0 {
		hit = 1
	}
	hitCount := 0
	for _, matched := range hitGold {
		if matched {
			hitCount++
		}
	}
	recall := float64(hitCount) / float64(len(gold))
	mrr := 0.0
	if firstRank > 0 {
		mrr = 1 / float64(firstRank)
	}
	contextPrecision := 0.0
	if relevantCount > 0 {
		contextPrecision = precisionSum / float64(relevantCount)
	}
	coverage := goldCoverage(gold, top)
	redundancy := chunkRedundancy(top)

	results[names[0]] = numericMetric(names[0], hit)
	results[names[1]] = numericMetric(names[1], recall)
	results[names[2]] = numericMetric(names[2], mrr)
	results[names[3]] = numericMetric(names[3], contextPrecision)
	results[names[4]] = numericMetric(names[4], coverage)
	results[names[5]] = numericMetric(names[5], redundancy)
	return results
}

func numericMetric(name string, value float64) MetricResult {
	return MetricResult{Name: name, Status: MetricValid, Value: &value}
}

func nonNumericMetric(name string, status MetricStatus, reason string) MetricResult {
	return MetricResult{Name: name, Status: status, Value: nil, ReasonCode: reason}
}

func validGold(gold []GoldEvidence) bool {
	for _, g := range gold {
		if g.SourceDocumentID == "" || g.SourceContentHash == "" ||
			g.NormalizationVersion == "" || g.OffsetUnit == "" ||
			g.StartAt < 0 || g.EndAt <= g.StartAt {
			return false
		}
	}
	return true
}

func validContexts(contexts []RetrievedContext) bool {
	for _, ctx := range contexts {
		if ctx.ExecutionDocumentID == "" || ctx.SourceDocumentID == "" ||
			ctx.SourceContentHash == "" || ctx.NormalizationVersion == "" || ctx.OffsetUnit == "" ||
			ctx.StartAt < 0 || ctx.EndAt <= ctx.StartAt {
			return false
		}
	}
	return true
}

func topK(contexts []RetrievedContext, k int) []RetrievedContext {
	result := append([]RetrievedContext(nil), contexts...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Rank == result[j].Rank {
			return i < j
		}
		return result[i].Rank < result[j].Rank
	})
	if len(result) > k {
		result = result[:k]
	}
	return result
}

func overlaps(ctx SourceInterval, evidence GoldEvidence) bool {
	return ctx.SourceDocumentID == evidence.SourceDocumentID &&
		ctx.SourceContentHash == evidence.SourceContentHash &&
		ctx.NormalizationVersion == evidence.NormalizationVersion &&
		ctx.OffsetUnit == evidence.OffsetUnit &&
		maxInt(ctx.StartAt, evidence.StartAt) < minInt(ctx.EndAt, evidence.EndAt)
}

func goldCoverage(gold []GoldEvidence, contexts []RetrievedContext) float64 {
	covered := 0
	total := 0
	for _, evidence := range gold {
		total += evidence.EndAt - evidence.StartAt
		intervals := make([]interval, 0)
		for _, ctx := range contexts {
			if !overlaps(ctx.SourceInterval, evidence) {
				continue
			}
			intervals = append(intervals, interval{
				start: maxInt(ctx.StartAt, evidence.StartAt),
				end:   minInt(ctx.EndAt, evidence.EndAt),
			})
		}
		covered += unionLength(intervals)
	}
	if total == 0 {
		return 0
	}
	return float64(covered) / float64(total)
}

func chunkRedundancy(contexts []RetrievedContext) float64 {
	total := 0
	grouped := make(map[string][]interval)
	for _, ctx := range contexts {
		length := ctx.EndAt - ctx.StartAt
		total += length
		key := ctx.SourceDocumentID + "\x00" + ctx.SourceContentHash + "\x00" + ctx.NormalizationVersion
		grouped[key] = append(grouped[key], interval{start: ctx.StartAt, end: ctx.EndAt})
	}
	if total == 0 {
		return 0
	}
	unique := 0
	for _, intervals := range grouped {
		unique += unionLength(intervals)
	}
	return 1 - float64(unique)/float64(total)
}

type interval struct{ start, end int }

func unionLength(intervals []interval) int {
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start == intervals[j].start {
			return intervals[i].end < intervals[j].end
		}
		return intervals[i].start < intervals[j].start
	})
	total := 0
	start, end := intervals[0].start, intervals[0].end
	for _, current := range intervals[1:] {
		if current.start <= end {
			if current.end > end {
				end = current.end
			}
			continue
		}
		total += end - start
		start, end = current.start, current.end
	}
	return total + end - start
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
