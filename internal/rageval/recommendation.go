package rageval

import (
	"fmt"
	"sort"
)

const (
	RecommendationPolicyVersion      = "recommendation-v1"
	RecommendationMinCommonValid     = 30
	RecommendationMinPrecisionGain   = 0.03
	RecommendationMaxJudgeMetricDrop = 0.02
)

type VariantMetrics struct {
	VariantID              string   `json:"variant_id"`
	ContextPrecision       float64  `json:"context_precision"`
	EvidenceRecall         float64  `json:"evidence_recall"`
	GoldCoverage           float64  `json:"gold_coverage"`
	Faithfulness           float64  `json:"answer_faithfulness"`
	AnswerCorrectness      float64  `json:"answer_correctness"`
	RefusalQuality         float64  `json:"unanswerable_refusal_quality"`
	FailureRate            float64  `json:"failure_rate"`
	ValidCoverage          float64  `json:"valid_coverage"`
	CommonValidItems       int      `json:"common_valid_items"`
	JudgeCalibrationPassed bool     `json:"judge_calibration_passed"`
	ChunkCount             int      `json:"chunk_count"`
	IndexBuildMilliseconds int64    `json:"index_build_milliseconds"`
	EmbeddingCost          *float64 `json:"embedding_cost,omitempty"`
	IndexCost              *float64 `json:"index_cost,omitempty"`
}

type RecommendationDecision string

const (
	DecisionRecommend        RecommendationDecision = "recommend"
	DecisionNoBetterStrategy RecommendationDecision = "no_better_strategy"
)

type Recommendation struct {
	PolicyVersion string                 `json:"policy_version"`
	Decision      RecommendationDecision `json:"decision"`
	BaselineID    string                 `json:"baseline_id"`
	CandidateID   string                 `json:"candidate_id,omitempty"`
	ReasonCodes   []string               `json:"reason_codes"`
	Baseline      VariantMetrics         `json:"baseline"`
	Candidate     *VariantMetrics        `json:"candidate,omitempty"`
}

// SelectProvisionalCandidate is tuning-only. Holdout data is intentionally not
// accepted by this API and therefore cannot silently re-select a candidate.
func SelectProvisionalCandidate(baseline VariantMetrics, candidates []VariantMetrics) (VariantMetrics, error) {
	if len(candidates) == 0 || len(candidates) > 2 {
		return VariantMetrics{}, fmt.Errorf("tuning requires one or two candidates")
	}
	eligible := make([]VariantMetrics, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.CommonValidItems < RecommendationMinCommonValid ||
			candidate.ValidCoverage < baseline.ValidCoverage ||
			candidate.EvidenceRecall < baseline.EvidenceRecall ||
			candidate.GoldCoverage < baseline.GoldCoverage ||
			candidate.FailureRate > baseline.FailureRate {
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return VariantMetrics{}, fmt.Errorf("no provisional candidate passed tuning guards")
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].ContextPrecision == eligible[j].ContextPrecision {
			return eligible[i].VariantID < eligible[j].VariantID
		}
		return eligible[i].ContextPrecision > eligible[j].ContextPrecision
	})
	return eligible[0], nil
}

// EvaluateHoldout compares exactly the frozen baseline and provisional
// candidate on common-valid holdout items.
func EvaluateHoldout(baseline, provisional VariantMetrics) Recommendation {
	decision := Recommendation{
		PolicyVersion: RecommendationPolicyVersion,
		Decision:      DecisionNoBetterStrategy,
		BaselineID:    baseline.VariantID,
		Baseline:      baseline,
		CandidateID:   provisional.VariantID,
		Candidate:     &provisional,
	}
	if provisional.CommonValidItems < RecommendationMinCommonValid {
		decision.ReasonCodes = append(decision.ReasonCodes, "COMMON_VALID_ITEMS_INSUFFICIENT")
	}
	if provisional.ValidCoverage < baseline.ValidCoverage {
		decision.ReasonCodes = append(decision.ReasonCodes, "CANDIDATE_VALID_COVERAGE_DEGRADED")
	}
	if provisional.ContextPrecision-baseline.ContextPrecision < RecommendationMinPrecisionGain {
		decision.ReasonCodes = append(decision.ReasonCodes, "CONTEXT_PRECISION_GAIN_INSUFFICIENT")
	}
	if provisional.EvidenceRecall < baseline.EvidenceRecall {
		decision.ReasonCodes = append(decision.ReasonCodes, "EVIDENCE_RECALL_DEGRADED")
	}
	if provisional.GoldCoverage < baseline.GoldCoverage {
		decision.ReasonCodes = append(decision.ReasonCodes, "GOLD_COVERAGE_DEGRADED")
	}
	if provisional.FailureRate > baseline.FailureRate {
		decision.ReasonCodes = append(decision.ReasonCodes, "FAILURE_RATE_INCREASED")
	}
	if baseline.JudgeCalibrationPassed && provisional.JudgeCalibrationPassed {
		if provisional.Faithfulness < baseline.Faithfulness-RecommendationMaxJudgeMetricDrop {
			decision.ReasonCodes = append(decision.ReasonCodes, "FAITHFULNESS_DEGRADED")
		}
		if provisional.AnswerCorrectness < baseline.AnswerCorrectness-RecommendationMaxJudgeMetricDrop {
			decision.ReasonCodes = append(decision.ReasonCodes, "ANSWER_CORRECTNESS_DEGRADED")
		}
		if provisional.RefusalQuality < baseline.RefusalQuality-RecommendationMaxJudgeMetricDrop {
			decision.ReasonCodes = append(decision.ReasonCodes, "REFUSAL_QUALITY_DEGRADED")
		}
	}
	if len(decision.ReasonCodes) == 0 {
		decision.Decision = DecisionRecommend
	}
	return decision
}
