package rageval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

const (
	CalibrationMinCases             = 60
	CalibrationMinRepeatCases       = 20
	CalibrationMinMacroF1           = 0.80
	CalibrationMinExactAgreement    = 0.85
	CalibrationMinWeightedKappa     = 0.70
	CalibrationMinRepeatConsistency = 0.90
	CalibrationMaxParserFailureRate = 0.01
)

type PointwiseAnnotation struct {
	CaseID        string   `json:"case_id"`
	ExpectedLabel string   `json:"expected_label"`
	ActualLabel   string   `json:"actual_label"`
	ParseOK       bool     `json:"parse_ok"`
	RepeatLabels  []string `json:"repeat_labels,omitempty"`
}

type CalibrationReport struct {
	EligibleCases      int      `json:"eligible_cases"`
	ParserFailures     int      `json:"parser_failures"`
	MacroF1            float64  `json:"macro_f1"`
	ExactAgreement     float64  `json:"exact_agreement"`
	WeightedKappa      float64  `json:"weighted_kappa"`
	RepeatCases        int      `json:"repeat_cases"`
	RepeatConsistency  float64  `json:"repeat_consistency"`
	ParserFailureRate  float64  `json:"parser_failure_rate"`
	Passed             bool     `json:"passed"`
	FailureReasonCodes []string `json:"failure_reason_codes,omitempty"`
}

// EvaluatePointwiseCalibration applies the executable MVP gate. labelOrder is
// the ordered verdict scale for weighted kappa; no pairwise presentation or
// swap test is part of this pointwise contract.
func EvaluatePointwiseCalibration(annotations []PointwiseAnnotation, labelOrder []string) (CalibrationReport, error) {
	if len(labelOrder) < 2 {
		return CalibrationReport{}, errors.New("at least two ordered labels are required")
	}
	labelIndex := make(map[string]int, len(labelOrder))
	for i, label := range labelOrder {
		if strings.TrimSpace(label) == "" {
			return CalibrationReport{}, errors.New("empty calibration label")
		}
		if _, exists := labelIndex[label]; exists {
			return CalibrationReport{}, fmt.Errorf("duplicate calibration label %q", label)
		}
		labelIndex[label] = i
	}

	report := CalibrationReport{EligibleCases: len(annotations)}
	expected := make([]int, 0, len(annotations))
	actual := make([]int, 0, len(annotations))
	repeatConsistent := 0
	for _, annotation := range annotations {
		if !annotation.ParseOK {
			report.ParserFailures++
			continue
		}
		e, ok := labelIndex[annotation.ExpectedLabel]
		if !ok {
			return CalibrationReport{}, fmt.Errorf("unknown expected label %q", annotation.ExpectedLabel)
		}
		a, ok := labelIndex[annotation.ActualLabel]
		if !ok {
			return CalibrationReport{}, fmt.Errorf("unknown actual label %q", annotation.ActualLabel)
		}
		expected = append(expected, e)
		actual = append(actual, a)
		if len(annotation.RepeatLabels) == 3 {
			report.RepeatCases++
			if annotation.RepeatLabels[0] == annotation.RepeatLabels[1] &&
				annotation.RepeatLabels[1] == annotation.RepeatLabels[2] {
				repeatConsistent++
			}
		}
	}

	if report.EligibleCases > 0 {
		report.ParserFailureRate = float64(report.ParserFailures) / float64(report.EligibleCases)
	}
	report.MacroF1 = macroF1(expected, actual, len(labelOrder))
	report.ExactAgreement = exactAgreement(expected, actual)
	report.WeightedKappa = quadraticWeightedKappa(expected, actual, len(labelOrder))
	if report.RepeatCases > 0 {
		report.RepeatConsistency = float64(repeatConsistent) / float64(report.RepeatCases)
	}

	if report.EligibleCases < CalibrationMinCases {
		report.FailureReasonCodes = append(report.FailureReasonCodes, "CALIBRATION_CASES_INSUFFICIENT")
	}
	if report.MacroF1 < CalibrationMinMacroF1 {
		report.FailureReasonCodes = append(report.FailureReasonCodes, "CALIBRATION_MACRO_F1_LOW")
	}
	if report.ExactAgreement < CalibrationMinExactAgreement {
		report.FailureReasonCodes = append(report.FailureReasonCodes, "CALIBRATION_EXACT_AGREEMENT_LOW")
	}
	if report.WeightedKappa < CalibrationMinWeightedKappa {
		report.FailureReasonCodes = append(report.FailureReasonCodes, "CALIBRATION_WEIGHTED_KAPPA_LOW")
	}
	if report.RepeatCases < CalibrationMinRepeatCases || report.RepeatConsistency < CalibrationMinRepeatConsistency {
		report.FailureReasonCodes = append(report.FailureReasonCodes, "CALIBRATION_REPEAT_STABILITY_LOW")
	}
	if report.ParserFailureRate >= CalibrationMaxParserFailureRate {
		report.FailureReasonCodes = append(report.FailureReasonCodes, "CALIBRATION_PARSER_FAILURE_HIGH")
	}
	report.Passed = len(report.FailureReasonCodes) == 0
	return report, nil
}

type ClaimVerdict struct {
	Claim       string `json:"claim"`
	Verdict     string `json:"verdict"`
	EvidenceIDs []int  `json:"evidence_ids,omitempty"`
	Reason      string `json:"reason"`
}

// PointwiseJudgeOutput is deliberately compact. It persists verdicts and
// evidence references, never unbounded model chain-of-thought.
type PointwiseJudgeOutput struct {
	Status     MetricStatus   `json:"status"`
	Score      *float64       `json:"score"`
	ReasonCode string         `json:"reason_code,omitempty"`
	Reason     string         `json:"reason"`
	Claims     []ClaimVerdict `json:"claims,omitempty"`
}

func ParsePointwiseJudgeOutput(raw []byte) (PointwiseJudgeOutput, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var output PointwiseJudgeOutput
	if err := decoder.Decode(&output); err != nil {
		return PointwiseJudgeOutput{}, fmt.Errorf("parse judge output: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return PointwiseJudgeOutput{}, err
	}
	switch output.Status {
	case MetricValid:
		if output.Score == nil || *output.Score < 0 || *output.Score > 1 {
			return PointwiseJudgeOutput{}, errors.New("valid judge output requires score in [0,1]")
		}
	case MetricInvalid, MetricAbstain:
		if output.Score != nil || output.ReasonCode == "" {
			return PointwiseJudgeOutput{}, errors.New("non-valid judge output requires null score and reason_code")
		}
	default:
		return PointwiseJudgeOutput{}, fmt.Errorf("unknown judge status %q", output.Status)
	}
	if len([]rune(output.Reason)) > 1000 {
		return PointwiseJudgeOutput{}, errors.New("judge reason exceeds 1000 runes")
	}
	for _, claim := range output.Claims {
		if strings.TrimSpace(claim.Claim) == "" || strings.TrimSpace(claim.Verdict) == "" {
			return PointwiseJudgeOutput{}, errors.New("claim and verdict are required")
		}
		if len([]rune(claim.Reason)) > 500 {
			return PointwiseJudgeOutput{}, errors.New("claim reason exceeds 500 runes")
		}
	}
	return output, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values in judge output")
	}
	return err
}

func macroF1(expected, actual []int, classCount int) float64 {
	if len(expected) == 0 || len(expected) != len(actual) {
		return 0
	}
	total := 0.0
	for class := 0; class < classCount; class++ {
		tp, fp, fn := 0, 0, 0
		for i := range expected {
			switch {
			case expected[i] == class && actual[i] == class:
				tp++
			case expected[i] != class && actual[i] == class:
				fp++
			case expected[i] == class && actual[i] != class:
				fn++
			}
		}
		denominator := 2*tp + fp + fn
		if denominator > 0 {
			total += float64(2*tp) / float64(denominator)
		}
	}
	return total / float64(classCount)
}

func exactAgreement(expected, actual []int) float64 {
	if len(expected) == 0 || len(expected) != len(actual) {
		return 0
	}
	matches := 0
	for i := range expected {
		if expected[i] == actual[i] {
			matches++
		}
	}
	return float64(matches) / float64(len(expected))
}

func quadraticWeightedKappa(expected, actual []int, classCount int) float64 {
	if len(expected) == 0 || len(expected) != len(actual) || classCount < 2 {
		return 0
	}
	observed := make([][]float64, classCount)
	for i := range observed {
		observed[i] = make([]float64, classCount)
	}
	histExpected := make([]float64, classCount)
	histActual := make([]float64, classCount)
	for i := range expected {
		observed[expected[i]][actual[i]]++
		histExpected[expected[i]]++
		histActual[actual[i]]++
	}
	n := float64(len(expected))
	weightedObserved, weightedExpected := 0.0, 0.0
	denominator := float64((classCount - 1) * (classCount - 1))
	for i := 0; i < classCount; i++ {
		for j := 0; j < classCount; j++ {
			weight := math.Pow(float64(i-j), 2) / denominator
			weightedObserved += weight * observed[i][j] / n
			weightedExpected += weight * (histExpected[i] * histActual[j] / (n * n))
		}
	}
	if weightedExpected == 0 {
		if weightedObserved == 0 {
			return 1
		}
		return 0
	}
	return 1 - weightedObserved/weightedExpected
}
