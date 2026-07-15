package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type feedbackServiceFixture struct {
	db           *gorm.DB
	service      interfaces.FeedbackService
	feedbackRepo interfaces.FeedbackRepository
	messageRepo  interfaces.MessageRepository
	sessionRepo  interfaces.SessionRepository
	chunkRepo    interfaces.ChunkRepository
	session      *types.Session
	message      *types.Message
	sharedChunk  *types.Chunk
	ctx          context.Context
}

type failingFeedbackSessionRepository struct {
	interfaces.SessionRepository
	err error
}

func (r failingFeedbackSessionRepository) Get(context.Context, uint64, string, string) (*types.Session, error) {
	return nil, r.err
}

type failingFeedbackMessageRepository struct {
	interfaces.MessageRepository
	err error
}

type resetSnapshotFeedbackRepository struct {
	interfaces.FeedbackRepository
	detail          *types.ChunkFeedbackDetail
	detailReadCalls int
}

func (r *resetSnapshotFeedbackRepository) ResetChunkFeedback(
	context.Context,
	uint64,
	string,
	string,
	string,
	*types.ChunkFeedbackConfig,
) (*types.ChunkFeedbackDetail, error) {
	return r.detail, nil
}

func (r *resetSnapshotFeedbackRepository) GetChunkFeedbackDetail(
	context.Context,
	uint64,
	string,
	string,
) (*types.ChunkFeedbackDetail, error) {
	r.detailReadCalls++
	return nil, errors.New("unexpected post-commit detail read")
}

func (r failingFeedbackMessageRepository) GetMessage(context.Context, string, string) (*types.Message, error) {
	return nil, r.err
}

func setupFeedbackServiceTest(t *testing.T) *feedbackServiceFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:feedback-service-%d?mode=memory&cache=shared&_busy_timeout=5000", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`CREATE TABLE messages (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		request_id TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL,
		knowledge_references TEXT,
		agent_steps TEXT,
		mentioned_items TEXT,
		images TEXT,
		attachments TEXT,
		is_completed BOOLEAN NOT NULL DEFAULT 0,
		is_fallback BOOLEAN NOT NULL DEFAULT 0,
		agent_duration_ms INTEGER NOT NULL DEFAULT 0,
		rendered_content TEXT NOT NULL DEFAULT '',
		channel TEXT NOT NULL DEFAULT '',
		agent_id TEXT NOT NULL DEFAULT '',
		agent_tenant_id INTEGER NOT NULL DEFAULT 0,
		model_id TEXT NOT NULL DEFAULT '',
		execution_context TEXT,
		knowledge_id TEXT NOT NULL DEFAULT '',
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create messages table: %v", err)
	}
	if err := db.AutoMigrate(
		&types.Session{},
		&types.Chunk{},
		&types.MessageFeedback{},
		&types.MessageChunkReference{},
		&types.ChunkFeedbackWeightLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	session := &types.Session{TenantID: 1, UserID: "user-1", Title: "feedback"}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	sharedChunk := &types.Chunk{
		ID:              "shared-chunk",
		TenantID:        2,
		KnowledgeBaseID: "shared-kb",
		KnowledgeID:     "shared-knowledge",
		Content:         "shared content",
		RecallWeight:    1,
	}
	if err := db.Create(sharedChunk).Error; err != nil {
		t.Fatalf("create shared chunk: %v", err)
	}
	message := &types.Message{
		SessionID:   session.ID,
		Role:        "assistant",
		Content:     "answer",
		IsCompleted: true,
		KnowledgeReferences: types.References{
			{
				ID: sharedChunk.ID, KnowledgeID: sharedChunk.KnowledgeID,
				KnowledgeBaseID: sharedChunk.KnowledgeBaseID, Score: 0.9, MatchType: types.MatchTypeEmbedding,
			},
			{
				ID: sharedChunk.ID, KnowledgeID: sharedChunk.KnowledgeID,
				KnowledgeBaseID: sharedChunk.KnowledgeBaseID, Score: 0.8, MatchType: types.MatchTypeKeywords,
			},
			{ID: "https://example.com", KnowledgeSource: "web_search", MatchType: types.MatchTypeWebSearch},
			{ID: "history-id", MatchType: types.MatchTypeHistory},
			{
				ID: sharedChunk.ID, KnowledgeID: "forged-knowledge",
				KnowledgeBaseID: sharedChunk.KnowledgeBaseID, MatchType: types.MatchTypeEmbedding,
			},
		},
	}
	insertServiceFeedbackMessage(t, db, message)

	feedbackRepo := repository.NewFeedbackRepository(db)
	messageRepo := repository.NewMessageRepository(db)
	sessionRepo := repository.NewSessionRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	cfg := &config.Config{Feedback: types.DefaultChunkFeedbackConfig()}
	feedbackSvc := service.NewFeedbackService(feedbackRepo, sessionRepo, messageRepo, chunkRepo, cfg)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "user-1")
	return &feedbackServiceFixture{
		db:           db,
		service:      feedbackSvc,
		feedbackRepo: feedbackRepo,
		messageRepo:  messageRepo,
		sessionRepo:  sessionRepo,
		chunkRepo:    chunkRepo,
		session:      session,
		message:      message,
		sharedChunk:  sharedChunk,
		ctx:          ctx,
	}
}

func insertServiceFeedbackMessage(t *testing.T, db *gorm.DB, message *types.Message) {
	t.Helper()
	if message.ID == "" {
		message.ID = uuid.NewString()
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	message.UpdatedAt = message.CreatedAt
	references, err := json.Marshal(message.KnowledgeReferences)
	if err != nil {
		t.Fatalf("marshal references: %v", err)
	}
	if err := db.Exec(`INSERT INTO messages
		(id, session_id, content, role, knowledge_references, is_completed, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID, message.SessionID, message.Content, message.Role, references,
		message.IsCompleted, message.CreatedAt, message.UpdatedAt,
	).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func TestFeedbackServiceResetUsesTransactionalSnapshot(t *testing.T) {
	repo := &resetSnapshotFeedbackRepository{
		detail: &types.ChunkFeedbackDetail{
			ChunkFeedbackListItem: types.ChunkFeedbackListItem{ChunkID: "chunk-1"},
			Content:               strings.Repeat("x", 210),
			ReasonCounts:          []*types.ChunkFeedbackReasonCount{},
		},
	}
	svc := service.NewFeedbackService(
		repo,
		nil,
		nil,
		nil,
		&config.Config{Feedback: types.DefaultChunkFeedbackConfig()},
	)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	detail, err := svc.ResetChunkFeedback(ctx, "kb-1", "chunk-1", &types.ChunkFeedbackResetInput{})
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if detail != repo.detail || len([]rune(detail.ContentPreview)) != 200 {
		t.Fatalf("reset detail = %#v", detail)
	}
	if repo.detailReadCalls != 0 {
		t.Fatalf("service performed %d post-commit detail reads", repo.detailReadCalls)
	}
}

func TestFeedbackServicePersistsEligibleSharedChunkReferences(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	if err := f.service.PersistMessageChunkReferences(f.ctx, f.message); err != nil {
		t.Fatalf("persist references: %v", err)
	}
	refs, err := f.feedbackRepo.ListMessageChunkReferences(f.ctx, f.session.TenantID, f.message.ID)
	if err != nil {
		t.Fatalf("list references: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("references = %#v, want one deduplicated knowledge chunk", refs)
	}
	ref := refs[0]
	if ref.SessionTenantID != 1 || ref.ChunkTenantID != 2 || ref.ChunkID != f.sharedChunk.ID || ref.ReferenceRank != 0 {
		t.Fatalf("reference = %#v", ref)
	}
	if err := f.service.PersistMessageChunkReferences(f.ctx, f.message); err != nil {
		t.Fatalf("repeat persist: %v", err)
	}
	refs, _ = f.feedbackRepo.ListMessageChunkReferences(f.ctx, f.session.TenantID, f.message.ID)
	if len(refs) != 1 {
		t.Fatalf("repeat persist duplicated references: %#v", refs)
	}
}

func TestFeedbackServiceRejectsMismatchedAttributionMetadata(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	f.message.KnowledgeReferences = types.References{{
		ID:              f.sharedChunk.ID,
		KnowledgeID:     "forged-knowledge",
		KnowledgeBaseID: f.sharedChunk.KnowledgeBaseID,
		MatchType:       types.MatchTypeEmbedding,
	}}
	if err := f.service.PersistMessageChunkReferences(f.ctx, f.message); err != nil {
		t.Fatalf("persist knowledge mismatch: %v", err)
	}
	refs, err := f.feedbackRepo.ListMessageChunkReferences(f.ctx, 1, f.message.ID)
	if err != nil || len(refs) != 0 {
		t.Fatalf("knowledge mismatch references = %#v err=%v", refs, err)
	}

	f.message.KnowledgeReferences = types.References{{
		ID:              f.sharedChunk.ID,
		KnowledgeID:     f.sharedChunk.KnowledgeID,
		KnowledgeBaseID: "forged-kb",
		MatchType:       types.MatchTypeEmbedding,
	}}
	if err := f.service.PersistMessageChunkReferences(f.ctx, f.message); err != nil {
		t.Fatalf("persist KB mismatch: %v", err)
	}
	refs, err = f.feedbackRepo.ListMessageChunkReferences(f.ctx, 1, f.message.ID)
	if err != nil || len(refs) != 0 {
		t.Fatalf("KB mismatch references = %#v err=%v", refs, err)
	}
}

func TestFeedbackServiceSkipsDeletedChunkAndPersistsValidChunk(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	deleted := &types.Chunk{
		ID:              "deleted-chunk",
		TenantID:        2,
		KnowledgeBaseID: "shared-kb",
		KnowledgeID:     "deleted-knowledge",
		Content:         "deleted",
		RecallWeight:    1,
	}
	if err := f.db.Create(deleted).Error; err != nil {
		t.Fatalf("create deleted chunk: %v", err)
	}
	if err := f.db.Delete(deleted).Error; err != nil {
		t.Fatalf("soft-delete chunk: %v", err)
	}
	f.message.KnowledgeReferences = types.References{
		{
			ID: deleted.ID, KnowledgeID: deleted.KnowledgeID,
			KnowledgeBaseID: deleted.KnowledgeBaseID, MatchType: types.MatchTypeEmbedding,
		},
		{
			ID: f.sharedChunk.ID, KnowledgeID: f.sharedChunk.KnowledgeID,
			KnowledgeBaseID: f.sharedChunk.KnowledgeBaseID, MatchType: types.MatchTypeEmbedding,
		},
	}
	if err := f.service.PersistMessageChunkReferences(f.ctx, f.message); err != nil {
		t.Fatalf("persist mixed references: %v", err)
	}
	refs, err := f.feedbackRepo.ListMessageChunkReferences(f.ctx, 1, f.message.ID)
	if err != nil || len(refs) != 1 || refs[0].ChunkID != f.sharedChunk.ID {
		t.Fatalf("mixed references = %#v err=%v", refs, err)
	}
	var deletedCount int64
	if err := f.db.Model(&types.Chunk{}).Where("id = ?", deleted.ID).Count(&deletedCount).Error; err != nil {
		t.Fatalf("count deleted chunk: %v", err)
	}
	if deletedCount != 0 {
		t.Fatal("deleted chunk was revived")
	}
}

func TestFeedbackServiceLazyFallbackAndCallerIsolation(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	state, err := f.service.SetMessageFeedback(f.ctx, f.session.ID, f.message.ID, &types.MessageFeedbackInput{
		FeedbackType: types.FeedbackTypeDislike,
		ReasonCode:   types.FeedbackReasonIncorrect,
	})
	if err != nil {
		t.Fatalf("set feedback with lazy fallback: %v", err)
	}
	if state == nil || state.FeedbackType != types.FeedbackTypeDislike {
		t.Fatalf("state = %#v", state)
	}
	refs, err := f.feedbackRepo.ListMessageChunkReferences(f.ctx, 1, f.message.ID)
	if err != nil || len(refs) != 1 {
		t.Fatalf("lazy references = %#v err=%v", refs, err)
	}
	var chunk types.Chunk
	if err := f.db.Where("id = ?", f.sharedChunk.ID).First(&chunk).Error; err != nil {
		t.Fatalf("load chunk: %v", err)
	}
	if chunk.TenantID != 2 || chunk.DislikeCount != 1 || chunk.LikeCount != 0 || chunk.RecallWeight != 0.8 {
		t.Fatalf("shared chunk aggregate = %#v", chunk)
	}

	otherCtx := context.WithValue(f.ctx, types.UserIDContextKey, "user-2")
	_, err = f.service.GetMessageFeedback(otherCtx, f.session.ID, f.message.ID)
	if !errors.Is(err, types.ErrFeedbackMessageNotFound) {
		t.Fatalf("other user error = %v, want not found", err)
	}
}

func TestFeedbackServiceRechecksTenantAPIKeyKnowledgeBaseAllowList(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	if err := f.service.PersistMessageChunkReferences(f.ctx, f.message); err != nil {
		t.Fatalf("persist references: %v", err)
	}
	revokedCtx := types.WithTenantAPIKeyScope(f.ctx, types.TenantAPIKeyScope{
		KeyID:            99,
		KnowledgeBaseIDs: types.StringArray{"different-kb"},
	})
	_, err := f.service.SetMessageFeedback(
		revokedCtx, f.session.ID, f.message.ID,
		&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
	)
	if !errors.Is(err, types.ErrFeedbackUnauthorized) {
		t.Fatalf("revoked allow-list error = %v, want unauthorized", err)
	}

	var feedbackCount int64
	if err := f.db.Model(&types.MessageFeedback{}).
		Where("message_id = ?", f.message.ID).
		Count(&feedbackCount).Error; err != nil {
		t.Fatalf("count feedback: %v", err)
	}
	var chunk types.Chunk
	if err := f.db.Where("id = ?", f.sharedChunk.ID).First(&chunk).Error; err != nil {
		t.Fatalf("load chunk: %v", err)
	}
	if feedbackCount != 0 || chunk.LikeCount != 0 || chunk.DislikeCount != 0 || chunk.RecallWeight != 1 {
		t.Fatalf("revoked key mutated feedback: count=%d chunk=%#v", feedbackCount, chunk)
	}

	allowedCtx := types.WithTenantAPIKeyScope(f.ctx, types.TenantAPIKeyScope{
		KeyID:            99,
		KnowledgeBaseIDs: types.StringArray{f.sharedChunk.KnowledgeBaseID},
	})
	if _, err := f.service.SetMessageFeedback(
		allowedCtx, f.session.ID, f.message.ID,
		&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
	); err != nil {
		t.Fatalf("allowed key feedback: %v", err)
	}
}

func TestFeedbackServiceRejectsInvalidMessageAndReason(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	_, err := f.service.SetMessageFeedback(f.ctx, f.session.ID, f.message.ID, &types.MessageFeedbackInput{
		FeedbackType: types.FeedbackTypeDislike,
		ReasonCode:   "made-up",
	})
	if err == nil {
		t.Fatal("invalid reason was accepted")
	}

	incomplete := &types.Message{SessionID: f.session.ID, Role: "assistant", Content: "partial", IsCompleted: false}
	insertServiceFeedbackMessage(t, f.db, incomplete)
	_, err = f.service.SetMessageFeedback(
		f.ctx, f.session.ID, incomplete.ID,
		&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
	)
	if !errors.Is(err, types.ErrFeedbackMessageIncomplete) {
		t.Fatalf("incomplete message error = %v", err)
	}

	userMessage := &types.Message{SessionID: f.session.ID, Role: "user", Content: "question", IsCompleted: true}
	insertServiceFeedbackMessage(t, f.db, userMessage)
	_, err = f.service.SetMessageFeedback(
		f.ctx, f.session.ID, userMessage.ID,
		&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
	)
	if !errors.Is(err, types.ErrFeedbackMessageIncomplete) {
		t.Fatalf("user message error = %v", err)
	}
}

func TestFeedbackServicePreservesRepositoryFailures(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	dbErr := errors.New("database unavailable")

	t.Run("session repository", func(t *testing.T) {
		svc := service.NewFeedbackService(
			f.feedbackRepo,
			failingFeedbackSessionRepository{SessionRepository: f.sessionRepo, err: dbErr},
			f.messageRepo,
			f.chunkRepo,
			&config.Config{Feedback: types.DefaultChunkFeedbackConfig()},
		)
		_, err := svc.SetMessageFeedback(
			f.ctx, f.session.ID, f.message.ID,
			&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
		)
		if !errors.Is(err, dbErr) || errors.Is(err, types.ErrFeedbackMessageNotFound) {
			t.Fatalf("session repository error = %v", err)
		}
	})

	t.Run("message repository", func(t *testing.T) {
		svc := service.NewFeedbackService(
			f.feedbackRepo,
			f.sessionRepo,
			failingFeedbackMessageRepository{MessageRepository: f.messageRepo, err: dbErr},
			f.chunkRepo,
			&config.Config{Feedback: types.DefaultChunkFeedbackConfig()},
		)
		_, err := svc.SetMessageFeedback(
			f.ctx, f.session.ID, f.message.ID,
			&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
		)
		if !errors.Is(err, dbErr) || errors.Is(err, types.ErrFeedbackMessageNotFound) {
			t.Fatalf("message repository error = %v", err)
		}
	})
}

func TestFeedbackServicePrincipalIsolationAndLongExternalUserID(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	legacySession := &types.Session{TenantID: 1, Title: "legacy tenant session"}
	if err := f.db.Create(legacySession).Error; err != nil {
		t.Fatalf("create legacy session: %v", err)
	}
	legacyMessage := &types.Message{
		SessionID:   legacySession.ID,
		Role:        "assistant",
		Content:     "legacy answer",
		IsCompleted: true,
		KnowledgeReferences: types.References{{
			ID: f.sharedChunk.ID, KnowledgeID: f.sharedChunk.KnowledgeID,
			KnowledgeBaseID: f.sharedChunk.KnowledgeBaseID, MatchType: types.MatchTypeEmbedding,
		}},
	}
	insertServiceFeedbackMessage(t, f.db, legacyMessage)

	keyContext := func(keyID uint64) context.Context {
		ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "1"})
		return types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: keyID})
	}
	if _, err := f.service.SetMessageFeedback(
		keyContext(101), legacySession.ID, legacyMessage.ID,
		&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
	); err != nil {
		t.Fatalf("first tenant API key feedback: %v", err)
	}
	if _, err := f.service.SetMessageFeedback(
		keyContext(202), legacySession.ID, legacyMessage.ID,
		&types.MessageFeedbackInput{
			FeedbackType: types.FeedbackTypeDislike,
			ReasonCode:   types.FeedbackReasonIncorrect,
		},
	); err != nil {
		t.Fatalf("second tenant API key feedback: %v", err)
	}
	var keyFeedbacks []*types.MessageFeedback
	if err := f.db.Where("message_id = ?", legacyMessage.ID).
		Order("user_id").Find(&keyFeedbacks).Error; err != nil {
		t.Fatalf("load API key feedbacks: %v", err)
	}
	if len(keyFeedbacks) != 2 || keyFeedbacks[0].UserID == keyFeedbacks[1].UserID {
		t.Fatalf("tenant API key feedbacks = %#v", keyFeedbacks)
	}

	externalPrincipal := types.Principal{
		Type: types.PrincipalAPIExternalUser,
		ID:   "1:" + strings.Repeat("x", 128),
	}
	externalSession := &types.Session{TenantID: 1, UserID: externalPrincipal.StorageID(), Title: "external"}
	if err := f.db.Create(externalSession).Error; err != nil {
		t.Fatalf("create external session: %v", err)
	}
	externalMessage := &types.Message{
		SessionID: externalSession.ID, Role: "assistant", Content: "external answer", IsCompleted: true,
	}
	insertServiceFeedbackMessage(t, f.db, externalMessage)
	externalCtx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	externalCtx = types.WithPrincipal(externalCtx, externalPrincipal)
	if _, err := f.service.SetMessageFeedback(
		externalCtx, externalSession.ID, externalMessage.ID,
		&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
	); err != nil {
		t.Fatalf("long external principal feedback: %v", err)
	}
	var externalFeedback types.MessageFeedback
	if err := f.db.Where("message_id = ?", externalMessage.ID).First(&externalFeedback).Error; err != nil {
		t.Fatalf("load external feedback: %v", err)
	}
	if externalFeedback.UserID != externalPrincipal.StorageID() {
		t.Fatalf("external feedback user_id = %q, want %q", externalFeedback.UserID, externalPrincipal.StorageID())
	}
}

func TestMessageServiceRestoresFeedbackStateInOneHistoryRead(t *testing.T) {
	f := setupFeedbackServiceTest(t)
	if _, err := f.service.SetMessageFeedback(
		f.ctx, f.session.ID, f.message.ID,
		&types.MessageFeedbackInput{FeedbackType: types.FeedbackTypeLike},
	); err != nil {
		t.Fatalf("set feedback: %v", err)
	}
	messageService := service.NewMessageService(
		f.messageRepo, f.sessionRepo, nil, nil, nil, nil, nil, f.feedbackRepo,
	)
	messages, err := messageService.GetMessagesBySession(f.ctx, f.session.ID, 1, 20)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(messages) != 1 || messages[0].Feedback == nil ||
		messages[0].Feedback.FeedbackType != types.FeedbackTypeLike {
		t.Fatalf("history feedback state = %#v", messages)
	}
	message, err := messageService.GetMessage(f.ctx, f.session.ID, f.message.ID)
	if err != nil || message.Feedback == nil || message.Feedback.FeedbackType != types.FeedbackTypeLike {
		t.Fatalf("single message feedback state = %#v err=%v", message, err)
	}
	recent, err := messageService.GetRecentMessagesBySession(f.ctx, f.session.ID, 10)
	if err != nil || len(recent) != 1 || recent[0].Feedback == nil ||
		recent[0].Feedback.FeedbackType != types.FeedbackTypeLike {
		t.Fatalf("recent feedback state = %#v err=%v", recent, err)
	}
	before, err := messageService.GetMessagesBySessionBeforeTime(
		f.ctx, f.session.ID, f.message.CreatedAt.Add(time.Second), 10,
	)
	if err != nil || len(before) != 1 || before[0].Feedback == nil ||
		before[0].Feedback.FeedbackType != types.FeedbackTypeLike {
		t.Fatalf("before-time feedback state = %#v err=%v", before, err)
	}
}
