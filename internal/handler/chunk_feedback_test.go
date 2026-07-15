package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type chunkFeedbackServiceStub struct {
	interfaces.FeedbackService
	listFn   func(context.Context, string, *types.ChunkFeedbackListQuery) (*types.PageResult, error)
	detailFn func(context.Context, string, string) (*types.ChunkFeedbackDetail, error)
	logsFn   func(context.Context, string, string, *types.Pagination) (*types.PageResult, error)
	resetFn  func(context.Context, string, string, *types.ChunkFeedbackResetInput) (*types.ChunkFeedbackDetail, error)
}

func (s *chunkFeedbackServiceStub) ListChunkFeedback(ctx context.Context, kbID string, query *types.ChunkFeedbackListQuery) (*types.PageResult, error) {
	return s.listFn(ctx, kbID, query)
}

func (s *chunkFeedbackServiceStub) GetChunkFeedbackDetail(ctx context.Context, kbID, chunkID string) (*types.ChunkFeedbackDetail, error) {
	return s.detailFn(ctx, kbID, chunkID)
}

func (s *chunkFeedbackServiceStub) ListChunkFeedbackWeightLogs(ctx context.Context, kbID, chunkID string, page *types.Pagination) (*types.PageResult, error) {
	return s.logsFn(ctx, kbID, chunkID, page)
}

func (s *chunkFeedbackServiceStub) ResetChunkFeedback(ctx context.Context, kbID, chunkID string, input *types.ChunkFeedbackResetInput) (*types.ChunkFeedbackDetail, error) {
	return s.resetFn(ctx, kbID, chunkID, input)
}

func chunkFeedbackHandlerRouter(service interfaces.FeedbackService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := NewChunkFeedbackHandler(service)
	r.GET("/knowledge-bases/:kb_id/chunk-feedback", h.List)
	r.GET("/knowledge-bases/:kb_id/chunk-feedback/:chunk_id/weight-logs", h.WeightLogs)
	r.GET("/knowledge-bases/:kb_id/chunk-feedback/:chunk_id", h.Detail)
	r.POST("/knowledge-bases/:kb_id/chunk-feedback/:chunk_id/reset", h.Reset)
	return r
}

func TestChunkFeedbackHandlerListValidatesAndReturnsPage(t *testing.T) {
	called := false
	service := &chunkFeedbackServiceStub{
		listFn: func(_ context.Context, kbID string, query *types.ChunkFeedbackListQuery) (*types.PageResult, error) {
			called = true
			if kbID != "kb-1" || query.Page != 2 || query.PageSize != 5 ||
				query.FeedbackStatus != types.ChunkFeedbackStatusLow || query.SortBy != "positive_rate" || query.SortOrder != "asc" {
				t.Fatalf("unexpected query: kb=%s query=%#v", kbID, query)
			}
			return types.NewPageResult(1, query.Pagination(), []*types.ChunkFeedbackListItem{{ChunkID: "chunk-1"}}), nil
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases/kb-1/chunk-feedback?page=2&page_size=5&feedback_status=low&sort_by=positive_rate&sort_order=asc", nil)
	chunkFeedbackHandlerRouter(service).ServeHTTP(w, req)
	if !called || w.Code != http.StatusOK {
		t.Fatalf("called=%v status=%d body=%s", called, w.Code, w.Body.String())
	}
	var response struct {
		Success bool             `json:"success"`
		Data    types.PageResult `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Data.Total != 1 || response.Data.Page != 2 || response.Data.PageSize != 5 {
		t.Fatalf("response = %#v", response)
	}
}

func TestChunkFeedbackHandlerRejectsInvalidListAndLogPagination(t *testing.T) {
	service := &chunkFeedbackServiceStub{}
	tests := []string{
		"/knowledge-bases/kb-1/chunk-feedback?sort_by=content%3BDROP%20TABLE%20chunks",
		"/knowledge-bases/kb-1/chunk-feedback?page=9223372036854775807&page_size=100",
		"/knowledge-bases/kb-1/chunk-feedback/chunk-1/weight-logs?page_size=101",
		"/knowledge-bases/kb-1/chunk-feedback/chunk-1/weight-logs?page=9223372036854775807&page_size=100",
	}
	for _, path := range tests {
		w := httptest.NewRecorder()
		chunkFeedbackHandlerRouter(service).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("path=%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestChunkFeedbackHandlerMapsNotFoundAndAcceptsEmptyResetBody(t *testing.T) {
	service := &chunkFeedbackServiceStub{
		detailFn: func(context.Context, string, string) (*types.ChunkFeedbackDetail, error) {
			return nil, types.ErrChunkFeedbackNotFound
		},
		resetFn: func(_ context.Context, kbID, chunkID string, input *types.ChunkFeedbackResetInput) (*types.ChunkFeedbackDetail, error) {
			if kbID != "kb-1" || chunkID != "chunk-1" || input.Reason != "" {
				t.Fatalf("unexpected reset: kb=%s chunk=%s input=%#v", kbID, chunkID, input)
			}
			return &types.ChunkFeedbackDetail{ChunkFeedbackListItem: types.ChunkFeedbackListItem{ChunkID: chunkID}}, nil
		},
	}
	router := chunkFeedbackHandlerRouter(service)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/knowledge-bases/kb-1/chunk-feedback/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("detail status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/kb-1/chunk-feedback/chunk-1/reset", strings.NewReader(""))
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", w.Code, w.Body.String())
	}
}
