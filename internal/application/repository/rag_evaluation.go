package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRAGEvaluationNotFound = errors.New("rag evaluation resource not found")
	ErrRAGEvaluationConflict = errors.New("rag evaluation resource conflict")
)

type ragEvaluationRepository struct{ db *gorm.DB }

func NewRAGEvaluationRepository(db *gorm.DB) interfaces.RAGEvaluationRepository {
	return &ragEvaluationRepository{db: db}
}

func evaluationPage(page, size int) (limit, offset int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return size, (page - 1) * size
}

func evaluationNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrRAGEvaluationNotFound
	}
	return err
}

func (r *ragEvaluationRepository) CreateTestset(ctx context.Context, testset *types.EvaluationTestset) error {
	return r.db.WithContext(ctx).Create(testset).Error
}

func (r *ragEvaluationRepository) GetTestset(ctx context.Context, tenantID uint64, kbID, id string) (*types.EvaluationTestset, error) {
	var result types.EvaluationTestset
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) ListTestsets(ctx context.Context, tenantID uint64, kbID string, page, size int) ([]*types.EvaluationTestset, int64, error) {
	limit, offset := evaluationPage(page, size)
	query := r.db.WithContext(ctx).Model(&types.EvaluationTestset{}).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var result []*types.EvaluationTestset
	err := query.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&result).Error
	return result, count, err
}

func (r *ragEvaluationRepository) UpdateTestset(ctx context.Context, tenantID uint64, kbID string, testset *types.EvaluationTestset) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationTestset{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, testset.ID).
		Updates(map[string]any{"name": testset.Name, "status": testset.Status, "archived_at": testset.ArchivedAt, "updated_at": testset.UpdatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationNotFound
	}
	return nil
}

func (r *ragEvaluationRepository) CreateVersion(ctx context.Context, version *types.EvaluationTestsetVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent types.EvaluationTestset
		if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", version.TenantID, version.KnowledgeBaseID, version.TestsetID).First(&parent).Error; err != nil {
			return evaluationNotFound(err)
		}
		return tx.Create(version).Error
	})
}

func (r *ragEvaluationRepository) GetVersion(ctx context.Context, tenantID uint64, kbID, id string) (*types.EvaluationTestsetVersion, error) {
	var result types.EvaluationTestsetVersion
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) ListVersions(ctx context.Context, tenantID uint64, kbID, testsetID string) ([]*types.EvaluationTestsetVersion, error) {
	var result []*types.EvaluationTestsetVersion
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND testset_id = ?", tenantID, kbID, testsetID).
		Order("version_number DESC").Find(&result).Error
	return result, err
}

func (r *ragEvaluationRepository) LockVersionGenerationProfile(ctx context.Context, tenantID uint64, kbID string, version *types.EvaluationTestsetVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing types.EvaluationTestsetVersion
		query := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, version.ID)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&existing).Error; err != nil {
			return evaluationNotFound(err)
		}
		if existing.Status != types.EvaluationVersionDraft {
			return fmt.Errorf("%w: draft version required", ErrRAGEvaluationConflict)
		}
		if existing.ProfileLocked {
			if existing.GenerationProfileHash != version.GenerationProfileHash {
				return fmt.Errorf("%w: generation profile is locked", ErrRAGEvaluationConflict)
			}
			return nil
		}
		result := tx.Model(&types.EvaluationTestsetVersion{}).
			Where("id = ? AND profile_locked = ?", version.ID, false).
			Updates(map[string]any{
				"profile_locked": true, "generation_profile_hash": version.GenerationProfileHash,
				"document_snapshots":           version.DocumentSnapshots,
				"source_normalization_version": version.SourceNormalizationVersion,
				"offset_unit":                  version.OffsetUnit, "split_algorithm": version.SplitAlgorithm,
				"split_seed": version.SplitSeed, "generator_model_snapshot": version.GeneratorModelSnapshot,
				"generator_prompt_version": version.GeneratorPromptVersion,
				"generator_prompt_hash":    version.GeneratorPromptHash, "updated_at": version.UpdatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRAGEvaluationConflict
		}
		return nil
	})
}
func (r *ragEvaluationRepository) PublishVersion(ctx context.Context, tenantID uint64, kbID, id string, publishedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version types.EvaluationTestsetVersion
		query := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&version).Error; err != nil {
			return evaluationNotFound(err)
		}
		if version.Status != types.EvaluationVersionDraft {
			return fmt.Errorf("%w: only a draft version can be published", ErrRAGEvaluationConflict)
		}
		if !version.ProfileLocked {
			return fmt.Errorf("%w: generation profile is not locked", ErrRAGEvaluationConflict)
		}
		var cases []types.EvaluationCase
		if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND version_id = ? AND enabled = ?", tenantID, kbID, id, true).Find(&cases).Error; err != nil {
			return err
		}
		if len(cases) == 0 {
			return fmt.Errorf("%w: no enabled cases", ErrRAGEvaluationConflict)
		}
		for _, evalCase := range cases {
			if evalCase.ReviewStatus != types.EvaluationReviewApproved || evalCase.QualityStatus != types.EvaluationQualityPassed {
				return fmt.Errorf("%w: case %s is not approved and quality-passed", ErrRAGEvaluationConflict, evalCase.ID)
			}
			var evidenceCount int64
			if err := tx.Model(&types.EvaluationGoldEvidence{}).Where("case_id = ?", evalCase.ID).Count(&evidenceCount).Error; err != nil {
				return err
			}
			expected := int64(0)
			switch evalCase.QuestionType {
			case "single_evidence":
				expected = 1
			case "multi_evidence":
				expected = 2
			case "unanswerable":
			default:
				return fmt.Errorf("%w: unsupported question type", ErrRAGEvaluationConflict)
			}
			if evidenceCount != expected {
				return fmt.Errorf("%w: case %s evidence count is %d, want %d", ErrRAGEvaluationConflict, evalCase.ID, evidenceCount, expected)
			}
		}
		result := tx.Model(&types.EvaluationTestsetVersion{}).
			Where("id = ? AND status = ?", id, types.EvaluationVersionDraft).
			Updates(map[string]any{"status": types.EvaluationVersionPublished, "published_at": publishedAt, "updated_at": publishedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRAGEvaluationConflict
		}
		return tx.Model(&types.EvaluationTestset{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, version.TestsetID).
			Updates(map[string]any{"current_published_version_id": id, "updated_at": publishedAt}).Error
	})
}

func (r *ragEvaluationRepository) CreateCase(ctx context.Context, evalCase *types.EvaluationCase, evidence []*types.EvaluationGoldEvidence) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireDraftVersion(tx, evalCase.TenantID, evalCase.KnowledgeBaseID, evalCase.VersionID); err != nil {
			return err
		}
		if err := tx.Create(evalCase).Error; err != nil {
			return err
		}
		if len(evidence) > 0 {
			return tx.Create(&evidence).Error
		}
		return nil
	})
}

func (r *ragEvaluationRepository) CreateGeneratedCases(ctx context.Context, versionID string, cases []*types.EvaluationCase, evidenceByCase map[string][]*types.EvaluationGoldEvidence) error {
	if len(cases) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		first := cases[0]
		if err := requireDraftVersion(tx, first.TenantID, first.KnowledgeBaseID, versionID); err != nil {
			return err
		}
		for _, evalCase := range cases {
			if evalCase.VersionID != versionID || evalCase.TenantID != first.TenantID || evalCase.KnowledgeBaseID != first.KnowledgeBaseID {
				return fmt.Errorf("%w: generated case scope mismatch", ErrRAGEvaluationConflict)
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(evalCase).Error; err != nil {
				return err
			}
			items := evidenceByCase[evalCase.ID]
			if len(items) > 0 {
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&items).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func requireDraftVersion(tx *gorm.DB, tenantID uint64, kbID, versionID string) error {
	var count int64
	if err := tx.Model(&types.EvaluationTestsetVersion{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?", tenantID, kbID, versionID, types.EvaluationVersionDraft).
		Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: draft version required", ErrRAGEvaluationConflict)
	}
	return nil
}

func (r *ragEvaluationRepository) GetCase(ctx context.Context, tenantID uint64, kbID, id string) (*types.EvaluationCase, error) {
	var result types.EvaluationCase
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) ListCases(ctx context.Context, tenantID uint64, kbID, versionID string, page, size int) ([]*types.EvaluationCase, int64, error) {
	limit, offset := evaluationPage(page, size)
	query := r.db.WithContext(ctx).Model(&types.EvaluationCase{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND version_id = ?", tenantID, kbID, versionID)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var result []*types.EvaluationCase
	err := query.Order("created_at ASC, id ASC").Limit(limit).Offset(offset).Find(&result).Error
	return result, count, err
}

func (r *ragEvaluationRepository) ListEvidence(ctx context.Context, tenantID uint64, kbID, caseID string) ([]*types.EvaluationGoldEvidence, error) {
	var result []*types.EvaluationGoldEvidence
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND case_id = ?", tenantID, kbID, caseID).
		Order("ordinal ASC").Find(&result).Error
	return result, err
}

func (r *ragEvaluationRepository) UpdateCase(ctx context.Context, tenantID uint64, kbID string, evalCase *types.EvaluationCase) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireDraftVersion(tx, tenantID, kbID, evalCase.VersionID); err != nil {
			return err
		}
		result := tx.Model(&types.EvaluationCase{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND version_id = ?", tenantID, kbID, evalCase.ID, evalCase.VersionID).
			Select("question", "answerability", "question_type", "difficulty", "reference_answer", "reference_key_points", "expected_refusal", "unanswerable_reason", "checked_scope", "review_status", "quality_status", "quality_reason_codes", "split", "enabled", "updated_at").
			Updates(evalCase)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRAGEvaluationNotFound
		}
		return nil
	})
}

func (r *ragEvaluationRepository) DeleteCase(ctx context.Context, tenantID uint64, kbID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var evalCase types.EvaluationCase
		if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&evalCase).Error; err != nil {
			return evaluationNotFound(err)
		}
		if err := requireDraftVersion(tx, tenantID, kbID, evalCase.VersionID); err != nil {
			return err
		}
		return tx.Delete(&evalCase).Error
	})
}

func (r *ragEvaluationRepository) CreateGeneration(ctx context.Context, generation *types.EvaluationTestsetGeneration) (*types.EvaluationTestsetGeneration, bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "idempotency_key"}}, DoNothing: true,
	}).Create(generation)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		return generation, true, nil
	}
	var existing types.EvaluationTestsetGeneration
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", generation.TenantID, generation.IdempotencyKey).First(&existing).Error
	return &existing, false, err
}

func (r *ragEvaluationRepository) GetGeneration(ctx context.Context, tenantID uint64, kbID, id string) (*types.EvaluationTestsetGeneration, error) {
	var result types.EvaluationTestsetGeneration
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) UpdateGeneration(ctx context.Context, tenantID uint64, kbID string, generation *types.EvaluationTestsetGeneration) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationTestsetGeneration{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, generation.ID).
		Select("status", "accepted_cases", "quality_counts", "error_code", "error_message", "started_at", "finished_at").Updates(generation)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationNotFound
	}
	return nil
}
func (r *ragEvaluationRepository) CancelGeneration(ctx context.Context, tenantID uint64, kbID, id string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationTestsetGeneration{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status IN ?", tenantID, kbID, id, []string{"pending", "running"}).
		Updates(map[string]any{"status": "canceled", "finished_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationConflict
	}
	return nil
}

func (r *ragEvaluationRepository) CreateRunWithItems(ctx context.Context, run *types.EvaluationRun, items []*types.EvaluationRunItem) (*types.EvaluationRun, bool, error) {
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "idempotency_key"}}, DoNothing: true,
		}).Create(run)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		created = true
		if len(items) > 0 {
			return tx.Create(&items).Error
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if created {
		return run, true, nil
	}
	var existing types.EvaluationRun
	err = r.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", run.TenantID, run.IdempotencyKey).First(&existing).Error
	return &existing, false, err
}

func (r *ragEvaluationRepository) GetRun(ctx context.Context, tenantID uint64, kbID, id string) (*types.EvaluationRun, error) {
	var result types.EvaluationRun
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) ListRuns(ctx context.Context, tenantID uint64, kbID string, page, size int) ([]*types.EvaluationRun, int64, error) {
	limit, offset := evaluationPage(page, size)
	query := r.db.WithContext(ctx).Model(&types.EvaluationRun{}).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var result []*types.EvaluationRun
	err := query.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&result).Error
	return result, count, err
}

func (r *ragEvaluationRepository) ListRunItems(ctx context.Context, tenantID uint64, kbID, runID string, page, size int) ([]*types.EvaluationRunItem, int64, error) {
	limit, offset := evaluationPage(page, size)
	query := r.db.WithContext(ctx).Model(&types.EvaluationRunItem{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND run_id = ?", tenantID, kbID, runID)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var result []*types.EvaluationRunItem
	err := query.Order("created_at ASC, id ASC").Limit(limit).Offset(offset).Find(&result).Error
	return result, count, err
}

func (r *ragEvaluationRepository) GetRunItem(ctx context.Context, tenantID uint64, kbID, runID, id string) (*types.EvaluationRunItem, error) {
	var result types.EvaluationRunItem
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND run_id = ? AND id = ?", tenantID, kbID, runID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) UpdateRun(ctx context.Context, tenantID uint64, kbID string, run *types.EvaluationRun) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationRun{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, run.ID).
		Select("status", "stage", "completed_items", "failed_items", "blocked_reason_code", "error_code", "error_message", "observability_status", "langfuse_trace_id", "started_at", "finished_at", "updated_at").
		Updates(run)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationNotFound
	}
	return nil
}

func (r *ragEvaluationRepository) UpdateRunItem(ctx context.Context, tenantID uint64, kbID string, item *types.EvaluationRunItem) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationRunItem{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, item.ID).
		Select("status", "retrieved_contexts", "generated_answer", "citations", "latency", "tokens", "cost", "langfuse_trace_id", "error_code", "error_message", "started_at", "finished_at", "updated_at").
		Updates(item)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationNotFound
	}
	return nil
}

func (r *ragEvaluationRepository) CreateMetricResults(ctx context.Context, results []*types.EvaluationMetricResult) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&results).Error
}

func (r *ragEvaluationRepository) ListMetricResults(ctx context.Context, tenantID uint64, kbID, runID, itemID string) ([]*types.EvaluationMetricResult, error) {
	query := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND run_id = ?", tenantID, kbID, runID)
	if itemID != "" {
		query = query.Where("run_item_id = ?", itemID)
	}
	var result []*types.EvaluationMetricResult
	err := query.Order("run_item_id ASC, metric_name ASC").Find(&result).Error
	return result, err
}

func (r *ragEvaluationRepository) CountActiveRuns(ctx context.Context, tenantID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&types.EvaluationRun{}).
		Where("tenant_id = ? AND status IN ?", tenantID, []string{types.EvaluationRunPending, types.EvaluationRunRunning}).Count(&count).Error
	return count, err
}

func (r *ragEvaluationRepository) CancelRun(ctx context.Context, tenantID uint64, kbID, id string, at time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&types.EvaluationRun{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status IN ?", tenantID, kbID, id, []string{types.EvaluationRunPending, types.EvaluationRunRunning}).
			Updates(map[string]any{"status": types.EvaluationRunCanceled, "finished_at": at, "updated_at": at})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRAGEvaluationConflict
		}
		return tx.Model(&types.EvaluationRunItem{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND run_id = ? AND status IN ?", tenantID, kbID, id, []string{"pending", "running"}).
			Updates(map[string]any{"status": "canceled", "finished_at": at, "updated_at": at}).Error
	})
}

func (r *ragEvaluationRepository) CreateSchedule(ctx context.Context, schedule *types.EvaluationSchedule) error {
	return r.db.WithContext(ctx).Create(schedule).Error
}

func (r *ragEvaluationRepository) GetSchedule(ctx context.Context, tenantID uint64, kbID, id string) (*types.EvaluationSchedule, error) {
	var result types.EvaluationSchedule
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) ListSchedules(ctx context.Context, tenantID uint64, kbID string, page, size int) ([]*types.EvaluationSchedule, int64, error) {
	limit, offset := evaluationPage(page, size)
	query := r.db.WithContext(ctx).Model(&types.EvaluationSchedule{}).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var result []*types.EvaluationSchedule
	err := query.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&result).Error
	return result, count, err
}

func (r *ragEvaluationRepository) UpdateSchedule(ctx context.Context, tenantID uint64, kbID string, schedule *types.EvaluationSchedule) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationSchedule{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND archived_at IS NULL", tenantID, kbID, schedule.ID).
		Select("name", "cron_expression", "timezone", "enabled", "skip_if_active", "run_template", "next_run_at", "last_run_status", "updated_at").
		Updates(schedule)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationNotFound
	}
	return nil
}

func (r *ragEvaluationRepository) ArchiveSchedule(ctx context.Context, tenantID uint64, kbID, id string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationSchedule{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND archived_at IS NULL", tenantID, kbID, id).
		Updates(map[string]any{"enabled": false, "archived_at": at, "updated_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationNotFound
	}
	return nil
}

func (r *ragEvaluationRepository) ClaimScheduleSlot(ctx context.Context, slot *types.EvaluationScheduleSlot) (bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "schedule_id"}, {Name: "scheduled_at_utc"}}, DoNothing: true,
	}).Create(slot)
	return result.RowsAffected == 1, result.Error
}

func (r *ragEvaluationRepository) GetJudgeCalibration(ctx context.Context, tenantID uint64, kbID, id string) (*types.EvaluationJudgeCalibration, error) {
	var result types.EvaluationJudgeCalibration
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}
func (r *ragEvaluationRepository) CreateDiagnosis(ctx context.Context, diagnosis *types.EvaluationFailureDiagnosis) error {
	return r.db.WithContext(ctx).Create(diagnosis).Error
}

func (r *ragEvaluationRepository) GetDiagnosis(ctx context.Context, tenantID uint64, kbID, sourceRunID string) (*types.EvaluationFailureDiagnosis, error) {
	var result types.EvaluationFailureDiagnosis
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND source_run_id = ?", tenantID, kbID, sourceRunID).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) CreateExperimentWithVariants(ctx context.Context, experiment *types.EvaluationChunkExperiment, variants []*types.EvaluationChunkExperimentVariant) (*types.EvaluationChunkExperiment, bool, error) {
	if len(variants) < 2 || len(variants) > 3 {
		return nil, false, fmt.Errorf("%w: experiment requires baseline and one or two candidates", ErrRAGEvaluationConflict)
	}
	baselineCount := 0
	for _, variant := range variants {
		if variant.Role == "baseline" {
			baselineCount++
		}
	}
	if baselineCount != 1 {
		return nil, false, fmt.Errorf("%w: exactly one baseline is required", ErrRAGEvaluationConflict)
	}
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var diagnosis types.EvaluationFailureDiagnosis
		if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ? AND classification = ?",
			experiment.TenantID, experiment.KnowledgeBaseID, experiment.DiagnosisID, "completed", "chunking_likely").First(&diagnosis).Error; err != nil {
			return fmt.Errorf("%w: completed chunking_likely diagnosis required", ErrRAGEvaluationConflict)
		}
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "idempotency_key"}}, DoNothing: true,
		}).Create(experiment)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		created = true
		return tx.Create(&variants).Error
	})
	if err != nil {
		return nil, false, err
	}
	if created {
		return experiment, true, nil
	}
	var existing types.EvaluationChunkExperiment
	err = r.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", experiment.TenantID, experiment.IdempotencyKey).First(&existing).Error
	return &existing, false, err
}

func (r *ragEvaluationRepository) GetExperiment(ctx context.Context, tenantID uint64, kbID, id string) (*types.EvaluationChunkExperiment, error) {
	var result types.EvaluationChunkExperiment
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).First(&result).Error
	return &result, evaluationNotFound(err)
}

func (r *ragEvaluationRepository) ListExperimentVariants(ctx context.Context, tenantID uint64, kbID, experimentID string) ([]*types.EvaluationChunkExperimentVariant, error) {
	var result []*types.EvaluationChunkExperimentVariant
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND experiment_id = ?", tenantID, kbID, experimentID).
		Order("role ASC, variant_key ASC").Find(&result).Error
	return result, err
}

func (r *ragEvaluationRepository) UpdateExperiment(ctx context.Context, tenantID uint64, kbID string, experiment *types.EvaluationChunkExperiment) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationChunkExperiment{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, experiment.ID).
		Select("status", "provisional_variant_id", "cleanup_status", "error_code", "error_message", "started_at", "finished_at", "updated_at").Updates(experiment)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationNotFound
	}
	return nil
}

func (r *ragEvaluationRepository) UpdateExperimentVariant(ctx context.Context, tenantID uint64, kbID string, variant *types.EvaluationChunkExperimentVariant) error {
	result := r.db.WithContext(ctx).Model(&types.EvaluationChunkExperimentVariant{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, variant.ID).
		Select("execution_knowledge_base_id", "status", "tuning_metrics", "holdout_metrics", "resource_metrics", "cleanup_status", "updated_at").Updates(variant)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRAGEvaluationNotFound
	}
	return nil
}

func (r *ragEvaluationRepository) CreateRecommendation(ctx context.Context, recommendation *types.EvaluationChunkRecommendation) error {
	return r.db.WithContext(ctx).Create(recommendation).Error
}

func (r *ragEvaluationRepository) GetRecommendation(ctx context.Context, tenantID uint64, kbID, experimentID string) (*types.EvaluationChunkRecommendation, error) {
	var result types.EvaluationChunkRecommendation
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND experiment_id = ?", tenantID, kbID, experimentID).First(&result).Error
	return &result, evaluationNotFound(err)
}
