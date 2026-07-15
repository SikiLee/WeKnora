// Package rageval contains the pure, deterministic domain rules used by the
// RAG evaluation services. It deliberately has no database, queue, model, or
// HTTP dependency so the highest-risk invariants can be tested in isolation.
package rageval

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	SourceNormalizationVersion = "weknora-nfc-lf-v1"
	OffsetUnitUnicodeCodepoint = "unicode_codepoint"
)

var (
	ErrSourceHashMismatch = errors.New("source content hash mismatch")
	ErrMappingMismatch    = errors.New("source/execution identity mapping mismatch")
	ErrInvalidOffset      = errors.New("invalid source offset")
)

// NormalizedSource is the immutable text identity used by TestsetVersion,
// GoldEvidence, EvalRun snapshots, and shadow-document mappings.
type NormalizedSource struct {
	Text                 string `json:"text"`
	ContentHash          string `json:"content_hash"`
	NormalizationVersion string `json:"source_normalization_version"`
	OffsetUnit           string `json:"offset_unit"`
	RuneLength           int    `json:"rune_length"`
}

// NormalizeSource converts all line endings to LF and canonicalizes Unicode
// to NFC. Offsets are then counted in Unicode code points (Go runes), matching
// the existing chunker Start/End invariant.
func NormalizeSource(raw string) NormalizedSource {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = norm.NFC.String(text)
	return NormalizedSource{
		Text:                 text,
		ContentHash:          SHA256(text),
		NormalizationVersion: SourceNormalizationVersion,
		OffsetUnit:           OffsetUnitUnicodeCodepoint,
		RuneLength:           len([]rune(text)),
	}
}

func SHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// GoldEvidence is a source-bound closed-open interval. Execution chunk IDs
// are intentionally absent: they are unstable across re-parsing.
type GoldEvidence struct {
	SourceDocumentID     string `json:"source_document_id"`
	SourceContentHash    string `json:"source_content_hash"`
	NormalizationVersion string `json:"source_normalization_version"`
	OffsetUnit           string `json:"offset_unit"`
	StartAt              int    `json:"start_at"`
	EndAt                int    `json:"end_at"`
	Text                 string `json:"text_snapshot"`
	TextHash             string `json:"content_hash"`
}

func NewGoldEvidence(sourceDocumentID string, source NormalizedSource, startAt, endAt int) (GoldEvidence, error) {
	text, err := SliceRunes(source.Text, startAt, endAt)
	if err != nil {
		return GoldEvidence{}, err
	}
	if sourceDocumentID == "" {
		return GoldEvidence{}, errors.New("source document id is required")
	}
	return GoldEvidence{
		SourceDocumentID:     sourceDocumentID,
		SourceContentHash:    source.ContentHash,
		NormalizationVersion: source.NormalizationVersion,
		OffsetUnit:           source.OffsetUnit,
		StartAt:              startAt,
		EndAt:                endAt,
		Text:                 text,
		TextHash:             SHA256(text),
	}, nil
}

// ValidateRoundTrip proves that a persisted evidence interval still resolves
// to the same normalized document and text. A changed source is stale, never a
// fuzzy match candidate.
func (g GoldEvidence) ValidateRoundTrip(source NormalizedSource) error {
	if g.SourceContentHash != source.ContentHash {
		return ErrSourceHashMismatch
	}
	if g.NormalizationVersion != source.NormalizationVersion || g.OffsetUnit != source.OffsetUnit {
		return ErrMappingMismatch
	}
	text, err := SliceRunes(source.Text, g.StartAt, g.EndAt)
	if err != nil {
		return err
	}
	if text != g.Text || SHA256(text) != g.TextHash {
		return fmt.Errorf("evidence round-trip mismatch")
	}
	return nil
}

func SliceRunes(text string, startAt, endAt int) (string, error) {
	runes := []rune(text)
	if startAt < 0 || endAt <= startAt || endAt > len(runes) {
		return "", ErrInvalidOffset
	}
	return string(runes[startAt:endAt]), nil
}

// SourceIdentityMapping is frozen when a source document is copied into a
// temporary evaluation KB. DirectOffset is true only when the execution copy
// was built from the exact same normalized source, so offsets can be mapped
// without an opaque transform.
type SourceIdentityMapping struct {
	SourceDocumentID     string `json:"source_document_id"`
	ExecutionDocumentID  string `json:"execution_document_id"`
	SourceContentHash    string `json:"source_content_hash"`
	ExecutionContentHash string `json:"execution_content_hash"`
	NormalizationVersion string `json:"source_normalization_version"`
	OffsetUnit           string `json:"offset_unit"`
	DirectOffset         bool   `json:"direct_offset"`
}

func NewDirectMapping(sourceDocumentID, executionDocumentID string, source, execution NormalizedSource) (SourceIdentityMapping, error) {
	if sourceDocumentID == "" || executionDocumentID == "" {
		return SourceIdentityMapping{}, errors.New("source and execution document ids are required")
	}
	if source.ContentHash != execution.ContentHash ||
		source.NormalizationVersion != execution.NormalizationVersion ||
		source.OffsetUnit != execution.OffsetUnit {
		return SourceIdentityMapping{}, ErrMappingMismatch
	}
	return SourceIdentityMapping{
		SourceDocumentID:     sourceDocumentID,
		ExecutionDocumentID:  executionDocumentID,
		SourceContentHash:    source.ContentHash,
		ExecutionContentHash: execution.ContentHash,
		NormalizationVersion: source.NormalizationVersion,
		OffsetUnit:           source.OffsetUnit,
		DirectOffset:         true,
	}, nil
}

// MapExecutionInterval maps a retrieved execution interval back to its stable
// source identity. Non-direct mappings are rejected rather than guessed.
func (m SourceIdentityMapping) MapExecutionInterval(executionDocumentID string, startAt, endAt int) (SourceInterval, error) {
	if !m.DirectOffset || executionDocumentID != m.ExecutionDocumentID {
		return SourceInterval{}, ErrMappingMismatch
	}
	if startAt < 0 || endAt <= startAt {
		return SourceInterval{}, ErrInvalidOffset
	}
	return SourceInterval{
		SourceDocumentID:     m.SourceDocumentID,
		SourceContentHash:    m.SourceContentHash,
		NormalizationVersion: m.NormalizationVersion,
		OffsetUnit:           m.OffsetUnit,
		StartAt:              startAt,
		EndAt:                endAt,
	}, nil
}

type SourceInterval struct {
	SourceDocumentID     string `json:"source_document_id"`
	SourceContentHash    string `json:"source_content_hash"`
	NormalizationVersion string `json:"source_normalization_version"`
	OffsetUnit           string `json:"offset_unit"`
	StartAt              int    `json:"start_at"`
	EndAt                int    `json:"end_at"`
}
