package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

func RegisterFeedbackRoutes(r *gin.RouterGroup, handler *handler.FeedbackHandler, g *rbacGuards) {
	if handler == nil {
		return
	}
	// Policies are declared so the global API-key gate has a complete route
	// map; the service still rejects all non-web-user principals.
	sessions := g.apiKeyGroup(r.Group("/sessions"), apiKeyFullAccess())
	sessions.PUT("/:session_id/messages/:message_id/feedback", g.Viewer(), handler.PutMessageFeedback)

	chunkRead := g.apiKeyGroup(r.Group("/chunks"), apiKeyFullAccess())
	chunkRead.GET("/by-id/:id/feedback", g.Viewer(), g.KBAccessReadFromChunkIDParam("id"), handler.GetChunkFeedbackDetails)

	kb := g.apiKeyGroup(r.Group("/knowledge-bases"), apiKeyFullAccess())
	kb.POST("/:id/chunks/:chunk_id/feedback/reset",
		g.OwnedKBOrAdmin(), g.KBAccessWrite("id"), handler.ResetChunkFeedback)
}
