package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newRAGEvaluationMockRepository(t *testing.T) (*ragEvaluationRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm: %v", err)
	}
	return &ragEvaluationRepository{db: gormDB}, mock
}

func TestClaimScheduleSlotIsIdempotent(t *testing.T) {
	repo, mock := newRAGEvaluationMockRepository(t)
	slot := &types.EvaluationScheduleSlot{
		ID: "slot", TenantID: 7, KnowledgeBaseID: "kb", ScheduleID: "schedule",
		ScheduledAtUTC: time.Date(2026, 7, 16, 2, 0, 0, 0, time.UTC), Status: "claimed",
	}
	insert := regexp.QuoteMeta(`INSERT INTO "evaluation_schedule_slots"`)
	mock.ExpectBegin()
	mock.ExpectExec(insert).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	claimed, err := repo.ClaimScheduleSlot(context.Background(), slot)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	mock.ExpectBegin()
	mock.ExpectExec(insert).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	claimed, err = repo.ClaimScheduleSlot(context.Background(), slot)
	if err != nil || claimed {
		t.Fatalf("duplicate claim = %v, %v", claimed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetRunAlwaysScopesTenantAndKnowledgeBase(t *testing.T) {
	repo, mock := newRAGEvaluationMockRepository(t)
	query := `SELECT \* FROM "evaluation_runs" WHERE tenant_id = \$1 AND knowledge_base_id = \$2 AND id = \$3`
	rows := sqlmock.NewRows([]string{"id", "tenant_id", "knowledge_base_id", "status"}).
		AddRow("run", 7, "kb", "completed")
	mock.ExpectQuery(query).WithArgs(uint64(7), "kb", "run", 1).WillReturnRows(rows)
	got, err := repo.GetRun(context.Background(), 7, "kb", "run")
	if err != nil || got.ID != "run" || got.TenantID != 7 || got.KnowledgeBaseID != "kb" {
		t.Fatalf("run = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExperimentRejectsVariantOverflowBeforeDatabase(t *testing.T) {
	repo := &ragEvaluationRepository{}
	variants := []*types.EvaluationChunkExperimentVariant{
		{Role: "baseline"}, {Role: "candidate"}, {Role: "candidate"}, {Role: "candidate"},
	}
	if _, _, err := repo.CreateExperimentWithVariants(context.Background(), &types.EvaluationChunkExperiment{}, variants); err == nil {
		t.Fatal("accepted more than baseline plus two candidates")
	}
}
