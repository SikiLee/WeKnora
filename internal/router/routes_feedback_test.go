package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

func TestFeedbackRoutesRequireFullAccessAPIKeyPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g := &rbacGuards{}
	v1 := gin.New().Group("/api/v1")
	RegisterFeedbackRoutes(v1, &handler.FeedbackHandler{}, g)

	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodPut, "/api/v1/sessions/:session_id/messages/:message_id/feedback"},
		{http.MethodGet, "/api/v1/chunks/by-id/:id/feedback"},
		{http.MethodPost, "/api/v1/knowledge-bases/:id/chunks/:chunk_id/feedback/reset"},
	} {
		policy := mustLookupAPIKeyPolicy(t, g, route.method, route.path)
		if !policy.RequireFullAccess {
			t.Fatalf("%s %s must require full API-key access", route.method, route.path)
		}
	}
}
