package rageval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const QualityGateVersion = "testset-quality-v1"

type GenerationProfile struct {
	DocumentSnapshots          any            `json:"document_snapshots"`
	SourceNormalizationVersion string         `json:"source_normalization_version"`
	OffsetUnit                 string         `json:"offset_unit"`
	SplitAlgorithm             string         `json:"split_algorithm"`
	SplitSeed                  int64          `json:"split_seed"`
	GeneratorModel             map[string]any `json:"generator_model"`
	GeneratorPromptVersion     string         `json:"generator_prompt_version"`
	GeneratorPromptHash        string         `json:"generator_prompt_hash"`
}

func (p GenerationProfile) Validate() error {
	if p.DocumentSnapshots == nil || p.SourceNormalizationVersion == "" || p.OffsetUnit == "" ||
		p.SplitAlgorithm == "" || len(p.GeneratorModel) == 0 ||
		p.GeneratorPromptVersion == "" || p.GeneratorPromptHash == "" {
		return fmt.Errorf("generation profile is incomplete")
	}
	if p.OffsetUnit != OffsetUnitUnicodeCodepoint {
		return fmt.Errorf("unsupported offset unit %q", p.OffsetUnit)
	}
	return nil
}

func (p GenerationProfile) Hash() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// CheckGenerationProfileLock allows the first accepted generation to lock an
// empty draft. Every later generation must supply the byte-equivalent profile.
func CheckGenerationProfileLock(locked bool, currentHash string, proposed GenerationProfile) (string, error) {
	proposedHash, err := proposed.Hash()
	if err != nil {
		return "", err
	}
	if !locked {
		return proposedHash, nil
	}
	if currentHash == "" || currentHash != proposedHash {
		return "", fmt.Errorf("generation profile conflicts with locked draft")
	}
	return currentHash, nil
}

type QualityCase struct {
	Question           string
	Answerability      Answerability
	QuestionType       string
	ReferenceAnswer    string
	ReferenceKeyPoints []string
	ExpectedRefusal    string
	UnanswerableReason string
	CheckedScope       map[string]any
	Evidence           []GoldEvidence
}

type QualityReport struct {
	Version     string   `json:"version"`
	Passed      bool     `json:"passed"`
	ReasonCodes []string `json:"reason_codes"`
}

func EvaluateCaseQuality(candidate QualityCase, existingQuestions []string) QualityReport {
	reasons := make(map[string]struct{})
	add := func(reason string) { reasons[reason] = struct{}{} }
	question := normalizeComparable(candidate.Question)
	if question == "" || len([]rune(question)) < 4 {
		add("SCHEMA_INCOMPLETE")
	}
	for _, existing := range existingQuestions {
		if duplicateSimilarity(question, normalizeComparable(existing)) >= .90 {
			add("QUESTION_DUPLICATE")
			break
		}
	}
	switch candidate.Answerability {
	case Answerable:
		if candidate.ReferenceAnswer == "" || candidate.ExpectedRefusal != "" || candidate.UnanswerableReason != "" {
			add("ANSWERABILITY_CONSTRAINT_VIOLATION")
		}
		expectedEvidence := 0
		switch candidate.QuestionType {
		case "single_evidence":
			expectedEvidence = 1
		case "multi_evidence":
			expectedEvidence = 2
		default:
			add("QUESTION_TYPE_INVALID")
		}
		if len(candidate.Evidence) != expectedEvidence || !validGold(candidate.Evidence) {
			add("EVIDENCE_INVALID")
		}
		if answerLeaked(candidate.Question, candidate.ReferenceAnswer) {
			add("ANSWER_LEAKAGE")
		}
		if len(candidate.ReferenceKeyPoints) == 0 || !keyPointsSupported(candidate.ReferenceKeyPoints, candidate.Evidence) {
			add("CLAIM_EVIDENCE_UNSUPPORTED")
		}
	case Unanswerable:
		if candidate.QuestionType != "unanswerable" || candidate.ReferenceAnswer != "" || len(candidate.Evidence) != 0 ||
			candidate.ExpectedRefusal == "" || candidate.UnanswerableReason == "" || len(candidate.CheckedScope) == 0 {
			add("UNANSWERABLE_CONSTRAINT_VIOLATION")
		}
	default:
		add("ANSWERABILITY_INVALID")
	}
	if likelyAmbiguous(candidate.Question) {
		add("QUESTION_AMBIGUOUS")
	}
	report := QualityReport{Version: QualityGateVersion, Passed: len(reasons) == 0}
	for reason := range reasons {
		report.ReasonCodes = append(report.ReasonCodes, reason)
	}
	sort.Strings(report.ReasonCodes)
	return report
}

func normalizeComparable(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func duplicateSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	aSet, bSet := runeNGrams(a, 2), runeNGrams(b, 2)
	intersection := 0
	for gram := range aSet {
		if _, ok := bSet[gram]; ok {
			intersection++
		}
	}
	union := len(aSet) + len(bSet) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func runeNGrams(value string, n int) map[string]struct{} {
	runes := []rune(value)
	result := make(map[string]struct{})
	if len(runes) < n {
		result[value] = struct{}{}
		return result
	}
	for i := 0; i+n <= len(runes); i++ {
		result[string(runes[i:i+n])] = struct{}{}
	}
	return result
}

func answerLeaked(question, answer string) bool {
	answer = normalizeComparable(answer)
	return len([]rune(answer)) >= 4 && strings.Contains(normalizeComparable(question), answer)
}

func keyPointsSupported(points []string, evidence []GoldEvidence) bool {
	var combined strings.Builder
	for _, item := range evidence {
		combined.WriteString(normalizeComparable(item.Text))
		combined.WriteByte(' ')
	}
	haystack := combined.String()
	for _, point := range points {
		point = normalizeComparable(point)
		if point == "" || !strings.Contains(haystack, point) {
			return false
		}
	}
	return true
}

func likelyAmbiguous(question string) bool {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return true
	}
	// Standalone demonstratives have no stable document referent.
	for _, token := range []string{"这个是什么", "那个是什么", "它是什么", "this?", "that?"} {
		if strings.EqualFold(trimmed, token) {
			return true
		}
	}
	contentRunes := 0
	for _, current := range trimmed {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			contentRunes++
		}
	}
	return contentRunes < 4
}
