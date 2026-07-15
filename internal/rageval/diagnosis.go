package rageval

import "fmt"

type DiagnosisClassification string

const (
	DiagnosisChunkingLikely    DiagnosisClassification = "chunking_likely"
	DiagnosisNonChunkingLikely DiagnosisClassification = "non_chunking_likely"
	DiagnosisUnknown           DiagnosisClassification = "unknown"
)

type DiagnosisStatus string

const (
	DiagnosisPending   DiagnosisStatus = "pending"
	DiagnosisRunning   DiagnosisStatus = "running"
	DiagnosisCompleted DiagnosisStatus = "completed"
	DiagnosisFailed    DiagnosisStatus = "failed"
)

const DiagnosisVersion = "failure-diagnosis-v1"

const (
	ReasonDocumentNotReady          = "DOCUMENT_NOT_READY"
	ReasonRetrievalExecutionFailed  = "RETRIEVAL_EXECUTION_FAILED"
	ReasonGenerationExecutionFailed = "GENERATION_EXECUTION_FAILED"
	ReasonTopKRedundancyHigh        = "TOPK_REDUNDANCY_HIGH"
	ReasonChunkBoundarySplit        = "CHUNK_BOUNDARY_SPLIT"
	ReasonChunkTooSmallIncomplete   = "CHUNK_TOO_SMALL_INCOMPLETE"
	ReasonChunkTooLargeNoisy        = "CHUNK_TOO_LARGE_NOISY"
	ReasonHeadingBodyDetached       = "HEADING_BODY_DETACHED"
	ReasonInsufficientSignals       = "INSUFFICIENT_SIGNALS"
)

type DiagnosisSignals struct {
	DocumentReady        bool    `json:"document_ready"`
	RetrievalSucceeded   bool    `json:"retrieval_succeeded"`
	GenerationSucceeded  bool    `json:"generation_succeeded"`
	ValidCases           int     `json:"valid_cases"`
	TargetK              int     `json:"target_k"`
	ContextPrecision     float64 `json:"context_precision"`
	EvidenceRecall       float64 `json:"evidence_recall"`
	GoldCoverage         float64 `json:"gold_coverage"`
	ChunkRedundancy      float64 `json:"chunk_redundancy"`
	BoundarySplitRate    float64 `json:"boundary_split_rate"`
	HeadingDetachedRate  float64 `json:"heading_detached_rate"`
	AverageChunkRunes    float64 `json:"average_chunk_runes"`
	AverageEvidenceRunes float64 `json:"average_evidence_runes"`
}

type FailureDiagnosis struct {
	Version        string                  `json:"version"`
	Status         DiagnosisStatus         `json:"status"`
	Classification DiagnosisClassification `json:"classification"`
	ReasonCode     string                  `json:"reason_code"`
	Evidence       DiagnosisSignals        `json:"evidence_signals"`
	TargetK        int                     `json:"target_k"`
	ValidCases     int                     `json:"valid_cases"`
}

func DiagnoseFailure(signals DiagnosisSignals) FailureDiagnosis {
	result := FailureDiagnosis{
		Version: DiagnosisVersion, Status: DiagnosisCompleted,
		Evidence: signals, TargetK: signals.TargetK, ValidCases: signals.ValidCases,
	}
	switch {
	case !signals.DocumentReady:
		result.Classification, result.ReasonCode = DiagnosisNonChunkingLikely, ReasonDocumentNotReady
	case !signals.RetrievalSucceeded:
		result.Classification, result.ReasonCode = DiagnosisNonChunkingLikely, ReasonRetrievalExecutionFailed
	case !signals.GenerationSucceeded:
		result.Classification, result.ReasonCode = DiagnosisNonChunkingLikely, ReasonGenerationExecutionFailed
	case signals.ValidCases < 10 || signals.TargetK < 1:
		result.Classification, result.ReasonCode = DiagnosisUnknown, ReasonInsufficientSignals
	case signals.ChunkRedundancy >= .30:
		result.Classification, result.ReasonCode = DiagnosisChunkingLikely, ReasonTopKRedundancyHigh
	case signals.HeadingDetachedRate >= .20:
		result.Classification, result.ReasonCode = DiagnosisChunkingLikely, ReasonHeadingBodyDetached
	case signals.BoundarySplitRate >= .20:
		result.Classification, result.ReasonCode = DiagnosisChunkingLikely, ReasonChunkBoundarySplit
	case signals.GoldCoverage < .75 && signals.AverageEvidenceRunes > signals.AverageChunkRunes:
		result.Classification, result.ReasonCode = DiagnosisChunkingLikely, ReasonChunkTooSmallIncomplete
	case signals.ContextPrecision < .70 && signals.EvidenceRecall >= .90:
		result.Classification, result.ReasonCode = DiagnosisChunkingLikely, ReasonChunkTooLargeNoisy
	default:
		result.Classification, result.ReasonCode = DiagnosisUnknown, ReasonInsufficientSignals
	}
	return result
}

type CandidateDimension string

const (
	CandidateDecreaseOverlap CandidateDimension = "decrease_overlap"
	CandidateIncreaseOverlap CandidateDimension = "increase_overlap"
	CandidateDecreaseSize    CandidateDimension = "decrease_chunk_size"
	CandidateIncreaseSize    CandidateDimension = "increase_chunk_size"
	CandidateHeadingStrategy CandidateDimension = "heading_strategy"
)

// CandidateDimensions restricts the experiment grid to the dimensions
// supported by diagnosis evidence. MVP callers may select at most two.
func CandidateDimensions(d FailureDiagnosis) ([]CandidateDimension, error) {
	if d.Status != DiagnosisCompleted || d.Classification != DiagnosisChunkingLikely {
		return nil, fmt.Errorf("completed chunking_likely diagnosis required")
	}
	switch d.ReasonCode {
	case ReasonTopKRedundancyHigh:
		return []CandidateDimension{CandidateDecreaseOverlap, CandidateDecreaseSize}, nil
	case ReasonHeadingBodyDetached:
		return []CandidateDimension{CandidateHeadingStrategy, CandidateIncreaseSize}, nil
	case ReasonChunkBoundarySplit:
		return []CandidateDimension{CandidateIncreaseOverlap, CandidateIncreaseSize}, nil
	case ReasonChunkTooSmallIncomplete:
		return []CandidateDimension{CandidateIncreaseSize, CandidateIncreaseOverlap}, nil
	case ReasonChunkTooLargeNoisy:
		return []CandidateDimension{CandidateDecreaseSize, CandidateHeadingStrategy}, nil
	default:
		return nil, fmt.Errorf("unsupported chunking diagnosis reason %q", d.ReasonCode)
	}
}
