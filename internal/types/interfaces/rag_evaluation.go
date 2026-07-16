package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// RAGEvaluationRepository is the persistence boundary for the MVP evaluation
// domain. Every read and write carries tenant and knowledge-base scope.
type RAGEvaluationRepository interface {
	CreateTestset(context.Context, *types.EvaluationTestset) error
	GetTestset(context.Context, uint64, string, string) (*types.EvaluationTestset, error)
	ListTestsets(context.Context, uint64, string, int, int) ([]*types.EvaluationTestset, int64, error)
	UpdateTestset(context.Context, uint64, string, *types.EvaluationTestset) error

	CreateVersion(context.Context, *types.EvaluationTestsetVersion) error
	GetVersion(context.Context, uint64, string, string) (*types.EvaluationTestsetVersion, error)
	ListVersions(context.Context, uint64, string, string) ([]*types.EvaluationTestsetVersion, error)
	LockVersionGenerationProfile(context.Context, uint64, string, *types.EvaluationTestsetVersion) error
	PublishVersion(context.Context, uint64, string, string, time.Time) error

	CreateCase(context.Context, *types.EvaluationCase, []*types.EvaluationGoldEvidence) error
	CreateGeneratedCases(context.Context, string, []*types.EvaluationCase, map[string][]*types.EvaluationGoldEvidence) error
	GetCase(context.Context, uint64, string, string) (*types.EvaluationCase, error)
	ListCases(context.Context, uint64, string, string, int, int) ([]*types.EvaluationCase, int64, error)
	ListEvidence(context.Context, uint64, string, string) ([]*types.EvaluationGoldEvidence, error)
	UpdateCase(context.Context, uint64, string, *types.EvaluationCase) error
	DeleteCase(context.Context, uint64, string, string) error

	CreateGeneration(context.Context, *types.EvaluationTestsetGeneration) (*types.EvaluationTestsetGeneration, bool, error)
	GetGeneration(context.Context, uint64, string, string) (*types.EvaluationTestsetGeneration, error)
	UpdateGeneration(context.Context, uint64, string, *types.EvaluationTestsetGeneration) error
	CancelGeneration(context.Context, uint64, string, string, time.Time) error

	CreateRunWithItems(context.Context, *types.EvaluationRun, []*types.EvaluationRunItem) (*types.EvaluationRun, bool, error)
	GetRun(context.Context, uint64, string, string) (*types.EvaluationRun, error)
	ListRuns(context.Context, uint64, string, int, int) ([]*types.EvaluationRun, int64, error)
	ListRunItems(context.Context, uint64, string, string, int, int) ([]*types.EvaluationRunItem, int64, error)
	GetRunItem(context.Context, uint64, string, string, string) (*types.EvaluationRunItem, error)
	UpdateRun(context.Context, uint64, string, *types.EvaluationRun) error
	UpdateRunItem(context.Context, uint64, string, *types.EvaluationRunItem) error
	CreateMetricResults(context.Context, []*types.EvaluationMetricResult) error
	ListMetricResults(context.Context, uint64, string, string, string) ([]*types.EvaluationMetricResult, error)
	CountActiveRuns(context.Context, uint64) (int64, error)
	CancelRun(context.Context, uint64, string, string, time.Time) error

	CreateSchedule(context.Context, *types.EvaluationSchedule) error
	GetSchedule(context.Context, uint64, string, string) (*types.EvaluationSchedule, error)
	ListSchedules(context.Context, uint64, string, int, int) ([]*types.EvaluationSchedule, int64, error)
	UpdateSchedule(context.Context, uint64, string, *types.EvaluationSchedule) error
	ArchiveSchedule(context.Context, uint64, string, string, time.Time) error
	ClaimScheduleSlot(context.Context, *types.EvaluationScheduleSlot) (bool, error)

	GetJudgeCalibration(context.Context, uint64, string, string) (*types.EvaluationJudgeCalibration, error)
	CreateDiagnosis(context.Context, *types.EvaluationFailureDiagnosis) error
	GetDiagnosis(context.Context, uint64, string, string) (*types.EvaluationFailureDiagnosis, error)
	CreateExperimentWithVariants(context.Context, *types.EvaluationChunkExperiment, []*types.EvaluationChunkExperimentVariant) (*types.EvaluationChunkExperiment, bool, error)
	GetExperiment(context.Context, uint64, string, string) (*types.EvaluationChunkExperiment, error)
	ListExperimentVariants(context.Context, uint64, string, string) ([]*types.EvaluationChunkExperimentVariant, error)
	UpdateExperiment(context.Context, uint64, string, *types.EvaluationChunkExperiment) error
	UpdateExperimentVariant(context.Context, uint64, string, *types.EvaluationChunkExperimentVariant) error
	CreateRecommendation(context.Context, *types.EvaluationChunkRecommendation) error
	GetRecommendation(context.Context, uint64, string, string) (*types.EvaluationChunkRecommendation, error)
}
