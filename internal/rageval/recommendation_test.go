package rageval

import "testing"

func baselineMetrics() VariantMetrics {
	return VariantMetrics{
		VariantID: "baseline", ContextPrecision: .70, EvidenceRecall: .80,
		GoldCoverage: .75, Faithfulness: .80, AnswerCorrectness: .80,
		RefusalQuality: .80, ValidCoverage: .95, CommonValidItems: 30,
		JudgeCalibrationPassed: true,
	}
}

func TestHoldoutRecommendationRequiresAllGuards(t *testing.T) {
	base := baselineMetrics()
	candidate := base
	candidate.VariantID = "candidate"
	candidate.ContextPrecision = .74
	if got := EvaluateHoldout(base, candidate); got.Decision != DecisionRecommend {
		t.Fatalf("decision = %#v", got)
	}
	candidate.EvidenceRecall = .79
	if got := EvaluateHoldout(base, candidate); got.Decision != DecisionNoBetterStrategy {
		t.Fatalf("recall regression was recommended: %#v", got)
	}
}

func TestSelectiveInvalidCandidateCannotWin(t *testing.T) {
	base := baselineMetrics()
	candidate := base
	candidate.VariantID = "selectively-invalid"
	candidate.ContextPrecision = .99
	candidate.ValidCoverage = .70
	candidate.CommonValidItems = 15
	decision := EvaluateHoldout(base, candidate)
	if decision.Decision != DecisionNoBetterStrategy || len(decision.ReasonCodes) < 2 {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestTuningSelectsOneProvisionalAndLimitsCandidates(t *testing.T) {
	base := baselineMetrics()
	a, b := base, base
	a.VariantID, a.ContextPrecision = "a", .73
	b.VariantID, b.ContextPrecision = "b", .75
	selected, err := SelectProvisionalCandidate(base, []VariantMetrics{a, b})
	if err != nil || selected.VariantID != "b" {
		t.Fatalf("selected = %#v, %v", selected, err)
	}
	if _, err := SelectProvisionalCandidate(base, []VariantMetrics{a, b, a}); err == nil {
		t.Fatal("accepted more than two candidates")
	}
}

func TestUncalibratedJudgeMetricsDoNotInfluenceRecommendation(t *testing.T) {
	base := baselineMetrics()
	base.JudgeCalibrationPassed = false
	candidate := base
	candidate.VariantID = "candidate"
	candidate.ContextPrecision = .74
	candidate.Faithfulness = 0
	if got := EvaluateHoldout(base, candidate); got.Decision != DecisionRecommend {
		t.Fatalf("uncalibrated judge influenced decision: %#v", got)
	}
}
