package rageval

import "testing"

func readySignals() DiagnosisSignals {
	return DiagnosisSignals{
		DocumentReady: true, RetrievalSucceeded: true, GenerationSucceeded: true,
		ValidCases: 30, TargetK: 5, ContextPrecision: .8, EvidenceRecall: .9,
		GoldCoverage: .9, AverageChunkRunes: 500, AverageEvidenceRunes: 200,
	}
}

func TestBlockedPrerequisiteCannotBeChunkingLikely(t *testing.T) {
	s := readySignals()
	s.DocumentReady = false
	d := DiagnoseFailure(s)
	if d.Classification != DiagnosisNonChunkingLikely || d.ReasonCode != ReasonDocumentNotReady {
		t.Fatalf("diagnosis = %#v", d)
	}
	if _, err := CandidateDimensions(d); err == nil {
		t.Fatal("non-chunking diagnosis created candidates")
	}
}

func TestDiagnosisRestrictsCandidateDimensions(t *testing.T) {
	s := readySignals()
	s.ChunkRedundancy = .35
	d := DiagnoseFailure(s)
	if d.Classification != DiagnosisChunkingLikely || d.ReasonCode != ReasonTopKRedundancyHigh {
		t.Fatalf("diagnosis = %#v", d)
	}
	dims, err := CandidateDimensions(d)
	if err != nil || len(dims) != 2 || dims[0] != CandidateDecreaseOverlap {
		t.Fatalf("dimensions = %#v, %v", dims, err)
	}
}

func TestInsufficientSignalsRemainUnknown(t *testing.T) {
	s := readySignals()
	s.ValidCases = 5
	d := DiagnoseFailure(s)
	if d.Classification != DiagnosisUnknown {
		t.Fatalf("diagnosis = %#v", d)
	}
}
