package rageval

import (
	"strings"
	"testing"
)

func perfectAnnotations() []PointwiseAnnotation {
	annotations := make([]PointwiseAnnotation, 60)
	labels := []string{"unsupported", "unclear", "supported"}
	for i := range annotations {
		label := labels[i%len(labels)]
		annotations[i] = PointwiseAnnotation{
			CaseID:        string(rune('a' + i%26)),
			ExpectedLabel: label,
			ActualLabel:   label,
			ParseOK:       true,
		}
		if i < 20 {
			annotations[i].RepeatLabels = []string{label, label, label}
		}
	}
	return annotations
}

func TestPointwiseCalibrationPassesExecutableGate(t *testing.T) {
	report, err := EvaluatePointwiseCalibration(perfectAnnotations(), []string{"unsupported", "unclear", "supported"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.ExactAgreement != 1 || report.RepeatConsistency != 1 || report.WeightedKappa != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestPointwiseCalibrationRejectsParserFailureAtOnePercent(t *testing.T) {
	annotations := perfectAnnotations()
	annotations[0].ParseOK = false
	report, err := EvaluatePointwiseCalibration(annotations, []string{"unsupported", "unclear", "supported"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.ParserFailureRate < CalibrationMaxParserFailureRate {
		t.Fatalf("expected parser gate failure: %+v", report)
	}
}

func TestStrictPointwiseJudgeParser(t *testing.T) {
	raw := []byte(`{"status":"valid","score":0.75,"reason":"three of four claims supported","claims":[{"claim":"x","verdict":"supported","evidence_ids":[1],"reason":"context 1"}]}`)
	output, err := ParsePointwiseJudgeOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if output.Score == nil || *output.Score != 0.75 {
		t.Fatalf("unexpected output: %+v", output)
	}
	if _, err := ParsePointwiseJudgeOutput([]byte(`{"status":"invalid","score":0,"reason_code":"PARSE_FAILED","reason":"bad"}`)); err == nil {
		t.Fatal("invalid status with numeric score must be rejected")
	}
	longReason := strings.Repeat("理", 1001)
	if _, err := ParsePointwiseJudgeOutput([]byte(`{"status":"abstain","score":null,"reason_code":"NO_CLAIMS","reason":"` + longReason + `"}`)); err == nil {
		t.Fatal("unbounded judge reason must be rejected")
	}
}
