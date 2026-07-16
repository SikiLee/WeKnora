package types

import "time"

const (
	EvaluationVersionDraft     = "draft"
	EvaluationVersionPublished = "published"
	EvaluationVersionArchived  = "archived"

	EvaluationReviewPending  = "pending"
	EvaluationReviewApproved = "approved"
	EvaluationReviewRejected = "rejected"

	EvaluationQualityPending = "pending"
	EvaluationQualityPassed  = "passed"
	EvaluationQualityFailed  = "failed"

	EvaluationRunPending   = "pending"
	EvaluationRunRunning   = "running"
	EvaluationRunCompleted = "completed"
	EvaluationRunPartial   = "partial"
	EvaluationRunFailed    = "failed"
	EvaluationRunCanceled  = "canceled"
	EvaluationRunBlocked   = "blocked"
)

// EvaluationTestset is a tenant- and knowledge-base-scoped logical testset.
// Content lives in immutable published versions.
type EvaluationTestset struct {
	ID                        string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID                  uint64     `json:"tenant_id" gorm:"not null;index:idx_eval_testsets_scope"`
	KnowledgeBaseID           string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_testsets_scope"`
	Name                      string     `json:"name" gorm:"type:varchar(255);not null"`
	Status                    string     `json:"status" gorm:"type:varchar(32);not null"`
	CurrentPublishedVersionID *string    `json:"current_published_version_id,omitempty" gorm:"type:varchar(36)"`
	CreatedBy                 string     `json:"created_by" gorm:"type:varchar(36);not null"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
	ArchivedAt                *time.Time `json:"archived_at,omitempty"`
}

func (EvaluationTestset) TableName() string { return "evaluation_testsets" }

// EvaluationTestsetVersion freezes generation provenance and source snapshots.
// Empty drafts acquire their profile lock on their first accepted generation.
type EvaluationTestsetVersion struct {
	ID                         string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID                   uint64     `json:"tenant_id" gorm:"not null;index:idx_eval_versions_scope"`
	KnowledgeBaseID            string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_versions_scope"`
	TestsetID                  string     `json:"testset_id" gorm:"type:varchar(36);not null;uniqueIndex:uq_eval_version_no"`
	VersionNumber              int        `json:"version_number" gorm:"not null;uniqueIndex:uq_eval_version_no"`
	Status                     string     `json:"status" gorm:"type:varchar(32);not null"`
	ProfileLocked              bool       `json:"profile_locked" gorm:"not null;default:false"`
	GenerationProfileHash      string     `json:"generation_profile_hash" gorm:"type:varchar(64)"`
	DocumentSnapshots          JSONMap    `json:"document_snapshots" gorm:"type:jsonb;not null"`
	SourceNormalizationVersion string     `json:"source_normalization_version" gorm:"type:varchar(64)"`
	OffsetUnit                 string     `json:"offset_unit" gorm:"type:varchar(32)"`
	SplitAlgorithm             string     `json:"split_algorithm" gorm:"type:varchar(64)"`
	SplitSeed                  int64      `json:"split_seed"`
	GeneratorModelSnapshot     JSONMap    `json:"generator_model_snapshot" gorm:"type:jsonb;not null"`
	GeneratorPromptVersion     string     `json:"generator_prompt_version" gorm:"type:varchar(128)"`
	GeneratorPromptHash        string     `json:"generator_prompt_hash" gorm:"type:varchar(64)"`
	CreatedBy                  string     `json:"created_by" gorm:"type:varchar(36);not null"`
	PublishedAt                *time.Time `json:"published_at,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

func (EvaluationTestsetVersion) TableName() string { return "evaluation_testset_versions" }

type EvaluationCase struct {
	ID                   string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID             uint64    `json:"tenant_id" gorm:"not null;index:idx_eval_cases_scope"`
	KnowledgeBaseID      string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_cases_scope"`
	VersionID            string    `json:"version_id" gorm:"type:varchar(36);not null;index"`
	Question             string    `json:"question" gorm:"type:text;not null"`
	Answerability        string    `json:"answerability" gorm:"type:varchar(32);not null"`
	QuestionType         string    `json:"question_type" gorm:"type:varchar(32);not null"`
	Difficulty           string    `json:"difficulty" gorm:"type:varchar(32);not null"`
	ReferenceAnswer      string    `json:"reference_answer" gorm:"type:text"`
	ReferenceKeyPoints   JSONMap   `json:"reference_key_points" gorm:"type:jsonb;not null"`
	ExpectedRefusal      string    `json:"expected_refusal" gorm:"type:text"`
	UnanswerableReason   string    `json:"unanswerable_reason" gorm:"type:text"`
	CheckedScope         JSONMap   `json:"checked_scope" gorm:"type:jsonb;not null"`
	ReviewStatus         string    `json:"review_status" gorm:"type:varchar(32);not null"`
	QualityStatus        string    `json:"quality_status" gorm:"type:varchar(32);not null"`
	QualityReasonCodes   JSONMap   `json:"quality_reason_codes" gorm:"type:jsonb;not null"`
	Split                string    `json:"split" gorm:"type:varchar(16);not null"`
	Enabled              bool      `json:"enabled" gorm:"not null;default:true"`
	GenerationProvenance JSONMap   `json:"generation_provenance" gorm:"type:jsonb;not null"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (EvaluationCase) TableName() string { return "evaluation_cases" }

type EvaluationGoldEvidence struct {
	ID                         string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID                   uint64    `json:"tenant_id" gorm:"not null;index:idx_eval_evidence_scope"`
	KnowledgeBaseID            string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_evidence_scope"`
	CaseID                     string    `json:"case_id" gorm:"type:varchar(36);not null;index"`
	Ordinal                    int       `json:"ordinal" gorm:"not null"`
	SourceDocumentID           string    `json:"source_document_id" gorm:"type:varchar(36);not null"`
	SourceDocumentHash         string    `json:"source_document_hash" gorm:"type:varchar(64);not null"`
	NormalizedSourceHash       string    `json:"normalized_source_hash" gorm:"type:varchar(64);not null"`
	SourceNormalizationVersion string    `json:"source_normalization_version" gorm:"type:varchar(64);not null"`
	OffsetUnit                 string    `json:"offset_unit" gorm:"type:varchar(32);not null"`
	StartAt                    int       `json:"start_at" gorm:"not null"`
	EndAt                      int       `json:"end_at" gorm:"not null"`
	EvidenceText               string    `json:"evidence_text" gorm:"type:text;not null"`
	EvidenceHash               string    `json:"evidence_hash" gorm:"type:varchar(64);not null"`
	CreatedAt                  time.Time `json:"created_at"`
}

func (EvaluationGoldEvidence) TableName() string { return "evaluation_gold_evidences" }

type EvaluationTestsetGeneration struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id" gorm:"not null;index:idx_eval_generations_scope"`
	KnowledgeBaseID string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_generations_scope"`
	TestsetID       string     `json:"testset_id" gorm:"type:varchar(36);not null"`
	VersionID       string     `json:"version_id" gorm:"type:varchar(36);not null"`
	Status          string     `json:"status" gorm:"type:varchar(32);not null"`
	IdempotencyKey  string     `json:"-" gorm:"type:varchar(128);not null;uniqueIndex:uq_eval_generation_idem"`
	ProfileSnapshot JSONMap    `json:"profile_snapshot" gorm:"type:jsonb;not null"`
	RequestedCases  int        `json:"requested_cases" gorm:"not null"`
	AcceptedCases   int        `json:"accepted_cases" gorm:"not null;default:0"`
	QualityCounts   JSONMap    `json:"quality_counts" gorm:"type:jsonb;not null"`
	ErrorCode       string     `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	ErrorMessage    string     `json:"error_message,omitempty" gorm:"type:text"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

func (EvaluationTestsetGeneration) TableName() string { return "evaluation_testset_generations" }

type EvaluationRun struct {
	ID                  string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID            uint64     `json:"tenant_id" gorm:"not null;index:idx_eval_runs_scope"`
	KnowledgeBaseID     string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_runs_scope"`
	TestsetVersionID    string     `json:"testset_version_id" gorm:"type:varchar(36);not null"`
	ParentRunID         *string    `json:"parent_run_id,omitempty" gorm:"type:varchar(36)"`
	TriggerType         string     `json:"trigger_type" gorm:"type:varchar(32);not null"`
	Status              string     `json:"status" gorm:"type:varchar(32);not null"`
	Stage               string     `json:"stage" gorm:"type:varchar(64);not null"`
	IdempotencyKey      string     `json:"-" gorm:"type:varchar(128);not null;uniqueIndex:uq_eval_run_idem"`
	Snapshot            JSONMap    `json:"snapshot" gorm:"type:jsonb;not null"`
	SampledCaseIDs      JSONMap    `json:"sampled_case_ids" gorm:"type:jsonb;not null"`
	TotalItems          int        `json:"total_items" gorm:"not null"`
	CompletedItems      int        `json:"completed_items" gorm:"not null;default:0"`
	FailedItems         int        `json:"failed_items" gorm:"not null;default:0"`
	BlockedReasonCode   string     `json:"blocked_reason_code,omitempty" gorm:"type:varchar(64)"`
	ErrorCode           string     `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	ErrorMessage        string     `json:"error_message,omitempty" gorm:"type:text"`
	ObservabilityStatus string     `json:"observability_status" gorm:"type:varchar(32);not null"`
	LangfuseTraceID     string     `json:"langfuse_trace_id,omitempty" gorm:"type:varchar(128)"`
	EstimatedCalls      int        `json:"estimated_calls" gorm:"not null;default:0"`
	CreatedBy           string     `json:"created_by" gorm:"type:varchar(36);not null"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (EvaluationRun) TableName() string { return "evaluation_runs" }

type EvaluationRunItem struct {
	ID                    string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID              uint64     `json:"tenant_id" gorm:"not null;index:idx_eval_run_items_scope"`
	KnowledgeBaseID       string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_run_items_scope"`
	RunID                 string     `json:"run_id" gorm:"type:varchar(36);not null;uniqueIndex:uq_eval_run_case"`
	CaseID                string     `json:"case_id" gorm:"type:varchar(36);not null;uniqueIndex:uq_eval_run_case"`
	Status                string     `json:"status" gorm:"type:varchar(32);not null"`
	AnswerabilitySnapshot string     `json:"answerability_snapshot" gorm:"type:varchar(32);not null"`
	QuestionSnapshot      string     `json:"question_snapshot" gorm:"type:text;not null"`
	ReferenceSnapshot     JSONMap    `json:"reference_snapshot" gorm:"type:jsonb;not null"`
	GoldEvidenceSnapshot  JSONMap    `json:"gold_evidence_snapshot" gorm:"type:jsonb;not null"`
	RetrievedContexts     JSONMap    `json:"retrieved_contexts" gorm:"type:jsonb;not null"`
	GeneratedAnswer       string     `json:"generated_answer" gorm:"type:text"`
	Citations             JSONMap    `json:"citations" gorm:"type:jsonb;not null"`
	Latency               JSONMap    `json:"latency" gorm:"type:jsonb;not null"`
	Tokens                JSONMap    `json:"tokens" gorm:"type:jsonb;not null"`
	Cost                  JSONMap    `json:"cost" gorm:"type:jsonb;not null"`
	TraceCorrelationID    string     `json:"trace_correlation_id" gorm:"type:varchar(128);not null"`
	LangfuseTraceID       string     `json:"langfuse_trace_id,omitempty" gorm:"type:varchar(128)"`
	ErrorCode             string     `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	ErrorMessage          string     `json:"error_message,omitempty" gorm:"type:text"`
	CreatedAt             time.Time  `json:"created_at"`
	StartedAt             *time.Time `json:"started_at,omitempty"`
	FinishedAt            *time.Time `json:"finished_at,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (EvaluationRunItem) TableName() string { return "evaluation_run_items" }

type EvaluationMetricResult struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64    `json:"tenant_id" gorm:"not null;index:idx_eval_metrics_scope"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_metrics_scope"`
	RunID           string    `json:"run_id" gorm:"type:varchar(36);not null;index"`
	RunItemID       string    `json:"run_item_id" gorm:"type:varchar(36);not null;uniqueIndex:uq_eval_item_metric"`
	MetricName      string    `json:"metric_name" gorm:"type:varchar(64);not null;uniqueIndex:uq_eval_item_metric"`
	MetricVersion   string    `json:"metric_version" gorm:"type:varchar(64);not null;uniqueIndex:uq_eval_item_metric"`
	Status          string    `json:"status" gorm:"type:varchar(16);not null"`
	Value           *float64  `json:"value"`
	ReasonCode      string    `json:"reason_code,omitempty" gorm:"type:varchar(64)"`
	Details         JSONMap   `json:"details" gorm:"type:jsonb;not null"`
	JudgeExecution  JSONMap   `json:"judge_execution" gorm:"type:jsonb;not null"`
	LangfuseScoreID string    `json:"langfuse_score_id,omitempty" gorm:"type:varchar(128)"`
	CreatedAt       time.Time `json:"created_at"`
}

func (EvaluationMetricResult) TableName() string { return "evaluation_metric_results" }

type EvaluationSchedule struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id" gorm:"not null;index:idx_eval_schedules_scope"`
	KnowledgeBaseID string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_schedules_scope"`
	Name            string     `json:"name" gorm:"type:varchar(255);not null"`
	CronExpression  string     `json:"cron_expression" gorm:"type:varchar(128);not null"`
	Timezone        string     `json:"timezone" gorm:"type:varchar(64);not null"`
	Enabled         bool       `json:"enabled" gorm:"not null;default:true"`
	SkipIfActive    bool       `json:"skip_if_active" gorm:"not null;default:true"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	RunTemplate     JSONMap    `json:"run_template" gorm:"type:jsonb;not null"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	LastRunStatus   string     `json:"last_run_status,omitempty" gorm:"type:varchar(32)"`
	CreatedBy       string     `json:"created_by" gorm:"type:varchar(36);not null"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (EvaluationSchedule) TableName() string { return "evaluation_schedules" }

type EvaluationScheduleSlot struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id" gorm:"not null"`
	KnowledgeBaseID string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null"`
	ScheduleID      string     `json:"schedule_id" gorm:"type:varchar(36);not null;uniqueIndex:uq_eval_schedule_slot"`
	ScheduledAtUTC  time.Time  `json:"scheduled_at_utc" gorm:"not null;uniqueIndex:uq_eval_schedule_slot"`
	Status          string     `json:"status" gorm:"type:varchar(32);not null"`
	RunID           *string    `json:"run_id,omitempty" gorm:"type:varchar(36)"`
	ReasonCode      string     `json:"reason_code,omitempty" gorm:"type:varchar(64)"`
	CreatedAt       time.Time  `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

func (EvaluationScheduleSlot) TableName() string { return "evaluation_schedule_slots" }

type EvaluationJudgeCalibration struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id" gorm:"not null;index:idx_eval_calibrations_scope"`
	KnowledgeBaseID string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_calibrations_scope"`
	Version         string     `json:"version" gorm:"type:varchar(128);not null"`
	MetricName      string     `json:"metric_name" gorm:"type:varchar(64);not null"`
	Status          string     `json:"status" gorm:"type:varchar(32);not null"`
	ModelSnapshot   JSONMap    `json:"model_snapshot" gorm:"type:jsonb;not null"`
	PromptVersion   string     `json:"prompt_version" gorm:"type:varchar(128);not null"`
	PromptHash      string     `json:"prompt_hash" gorm:"type:varchar(64);not null"`
	ParserVersion   string     `json:"parser_version" gorm:"type:varchar(64);not null"`
	Report          JSONMap    `json:"report" gorm:"type:jsonb;not null"`
	PassedAt        *time.Time `json:"passed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (EvaluationJudgeCalibration) TableName() string { return "evaluation_judge_calibrations" }

type EvaluationFailureDiagnosis struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64    `json:"tenant_id" gorm:"not null;index:idx_eval_diagnoses_scope"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_diagnoses_scope"`
	SourceRunID     string    `json:"source_run_id" gorm:"type:varchar(36);not null;uniqueIndex"`
	Version         string    `json:"version" gorm:"type:varchar(64);not null"`
	Status          string    `json:"status" gorm:"type:varchar(32);not null"`
	Classification  string    `json:"classification" gorm:"type:varchar(32);not null"`
	ReasonCode      string    `json:"reason_code" gorm:"type:varchar(64);not null"`
	EvidenceSignals JSONMap   `json:"evidence_signals" gorm:"type:jsonb;not null"`
	TargetK         int       `json:"target_k" gorm:"not null"`
	ValidCases      int       `json:"valid_cases" gorm:"not null"`
	ErrorCode       string    `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	ErrorMessage    string    `json:"error_message,omitempty" gorm:"type:text"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (EvaluationFailureDiagnosis) TableName() string { return "evaluation_failure_diagnoses" }

type EvaluationChunkExperiment struct {
	ID                   string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID             uint64     `json:"tenant_id" gorm:"not null;index:idx_eval_experiments_scope"`
	KnowledgeBaseID      string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_eval_experiments_scope"`
	SourceRunID          string     `json:"source_run_id" gorm:"type:varchar(36);not null;uniqueIndex"`
	DiagnosisID          string     `json:"diagnosis_id" gorm:"type:varchar(36);not null"`
	TestsetVersionID     string     `json:"testset_version_id" gorm:"type:varchar(36);not null"`
	Status               string     `json:"status" gorm:"type:varchar(32);not null"`
	IdempotencyKey       string     `json:"-" gorm:"type:varchar(128);not null;uniqueIndex:uq_eval_experiment_idem"`
	FixedSnapshot        JSONMap    `json:"fixed_snapshot" gorm:"type:jsonb;not null"`
	ProvisionalVariantID *string    `json:"provisional_variant_id,omitempty" gorm:"type:varchar(36)"`
	EstimatedCalls       int        `json:"estimated_calls" gorm:"not null"`
	CleanupStatus        string     `json:"cleanup_status" gorm:"type:varchar(32);not null"`
	ErrorCode            string     `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	ErrorMessage         string     `json:"error_message,omitempty" gorm:"type:text"`
	CreatedBy            string     `json:"created_by" gorm:"type:varchar(36);not null"`
	CreatedAt            time.Time  `json:"created_at"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	FinishedAt           *time.Time `json:"finished_at,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (EvaluationChunkExperiment) TableName() string { return "evaluation_chunk_experiments" }

type EvaluationChunkExperimentVariant struct {
	ID                       string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID                 uint64    `json:"tenant_id" gorm:"not null"`
	KnowledgeBaseID          string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null"`
	ExperimentID             string    `json:"experiment_id" gorm:"type:varchar(36);not null;uniqueIndex:uq_eval_variant_key"`
	VariantKey               string    `json:"variant_key" gorm:"type:varchar(32);not null;uniqueIndex:uq_eval_variant_key"`
	Role                     string    `json:"role" gorm:"type:varchar(16);not null"`
	ChunkingSnapshot         JSONMap   `json:"chunking_snapshot" gorm:"type:jsonb;not null"`
	ConfigHash               string    `json:"config_hash" gorm:"type:varchar(64);not null"`
	SourceMapping            JSONMap   `json:"source_mapping" gorm:"type:jsonb;not null"`
	ExecutionKnowledgeBaseID string    `json:"execution_knowledge_base_id,omitempty" gorm:"type:varchar(36)"`
	Status                   string    `json:"status" gorm:"type:varchar(32);not null"`
	TuningMetrics            JSONMap   `json:"tuning_metrics" gorm:"type:jsonb;not null"`
	HoldoutMetrics           JSONMap   `json:"holdout_metrics" gorm:"type:jsonb;not null"`
	ResourceMetrics          JSONMap   `json:"resource_metrics" gorm:"type:jsonb;not null"`
	CleanupStatus            string    `json:"cleanup_status" gorm:"type:varchar(32);not null"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

func (EvaluationChunkExperimentVariant) TableName() string {
	return "evaluation_chunk_experiment_variants"
}

type EvaluationChunkRecommendation struct {
	ID                 string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID           uint64    `json:"tenant_id" gorm:"not null"`
	KnowledgeBaseID    string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null"`
	ExperimentID       string    `json:"experiment_id" gorm:"type:varchar(36);not null;uniqueIndex"`
	PolicyVersion      string    `json:"policy_version" gorm:"type:varchar(64);not null"`
	Decision           string    `json:"decision" gorm:"type:varchar(32);not null"`
	BaselineVariantID  string    `json:"baseline_variant_id" gorm:"type:varchar(36);not null"`
	CandidateVariantID *string   `json:"candidate_variant_id,omitempty" gorm:"type:varchar(36)"`
	MetricDeltas       JSONMap   `json:"metric_deltas" gorm:"type:jsonb;not null"`
	Evidence           JSONMap   `json:"evidence" gorm:"type:jsonb;not null"`
	Limitations        JSONMap   `json:"limitations" gorm:"type:jsonb;not null"`
	CreatedAt          time.Time `json:"created_at"`
}

func (EvaluationChunkRecommendation) TableName() string {
	return "evaluation_chunk_recommendations"
}
