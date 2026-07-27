// Package router tests feedback route registration and API-key policy coverage.
package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

type chunkFeedbackRouteKBLookup struct {
	kb *types.KnowledgeBase
}

func (s *chunkFeedbackRouteKBLookup) GetKnowledgeBaseByID(
	context.Context,
	string,
) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type chunkFeedbackRouteService struct {
	resetCalls int
}

func (s *chunkFeedbackRouteService) CompleteAssistantMessage(context.Context, *types.Message) error {
	return nil
}

func (s *chunkFeedbackRouteService) PersistMessageChunkReferences(context.Context, *types.Message) error {
	return nil
}

func (s *chunkFeedbackRouteService) SetMessageFeedback(
	context.Context,
	string,
	string,
	*types.MessageFeedbackInput,
) (*types.MessageFeedbackState, error) {
	return nil, nil
}

func (s *chunkFeedbackRouteService) GetMessageFeedback(
	context.Context,
	string,
	string,
) (*types.MessageFeedbackState, error) {
	return nil, nil
}

func (s *chunkFeedbackRouteService) ListChunkFeedback(
	context.Context,
	string,
	*types.ChunkFeedbackListQuery,
) (*types.PageResult, error) {
	return nil, nil
}

func (s *chunkFeedbackRouteService) GetChunkFeedbackDetail(
	context.Context,
	string,
	string,
) (*types.ChunkFeedbackDetail, error) {
	return nil, nil
}

func (s *chunkFeedbackRouteService) ListChunkFeedbackWeightLogs(
	context.Context,
	string,
	string,
	*types.Pagination,
) (*types.PageResult, error) {
	return nil, nil
}

func (s *chunkFeedbackRouteService) ResetChunkFeedback(
	_ context.Context,
	_,
	chunkID string,
	_ *types.ChunkFeedbackResetInput,
) (*types.ChunkFeedbackDetail, error) {
	s.resetCalls++
	return &types.ChunkFeedbackDetail{ChunkFeedbackListItem: types.ChunkFeedbackListItem{
		ChunkID: chunkID,
	}}, nil
}

func TestChunkFeedbackRoutesStartWithoutWildcardConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	g := &rbacGuards{}

	RegisterKnowledgeBaseRoutes(v1, &handler.KnowledgeBaseHandler{}, g)
	RegisterChunkFeedbackRoutes(v1, handler.NewChunkFeedbackHandler(nil), g)
	g.assertAPIKeyPoliciesMatchRoutes(engine)

	want := map[string]bool{
		http.MethodGet + " /api/v1/knowledge-bases/:id/chunk-feedback":                       false,
		http.MethodGet + " /api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id":             false,
		http.MethodGet + " /api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id/weight-logs": false,
		http.MethodPost + " /api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id/reset":      false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("route not registered: %s", route)
		}
	}
}

func TestChunkFeedbackRoutesDenyAPIKeysByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	g := &rbacGuards{apiKeyAuthorizer: middleware.NewAPIKeyRouteAuthorizer()}
	v1 := engine.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(types.WithTenantAPIKeyScope(
			c.Request.Context(), types.TenantAPIKeyScope{KeyID: 7, FullAccess: true},
		))
		c.Next()
	})
	v1.Use(g.apiKeyAuthorizer.Middleware())
	RegisterChunkFeedbackRoutes(v1, handler.NewChunkFeedbackHandler(nil), g)

	cases := []struct {
		method string
		tpl    string
		path   string
	}{
		{
			http.MethodGet,
			"/api/v1/knowledge-bases/:id/chunk-feedback",
			"/api/v1/knowledge-bases/kb-1/chunk-feedback",
		},
		{
			http.MethodGet,
			"/api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id",
			"/api/v1/knowledge-bases/kb-1/chunk-feedback/chunk-1",
		},
		{
			http.MethodGet,
			"/api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id/weight-logs",
			"/api/v1/knowledge-bases/kb-1/chunk-feedback/chunk-1/weight-logs",
		},
		{
			http.MethodPost,
			"/api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id/reset",
			"/api/v1/knowledge-bases/kb-1/chunk-feedback/chunk-1/reset",
		},
	}
	for _, tc := range cases {
		if _, ok := g.ensureAPIKeyAuthorizer().Lookup(tc.method, tc.tpl); ok {
			t.Fatalf("governance route %s %s unexpectedly declared an API-key policy", tc.method, tc.tpl)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("API key reached governance route %s %s: status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestChunkFeedbackResetRouteAllowsOnlyCreatorOrVerifiedAdmin(t *testing.T) {
	enabled := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
	tests := []struct {
		name       string
		userID     string
		role       types.TenantRole
		verified   bool
		wantStatus int
		wantCalls  int
	}{
		{
			name: "creator", userID: "creator", role: types.TenantRoleContributor,
			wantStatus: http.StatusOK, wantCalls: 1,
		},
		{
			name: "verified admin", userID: "admin", role: types.TenantRoleAdmin, verified: true,
			wantStatus: http.StatusOK, wantCalls: 1,
		},
		{
			name: "non-creator contributor", userID: "contributor", role: types.TenantRoleContributor,
			verified: true, wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			engine.Use(middleware.ErrorHandler())
			engine.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
				ctx = context.WithValue(ctx, types.UserIDContextKey, tc.userID)
				ctx = context.WithValue(ctx, types.TenantRoleContextKey, tc.role)
				ctx = context.WithValue(ctx, types.TenantRoleVerifiedContextKey, tc.verified)
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			service := &chunkFeedbackRouteService{}
			guards := &rbacGuards{
				cfg: cfg,
				kbService: &chunkFeedbackRouteKBLookup{kb: &types.KnowledgeBase{
					ID: "kb-1", TenantID: 1, CreatorID: "creator",
				}},
			}
			v1 := engine.Group("/api/v1")
			RegisterChunkFeedbackRoutes(v1, handler.NewChunkFeedbackHandler(service), guards)

			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/knowledge-bases/kb-1/chunk-feedback/chunk-1/reset",
				strings.NewReader(`{}`),
			)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if service.resetCalls != tc.wantCalls {
				t.Fatalf("reset calls=%d want=%d", service.resetCalls, tc.wantCalls)
			}
		})
	}
}
