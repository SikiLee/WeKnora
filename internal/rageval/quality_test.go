package rageval

import "testing"

func qualityEvidence(text string) GoldEvidence {
	return GoldEvidence{
		SourceDocumentID: "doc", SourceContentHash: "source-hash",
		NormalizationVersion: SourceNormalizationVersion, OffsetUnit: OffsetUnitUnicodeCodepoint,
		StartAt: 0, EndAt: len([]rune(text)), Text: text, TextHash: SHA256(text),
	}
}

func TestGenerationProfileLock(t *testing.T) {
	profile := GenerationProfile{
		DocumentSnapshots: []string{"doc"}, SourceNormalizationVersion: SourceNormalizationVersion,
		OffsetUnit: OffsetUnitUnicodeCodepoint, SplitAlgorithm: "stable-hash", SplitSeed: 42,
		GeneratorModel:         map[string]any{"id": "model", "temperature": 0},
		GeneratorPromptVersion: "v1", GeneratorPromptHash: "hash",
	}
	hash, err := CheckGenerationProfileLock(false, "", profile)
	if err != nil || hash == "" {
		t.Fatalf("first lock = %q, %v", hash, err)
	}
	if _, err := CheckGenerationProfileLock(true, hash, profile); err != nil {
		t.Fatalf("same profile rejected: %v", err)
	}
	profile.SplitSeed++
	if _, err := CheckGenerationProfileLock(true, hash, profile); err == nil {
		t.Fatal("profile drift accepted")
	}
}

func TestAnswerableQualityGate(t *testing.T) {
	report := EvaluateCaseQuality(QualityCase{
		Question: "WeKnora 如何保存评测证据？", Answerability: Answerable,
		QuestionType: "single_evidence", ReferenceAnswer: "绑定稳定原文位置",
		ReferenceKeyPoints: []string{"稳定原文位置"}, Evidence: []GoldEvidence{qualityEvidence("证据必须绑定稳定原文位置，不能只保存 chunk ID。")},
	}, nil)
	if !report.Passed {
		t.Fatalf("quality = %#v", report)
	}
}

func TestUnanswerableRequiresCheckedScopeAndNoEvidence(t *testing.T) {
	report := EvaluateCaseQuality(QualityCase{
		Question: "文档是否声明了火星部署方案？", Answerability: Unanswerable,
		QuestionType: "unanswerable", ExpectedRefusal: "文档未提供该信息",
		UnanswerableReason: "scope searched", CheckedScope: map[string]any{"documents": []string{"doc"}},
	}, nil)
	if !report.Passed {
		t.Fatalf("quality = %#v", report)
	}
	report = EvaluateCaseQuality(QualityCase{
		Question: "文档是否声明了火星部署方案？", Answerability: Unanswerable,
		QuestionType: "unanswerable", ExpectedRefusal: "文档未提供该信息", UnanswerableReason: "scope searched",
	}, nil)
	if report.Passed {
		t.Fatal("unanswerable case without checked scope passed")
	}
}

func TestDuplicateAndAnswerLeakageFailQuality(t *testing.T) {
	report := EvaluateCaseQuality(QualityCase{
		Question: "答案是稳定原文位置吗？", Answerability: Answerable,
		QuestionType: "single_evidence", ReferenceAnswer: "稳定原文位置",
		ReferenceKeyPoints: []string{"稳定原文位置"}, Evidence: []GoldEvidence{qualityEvidence("稳定原文位置")},
	}, []string{"答案是稳定原文位置吗？"})
	if report.Passed || len(report.ReasonCodes) < 2 {
		t.Fatalf("quality = %#v", report)
	}
}
