package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/rageval"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const (
	RAGEvaluationMaxRunCases             = 200
	RAGEvaluationMaxTenantConcurrentRuns = 2
	RAGEvaluationMaxGenerationCases      = 50
	RAGEvaluationMaxDocumentBytes        = 2 << 20
	RAGEvaluationGeneratorPromptVersion  = "qa-testset-generator-v1"
	RAGEvaluationAnswerPromptVersion     = "weknora-rag-answer-v1"
	RAGEvaluationContextBuilderVersion   = "weknora-context-builder-v1"
	RAGEvaluationNoHistoryPolicy         = "empty_history_memory_disabled"
	RAGEvaluationMetricVersion           = "source-overlap-v1"
	RAGEvaluationJudgeParserVersion      = "pointwise-json-v1"
)

var (
	ErrRAGEvaluationInvalid       = errors.New("rag evaluation invalid request")
	ErrRAGEvaluationConflict      = errors.New("rag evaluation conflict")
	ErrRAGEvaluationTooManyActive = errors.New("rag evaluation tenant concurrency limit reached")
)

type RAGEvaluationMVPService struct {
	cfg              *config.Config
	repo             interfaces.RAGEvaluationRepository
	kbService        interfaces.KnowledgeBaseService
	knowledgeService interfaces.KnowledgeService
	modelService     interfaces.ModelService
	sessionService   interfaces.SessionService
	tenantRepo       interfaces.TenantRepository
	enqueuer         interfaces.TaskEnqueuer
}

func NewRAGEvaluationMVPService(
	cfg *config.Config,
	repo interfaces.RAGEvaluationRepository,
	kbService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	modelService interfaces.ModelService,
	sessionService interfaces.SessionService,
	tenantRepo interfaces.TenantRepository,
	enqueuer interfaces.TaskEnqueuer,
) *RAGEvaluationMVPService {
	return &RAGEvaluationMVPService{
		cfg: cfg, repo: repo, kbService: kbService, knowledgeService: knowledgeService,
		modelService: modelService, sessionService: sessionService, tenantRepo: tenantRepo, enqueuer: enqueuer,
	}
}

type RAGEvaluationDocumentSnapshot struct {
	SourceDocumentID           string `json:"source_document_id"`
	SourceDocumentHash         string `json:"source_document_hash"`
	NormalizedSourceHash       string `json:"normalized_source_hash"`
	SourceNormalizationVersion string `json:"source_normalization_version"`
	OffsetUnit                 string `json:"offset_unit"`
	FileType                   string `json:"file_type"`
	PrototypeStatus            string `json:"prototype_status"`
}

type CreateRAGTestsetInput struct {
	TenantID        uint64
	KnowledgeBaseID string
	Name            string
	CreatedBy       string
}

func (s *RAGEvaluationMVPService) CreateTestset(ctx context.Context, input CreateRAGTestsetInput) (*types.EvaluationTestset, error) {
	if input.TenantID == 0 || input.KnowledgeBaseID == "" || strings.TrimSpace(input.Name) == "" || input.CreatedBy == "" {
		return nil, fmt.Errorf("%w: tenant, knowledge base, name and creator are required", ErrRAGEvaluationInvalid)
	}
	now := time.Now()
	result := &types.EvaluationTestset{
		ID: uuid.NewString(), TenantID: input.TenantID, KnowledgeBaseID: input.KnowledgeBaseID,
		Name: strings.TrimSpace(input.Name), Status: "active", CreatedBy: input.CreatedBy,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateTestset(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *RAGEvaluationMVPService) CreateVersion(ctx context.Context, tenantID uint64, kbID, testsetID, createdBy string) (*types.EvaluationTestsetVersion, error) {
	if _, err := s.repo.GetTestset(ctx, tenantID, kbID, testsetID); err != nil {
		return nil, err
	}
	versions, err := s.repo.ListVersions(ctx, tenantID, kbID, testsetID)
	if err != nil {
		return nil, err
	}
	next := 1
	if len(versions) > 0 {
		next = versions[0].VersionNumber + 1
	}
	now := time.Now()
	version := &types.EvaluationTestsetVersion{
		ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, TestsetID: testsetID,
		VersionNumber: next, Status: types.EvaluationVersionDraft,
		DocumentSnapshots: types.JSONMap{}, GeneratorModelSnapshot: types.JSONMap{},
		CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateVersion(ctx, version); err != nil {
		return nil, err
	}
	return version, nil
}

type CreateRAGEvalCaseInput struct {
	TenantID           uint64
	KnowledgeBaseID    string
	VersionID          string
	Question           string
	Answerability      rageval.Answerability
	QuestionType       string
	Difficulty         string
	ReferenceAnswer    string
	ReferenceKeyPoints []string
	ExpectedRefusal    string
	UnanswerableReason string
	CheckedScope       map[string]any
	Split              string
	Enabled            bool
	Evidence           []types.EvaluationGoldEvidence
}

func (s *RAGEvaluationMVPService) CreateCase(ctx context.Context, input CreateRAGEvalCaseInput) (*types.EvaluationCase, []*types.EvaluationGoldEvidence, error) {
	if input.Split != "tuning" && input.Split != "holdout" {
		return nil, nil, fmt.Errorf("%w: split must be tuning or holdout", ErrRAGEvaluationInvalid)
	}
	existing, _, err := s.repo.ListCases(ctx, input.TenantID, input.KnowledgeBaseID, input.VersionID, 1, 100)
	if err != nil {
		return nil, nil, err
	}
	existingQuestions := make([]string, 0, len(existing))
	for _, item := range existing {
		existingQuestions = append(existingQuestions, item.Question)
	}
	gold := make([]rageval.GoldEvidence, 0, len(input.Evidence))
	for _, item := range input.Evidence {
		gold = append(gold, rageval.GoldEvidence{
			SourceDocumentID: item.SourceDocumentID, SourceContentHash: item.NormalizedSourceHash,
			NormalizationVersion: item.SourceNormalizationVersion, OffsetUnit: item.OffsetUnit,
			StartAt: item.StartAt, EndAt: item.EndAt, Text: item.EvidenceText, TextHash: item.EvidenceHash,
		})
	}
	quality := rageval.EvaluateCaseQuality(rageval.QualityCase{
		Question: input.Question, Answerability: input.Answerability, QuestionType: input.QuestionType,
		ReferenceAnswer: input.ReferenceAnswer, ReferenceKeyPoints: input.ReferenceKeyPoints,
		ExpectedRefusal: input.ExpectedRefusal, UnanswerableReason: input.UnanswerableReason,
		CheckedScope: input.CheckedScope, Evidence: gold,
	}, existingQuestions)
	now := time.Now()
	caseID := uuid.NewString()
	qualityStatus := types.EvaluationQualityFailed
	if quality.Passed {
		qualityStatus = types.EvaluationQualityPassed
	}
	evalCase := &types.EvaluationCase{
		ID: caseID, TenantID: input.TenantID, KnowledgeBaseID: input.KnowledgeBaseID, VersionID: input.VersionID,
		Question: strings.TrimSpace(input.Question), Answerability: string(input.Answerability), QuestionType: input.QuestionType,
		Difficulty: input.Difficulty, ReferenceAnswer: input.ReferenceAnswer,
		ReferenceKeyPoints: types.JSONMap{"items": input.ReferenceKeyPoints}, ExpectedRefusal: input.ExpectedRefusal,
		UnanswerableReason: input.UnanswerableReason, CheckedScope: types.JSONMap(input.CheckedScope),
		ReviewStatus: types.EvaluationReviewPending, QualityStatus: qualityStatus,
		QualityReasonCodes: types.JSONMap{"version": quality.Version, "items": quality.ReasonCodes},
		Split:              input.Split, Enabled: input.Enabled, GenerationProvenance: types.JSONMap{"source": "manual"},
		CreatedAt: now, UpdatedAt: now,
	}
	evidence := make([]*types.EvaluationGoldEvidence, 0, len(input.Evidence))
	for index := range input.Evidence {
		item := input.Evidence[index]
		item.ID = uuid.NewString()
		item.TenantID = input.TenantID
		item.KnowledgeBaseID = input.KnowledgeBaseID
		item.CaseID = caseID
		item.Ordinal = index + 1
		item.CreatedAt = now
		evidence = append(evidence, &item)
	}
	if err := s.repo.CreateCase(ctx, evalCase, evidence); err != nil {
		return nil, nil, err
	}
	return evalCase, evidence, nil
}

func (s *RAGEvaluationMVPService) PublishVersion(ctx context.Context, tenantID uint64, kbID, versionID string) error {
	return s.repo.PublishVersion(ctx, tenantID, kbID, versionID, time.Now())
}

type CreateRAGGenerationInput struct {
	TenantID        uint64
	KnowledgeBaseID string
	TestsetID       string
	VersionID       string
	DocumentIDs     []string
	ModelID         string
	RequestedCases  int
	SplitAlgorithm  string
	SplitSeed       int64
	IdempotencyKey  string
}

func (s *RAGEvaluationMVPService) CreateGeneration(ctx context.Context, input CreateRAGGenerationInput) (*types.EvaluationTestsetGeneration, bool, error) {
	if input.RequestedCases < 1 || input.RequestedCases > RAGEvaluationMaxGenerationCases || input.IdempotencyKey == "" || len(input.DocumentIDs) == 0 {
		return nil, false, fmt.Errorf("%w: generation requires 1-%d cases, documents and Idempotency-Key", ErrRAGEvaluationInvalid, RAGEvaluationMaxGenerationCases)
	}
	version, err := s.repo.GetVersion(ctx, input.TenantID, input.KnowledgeBaseID, input.VersionID)
	if err != nil {
		return nil, false, err
	}
	if version.TestsetID != input.TestsetID || version.Status != types.EvaluationVersionDraft {
		return nil, false, fmt.Errorf("%w: generation requires the target draft version", ErrRAGEvaluationConflict)
	}
	documents, _, blocker, err := s.loadDocumentSnapshots(ctx, input.TenantID, input.KnowledgeBaseID, input.DocumentIDs, false)
	if err != nil {
		return nil, false, err
	}
	modelSnapshot := types.JSONMap{"id": input.ModelID}
	profile := rageval.GenerationProfile{
		DocumentSnapshots: documents, SourceNormalizationVersion: rageval.SourceNormalizationVersion,
		OffsetUnit: rageval.OffsetUnitUnicodeCodepoint, SplitAlgorithm: defaultString(input.SplitAlgorithm, "stable-hash-v1"),
		SplitSeed: input.SplitSeed, GeneratorModel: map[string]any{"id": input.ModelID, "temperature": 0, "seed": input.SplitSeed},
		GeneratorPromptVersion: RAGEvaluationGeneratorPromptVersion, GeneratorPromptHash: hashString(ragTestsetGeneratorSystemPrompt),
	}
	profileHash, err := profile.Hash()
	if err != nil {
		return nil, false, err
	}
	if version.ProfileLocked && version.GenerationProfileHash != profileHash {
		return nil, false, fmt.Errorf("%w: draft generation profile is locked", ErrRAGEvaluationConflict)
	}
	status := types.EvaluationRunPending
	errorCode, errorMessage := "", ""
	if blocker != "" {
		status, errorCode, errorMessage = types.EvaluationRunBlocked, blocker, blockerMessage(blocker)
	} else if _, modelErr := s.modelService.GetChatModel(ctx, input.ModelID); modelErr != nil {
		status, errorCode, errorMessage = types.EvaluationRunBlocked, "GENERATOR_MODEL_NOT_CONFIGURED", modelErr.Error()
	}
	now := time.Now()
	generation := &types.EvaluationTestsetGeneration{
		ID: uuid.NewString(), TenantID: input.TenantID, KnowledgeBaseID: input.KnowledgeBaseID,
		TestsetID: input.TestsetID, VersionID: input.VersionID, Status: status,
		IdempotencyKey: input.IdempotencyKey,
		ProfileSnapshot: types.JSONMap{
			"profile_hash": profileHash, "documents": documents, "document_ids": input.DocumentIDs,
			"source_normalization_version": rageval.SourceNormalizationVersion, "offset_unit": rageval.OffsetUnitUnicodeCodepoint,
			"split_algorithm": profile.SplitAlgorithm, "split_seed": input.SplitSeed,
			"generator_model": modelSnapshot, "generator_prompt_version": RAGEvaluationGeneratorPromptVersion,
			"generator_prompt_hash": hashString(ragTestsetGeneratorSystemPrompt),
		},
		RequestedCases: input.RequestedCases, QualityCounts: types.JSONMap{}, ErrorCode: errorCode,
		ErrorMessage: errorMessage, CreatedAt: now,
	}
	if status == types.EvaluationRunBlocked {
		generation.FinishedAt = &now
	}
	createdGeneration, created, err := s.repo.CreateGeneration(ctx, generation)
	if err != nil || !created || status == types.EvaluationRunBlocked {
		return createdGeneration, created, err
	}
	payload, _ := json.Marshal(types.RAGEvaluationGenerationPayload{TenantID: input.TenantID, KnowledgeBaseID: input.KnowledgeBaseID, GenerationID: generation.ID})
	if _, err := s.enqueuer.Enqueue(asynq.NewTask(types.TypeRAGEvaluationGenerateTestset, payload), asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(3)); err != nil {
		now := time.Now()
		generation.Status, generation.ErrorCode, generation.ErrorMessage, generation.FinishedAt = types.EvaluationRunBlocked, "QUEUE_UNAVAILABLE", err.Error(), &now
		_ = s.repo.UpdateGeneration(ctx, input.TenantID, input.KnowledgeBaseID, generation)
	}
	return generation, true, nil
}

type CreateRAGRunInput struct {
	TenantID         uint64
	KnowledgeBaseID  string
	TestsetVersionID string
	AnswerModelID    string
	RerankModelID    string
	JudgeModelID     string
	CalibrationIDs   map[string]string
	TopK             int
	SampleLimit      int
	SplitSeed        int64
	TriggerType      string
	IdempotencyKey   string
	CreatedBy        string
	ParentRunID      *string
}

func (s *RAGEvaluationMVPService) CreateRun(ctx context.Context, input CreateRAGRunInput) (*types.EvaluationRun, bool, error) {
	if input.IdempotencyKey == "" || input.AnswerModelID == "" || input.JudgeModelID == "" || input.TestsetVersionID == "" {
		return nil, false, fmt.Errorf("%w: version, answer model, judge model and Idempotency-Key are required", ErrRAGEvaluationInvalid)
	}
	if input.TopK < 1 || input.TopK > 100 {
		return nil, false, fmt.Errorf("%w: top_k must be between 1 and 100", ErrRAGEvaluationInvalid)
	}
	active, err := s.repo.CountActiveRuns(ctx, input.TenantID)
	if err != nil {
		return nil, false, err
	}
	if active >= RAGEvaluationMaxTenantConcurrentRuns {
		return nil, false, ErrRAGEvaluationTooManyActive
	}
	version, err := s.repo.GetVersion(ctx, input.TenantID, input.KnowledgeBaseID, input.TestsetVersionID)
	if err != nil {
		return nil, false, err
	}
	if version.Status != types.EvaluationVersionPublished {
		return nil, false, fmt.Errorf("%w: EvalRun requires a published TestsetVersion", ErrRAGEvaluationConflict)
	}
	cases, err := s.loadEnabledCases(ctx, input.TenantID, input.KnowledgeBaseID, input.TestsetVersionID)
	if err != nil {
		return nil, false, err
	}
	if len(cases) == 0 {
		return nil, false, fmt.Errorf("%w: published version has no enabled cases", ErrRAGEvaluationConflict)
	}
	limit := input.SampleLimit
	if limit <= 0 || limit > len(cases) {
		limit = len(cases)
	}
	if limit > RAGEvaluationMaxRunCases {
		return nil, false, fmt.Errorf("%w: run exceeds %d case hard limit", ErrRAGEvaluationInvalid, RAGEvaluationMaxRunCases)
	}
	cases = deterministicCaseSample(cases, input.SplitSeed, limit)
	kb, kbErr := s.kbService.GetKnowledgeBaseByID(ctx, input.KnowledgeBaseID)
	blocker := ""
	if kbErr != nil || kb == nil || kb.TenantID != input.TenantID {
		blocker = "KNOWLEDGE_BASE_NOT_READY"
	}
	if blocker == "" && kb.NeedsEmbeddingModel() {
		if _, modelErr := s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID); modelErr != nil {
			blocker = "EMBEDDING_MODEL_NOT_CONFIGURED"
		}
	}
	if blocker == "" {
		if _, modelErr := s.modelService.GetChatModel(ctx, input.AnswerModelID); modelErr != nil {
			blocker = "ANSWER_MODEL_NOT_CONFIGURED"
		}
	}
	if blocker == "" {
		if _, modelErr := s.modelService.GetChatModel(ctx, input.JudgeModelID); modelErr != nil {
			blocker = "JUDGE_MODEL_NOT_CONFIGURED"
		}
	}
	calibrationSnapshot := types.JSONMap{}
	for metric, calibrationID := range input.CalibrationIDs {
		calibration, calibrationErr := s.repo.GetJudgeCalibration(ctx, input.TenantID, input.KnowledgeBaseID, calibrationID)
		if calibrationErr != nil {
			calibrationSnapshot[metric] = map[string]any{"id": calibrationID, "passed": false, "status": "missing"}
			continue
		}
		calibrationSnapshot[metric] = map[string]any{"id": calibration.ID, "version": calibration.Version, "passed": calibration.Status == "passed"}
	}
	answerPrompt, contextTemplate := "", ""
	if s.cfg != nil && s.cfg.Conversation.Summary != nil {
		answerPrompt = s.cfg.Conversation.Summary.Prompt
		contextTemplate = s.cfg.Conversation.Summary.ContextTemplate
	}
	documentSnapshots := version.DocumentSnapshots
	snapshot := types.JSONMap{
		"testset_version_id": version.ID, "document_snapshots": documentSnapshots,
		"chunking_baseline": kb.ChunkingConfig, "embedding_model_id": kb.EmbeddingModelID,
		"rerank_model_id": input.RerankModelID, "answer_model_id": input.AnswerModelID, "judge_model_id": input.JudgeModelID,
		"rag_answer_prompt_version": RAGEvaluationAnswerPromptVersion, "rag_answer_prompt_hash": hashString(answerPrompt),
		"context_builder_version": RAGEvaluationContextBuilderVersion, "context_template_hash": hashString(contextTemplate),
		"no_history_policy": RAGEvaluationNoHistoryPolicy, "top_k": input.TopK,
		"query_parameters":     map[string]any{"enable_rewrite": false, "enable_memory": false},
		"judge_parser_version": RAGEvaluationJudgeParserVersion, "judge_calibrations": calibrationSnapshot,
		"metric_version": RAGEvaluationMetricVersion, "split_algorithm": version.SplitAlgorithm,
		"split_seed": version.SplitSeed, "source_normalization_version": version.SourceNormalizationVersion,
		"offset_unit": version.OffsetUnit, "app_version": "git:" + buildVersion(),
		"trigger_type": defaultString(input.TriggerType, "manual"),
	}
	now := time.Now()
	status, stage := types.EvaluationRunPending, "queued"
	if blocker != "" {
		status, stage = types.EvaluationRunBlocked, "preflight"
	}
	run := &types.EvaluationRun{
		ID: uuid.NewString(), TenantID: input.TenantID, KnowledgeBaseID: input.KnowledgeBaseID,
		TestsetVersionID: input.TestsetVersionID, ParentRunID: input.ParentRunID,
		TriggerType: defaultString(input.TriggerType, "manual"), Status: status, Stage: stage,
		IdempotencyKey: input.IdempotencyKey, Snapshot: snapshot,
		SampledCaseIDs: types.JSONMap{"items": caseIDs(cases)}, TotalItems: len(cases),
		BlockedReasonCode: blocker, ObservabilityStatus: "pending", EstimatedCalls: len(cases) * 4,
		CreatedBy: input.CreatedBy, CreatedAt: now, UpdatedAt: now,
	}
	if blocker != "" {
		run.ErrorMessage = blockerMessage(blocker)
		run.FinishedAt = &now
	}
	items, err := s.snapshotRunItems(ctx, run, cases)
	if err != nil {
		return nil, false, err
	}
	createdRun, created, err := s.repo.CreateRunWithItems(ctx, run, items)
	if err != nil || !created || status == types.EvaluationRunBlocked {
		return createdRun, created, err
	}
	payload, _ := json.Marshal(types.RAGEvaluationRunPayload{TenantID: input.TenantID, KnowledgeBaseID: input.KnowledgeBaseID, RunID: run.ID})
	if _, err := s.enqueuer.Enqueue(asynq.NewTask(types.TypeRAGEvaluationRun, payload), asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(3)); err != nil {
		now := time.Now()
		run.Status, run.Stage, run.BlockedReasonCode, run.ErrorMessage, run.FinishedAt, run.UpdatedAt = types.EvaluationRunBlocked, "enqueue", "QUEUE_UNAVAILABLE", err.Error(), &now, now
		_ = s.repo.UpdateRun(ctx, input.TenantID, input.KnowledgeBaseID, run)
	}
	return run, true, nil
}

func (s *RAGEvaluationMVPService) RetryRun(ctx context.Context, tenantID uint64, kbID, runID, scope, idempotencyKey, createdBy string) (*types.EvaluationRun, bool, error) {
	source, err := s.repo.GetRun(ctx, tenantID, kbID, runID)
	if err != nil {
		return nil, false, err
	}
	if source.Status != types.EvaluationRunFailed && source.Status != types.EvaluationRunPartial && source.Status != types.EvaluationRunCanceled && source.Status != types.EvaluationRunBlocked {
		return nil, false, fmt.Errorf("%w: retry is allowed only for blocked/failed/partial/canceled runs", ErrRAGEvaluationConflict)
	}
	items, _, err := s.repo.ListRunItems(ctx, tenantID, kbID, runID, 1, RAGEvaluationMaxRunCases)
	if err != nil {
		return nil, false, err
	}
	states := make([]rageval.RunItemState, 0, len(items))
	for _, item := range items {
		states = append(states, rageval.RunItemState{CaseID: item.CaseID, Status: rageval.RunItemStatus(item.Status)})
	}
	selected, err := rageval.RetryCaseIDs(states, rageval.RetryScope(scope))
	if err != nil || len(selected) == 0 {
		return nil, false, fmt.Errorf("%w: retry scope selected no cases", ErrRAGEvaluationInvalid)
	}
	var snap struct {
		AnswerModelID string            `json:"answer_model_id"`
		RerankModelID string            `json:"rerank_model_id"`
		JudgeModelID  string            `json:"judge_model_id"`
		TopK          int               `json:"top_k"`
		SplitSeed     int64             `json:"split_seed"`
		Calibrations  map[string]string `json:"calibration_ids"`
	}
	decodeJSONMap(source.Snapshot, &snap)
	return s.CreateRun(ctx, CreateRAGRunInput{
		TenantID: tenantID, KnowledgeBaseID: kbID, TestsetVersionID: source.TestsetVersionID,
		AnswerModelID: snap.AnswerModelID, RerankModelID: snap.RerankModelID, JudgeModelID: snap.JudgeModelID,
		TopK: snap.TopK, SampleLimit: len(selected), SplitSeed: snap.SplitSeed, TriggerType: "retry",
		IdempotencyKey: idempotencyKey, CreatedBy: createdBy, ParentRunID: &source.ID,
	})
}

func (s *RAGEvaluationMVPService) CancelRun(ctx context.Context, tenantID uint64, kbID, runID string) error {
	return s.repo.CancelRun(ctx, tenantID, kbID, runID, time.Now())
}

func (s *RAGEvaluationMVPService) loadEnabledCases(ctx context.Context, tenantID uint64, kbID, versionID string) ([]*types.EvaluationCase, error) {
	result := make([]*types.EvaluationCase, 0, RAGEvaluationMaxRunCases)
	for page := 1; page <= 2; page++ {
		items, total, err := s.repo.ListCases(ctx, tenantID, kbID, versionID, page, 100)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.Enabled {
				result = append(result, item)
			}
		}
		if int64(page*100) >= total {
			break
		}
	}
	return result, nil
}

func (s *RAGEvaluationMVPService) snapshotRunItems(ctx context.Context, run *types.EvaluationRun, cases []*types.EvaluationCase) ([]*types.EvaluationRunItem, error) {
	items := make([]*types.EvaluationRunItem, 0, len(cases))
	for _, evalCase := range cases {
		evidence, err := s.repo.ListEvidence(ctx, run.TenantID, run.KnowledgeBaseID, evalCase.ID)
		if err != nil {
			return nil, err
		}
		reference, gold := types.JSONMap{}, types.JSONMap{"items": []any{}}
		if evalCase.Answerability == string(rageval.Answerable) {
			reference = types.JSONMap{"answer": evalCase.ReferenceAnswer, "key_points": evalCase.ReferenceKeyPoints}
			gold = types.JSONMap{"items": evidence}
		} else {
			reference = types.JSONMap{}
			gold = types.JSONMap{"items": []any{}}
		}
		now := time.Now()
		items = append(items, &types.EvaluationRunItem{
			ID: uuid.NewString(), TenantID: run.TenantID, KnowledgeBaseID: run.KnowledgeBaseID,
			RunID: run.ID, CaseID: evalCase.ID, Status: string(rageval.RunItemPending),
			AnswerabilitySnapshot: evalCase.Answerability, QuestionSnapshot: evalCase.Question,
			ReferenceSnapshot: reference, GoldEvidenceSnapshot: gold,
			RetrievedContexts: types.JSONMap{"items": []any{}}, Citations: types.JSONMap{"items": []any{}},
			Latency: types.JSONMap{}, Tokens: types.JSONMap{}, Cost: types.JSONMap{},
			TraceCorrelationID: uuid.NewString(), CreatedAt: now, UpdatedAt: now,
		})
	}
	return items, nil
}

func (s *RAGEvaluationMVPService) loadDocumentSnapshots(ctx context.Context, tenantID uint64, kbID string, documentIDs []string, includeContent bool) ([]RAGEvaluationDocumentSnapshot, map[string]string, string, error) {
	documents, err := s.knowledgeService.GetKnowledgeBatch(ctx, tenantID, documentIDs)
	if err != nil {
		return nil, nil, "", err
	}
	byID := make(map[string]*types.Knowledge, len(documents))
	for _, item := range documents {
		byID[item.ID] = item
	}
	snapshots := make([]RAGEvaluationDocumentSnapshot, 0, len(documentIDs))
	contents := make(map[string]string, len(documentIDs))
	for _, id := range documentIDs {
		document := byID[id]
		if document == nil || document.KnowledgeBaseID != kbID {
			return nil, nil, "DOCUMENT_NOT_FOUND", nil
		}
		if document.ParseStatus != types.ParseStatusCompleted {
			return nil, nil, "DOCUMENT_NOT_PARSED", nil
		}
		if !evaluationPrototypeFileType(document.FileType, document.Type) {
			return nil, nil, "DOCUMENT_TYPE_PENDING_VERIFICATION", nil
		}
		content, readErr := s.readEvaluationDocument(ctx, document)
		if readErr != nil {
			return nil, nil, "DOCUMENT_SOURCE_UNAVAILABLE", nil
		}
		normalized := rageval.NormalizeSource(content)
		snapshots = append(snapshots, RAGEvaluationDocumentSnapshot{
			SourceDocumentID: id, SourceDocumentHash: document.FileHash, NormalizedSourceHash: normalized.ContentHash,
			SourceNormalizationVersion: normalized.NormalizationVersion, OffsetUnit: normalized.OffsetUnit,
			FileType: document.FileType, PrototypeStatus: "verified",
		})
		if includeContent {
			contents[id] = normalized.Text
		}
	}
	return snapshots, contents, "", nil
}

func (s *RAGEvaluationMVPService) readEvaluationDocument(ctx context.Context, document *types.Knowledge) (string, error) {
	if document.Type == types.KnowledgeTypeManual {
		var metadata types.ManualKnowledgeMetadata
		if err := json.Unmarshal(document.Metadata, &metadata); err != nil {
			return "", err
		}
		if len([]byte(metadata.Content)) > RAGEvaluationMaxDocumentBytes {
			return "", fmt.Errorf("document exceeds evaluation source limit")
		}
		return metadata.Content, nil
	}
	reader, _, err := s.knowledgeService.GetKnowledgeFile(ctx, document.ID)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	limited := io.LimitReader(reader, RAGEvaluationMaxDocumentBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if len(raw) > RAGEvaluationMaxDocumentBytes {
		return "", fmt.Errorf("document exceeds evaluation source limit")
	}
	return string(raw), nil
}

func evaluationPrototypeFileType(fileType, knowledgeType string) bool {
	if knowledgeType == types.KnowledgeTypeManual {
		return true
	}
	switch strings.ToLower(strings.TrimPrefix(fileType, ".")) {
	case "md", "markdown", "txt", "text", "plain":
		return true
	default:
		return false
	}
}

func deterministicCaseSample(cases []*types.EvaluationCase, seed int64, limit int) []*types.EvaluationCase {
	result := append([]*types.EvaluationCase(nil), cases...)
	sort.SliceStable(result, func(i, j int) bool {
		return hashString(fmt.Sprintf("%d:%s", seed, result[i].ID)) < hashString(fmt.Sprintf("%d:%s", seed, result[j].ID))
	})
	return result[:limit]
}

func caseIDs(cases []*types.EvaluationCase) []string {
	result := make([]string, 0, len(cases))
	for _, item := range cases {
		result = append(result, item.ID)
	}
	return result
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func blockerMessage(code string) string {
	messages := map[string]string{
		"DOCUMENT_NOT_FOUND":                 "document is outside the selected knowledge base or missing",
		"DOCUMENT_NOT_PARSED":                "all selected documents must finish parsing before evaluation",
		"DOCUMENT_TYPE_PENDING_VERIFICATION": "this document type has not passed stable-source offset verification",
		"DOCUMENT_SOURCE_UNAVAILABLE":        "normalized source text could not be read",
		"KNOWLEDGE_BASE_NOT_READY":           "knowledge base is unavailable",
		"EMBEDDING_MODEL_NOT_CONFIGURED":     "embedding model is not configured or unavailable",
		"ANSWER_MODEL_NOT_CONFIGURED":        "answer model is not configured or unavailable",
		"JUDGE_MODEL_NOT_CONFIGURED":         "judge model is not configured or unavailable",
	}
	if message := messages[code]; message != "" {
		return message
	}
	return code
}

func decodeJSONMap(value types.JSONMap, target any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func buildVersion() string { return "unknown" }

const ragTestsetGeneratorSystemPrompt = `You generate a compact RAG evaluation testset from the supplied documents. Return strict JSON only. Every answerable case must cite exact verbatim evidence text from one source document. Multi-evidence cases use exactly two evidence spans from the same document. Unanswerable cases must have no reference answer and no evidence, and must state the checked document scope.`
