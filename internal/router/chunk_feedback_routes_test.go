package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

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
		{http.MethodGet, "/api/v1/knowledge-bases/:id/chunk-feedback", "/api/v1/knowledge-bases/kb-1/chunk-feedback"},
		{http.MethodGet, "/api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id", "/api/v1/knowledge-bases/kb-1/chunk-feedback/chunk-1"},
		{http.MethodGet, "/api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id/weight-logs", "/api/v1/knowledge-bases/kb-1/chunk-feedback/chunk-1/weight-logs"},
		{http.MethodPost, "/api/v1/knowledge-bases/:id/chunk-feedback/:chunk_id/reset", "/api/v1/knowledge-bases/kb-1/chunk-feedback/chunk-1/reset"},
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
