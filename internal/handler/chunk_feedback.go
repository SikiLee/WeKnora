package handler

import (
	stderrors "errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ChunkFeedbackHandler exposes the KB-scoped quality governance surface.
type ChunkFeedbackHandler struct {
	service interfaces.FeedbackService
}

// NewChunkFeedbackHandler creates the KB-scoped feedback governance handler.
func NewChunkFeedbackHandler(service interfaces.FeedbackService) *ChunkFeedbackHandler {
	return &ChunkFeedbackHandler{service: service}
}

// List godoc
// @Summary List chunk feedback quality
// @Tags Chunk Feedback
// @Produce json
// @Param id path string true "Knowledge base ID"
// @Param page query int false "Page"
// @Param page_size query int false "Page size (max 100)"
// @Param keyword query string false "Chunk content or knowledge title"
// @Param feedback_status query string false "all, rated, high, normal, low, or unrated"
// @Param needs_optimization query bool false "Optimization flag"
// @Param sort_by query string false "Governance sort field"
// @Param sort_order query string false "asc or desc"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Router /knowledge-bases/{id}/chunk-feedback [get]
func (h *ChunkFeedbackHandler) List(c *gin.Context) {
	var query types.ChunkFeedbackListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		_ = c.Error(apperrors.NewBadRequestError("invalid chunk feedback query").WithDetails(err.Error()))
		return
	}
	if err := query.Validate(); err != nil {
		_ = c.Error(apperrors.NewValidationError(err.Error()))
		return
	}
	result, err := h.service.ListChunkFeedback(c.Request.Context(), chunkFeedbackKBID(c), &query)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// Detail godoc
// @Summary Get chunk feedback detail
// @Tags Chunk Feedback
// @Produce json
// @Param id path string true "Knowledge base ID"
// @Param chunk_id path string true "Chunk ID"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Router /knowledge-bases/{id}/chunk-feedback/{chunk_id} [get]
func (h *ChunkFeedbackHandler) Detail(c *gin.Context) {
	detail, err := h.service.GetChunkFeedbackDetail(
		c.Request.Context(), chunkFeedbackKBID(c), c.Param("chunk_id"),
	)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
}

// WeightLogs godoc
// @Summary List chunk feedback weight logs
// @Tags Chunk Feedback
// @Produce json
// @Param id path string true "Knowledge base ID"
// @Param chunk_id path string true "Chunk ID"
// @Param page query int false "Page"
// @Param page_size query int false "Page size (max 100)"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Router /knowledge-bases/{id}/chunk-feedback/{chunk_id}/weight-logs [get]
func (h *ChunkFeedbackHandler) WeightLogs(c *gin.Context) {
	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}
	if err := types.ValidateChunkFeedbackPagination(page, pageSize); err != nil {
		_ = c.Error(apperrors.NewValidationError(err.Error()))
		return
	}
	result, err := h.service.ListChunkFeedbackWeightLogs(
		c.Request.Context(), chunkFeedbackKBID(c), c.Param("chunk_id"),
		&types.Pagination{Page: page, PageSize: pageSize},
	)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// Reset godoc
// @Summary Reset chunk feedback aggregates
// @Tags Chunk Feedback
// @Accept json
// @Produce json
// @Param id path string true "Knowledge base ID"
// @Param chunk_id path string true "Chunk ID"
// @Param request body types.ChunkFeedbackResetInput false "Reset reason"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Router /knowledge-bases/{id}/chunk-feedback/{chunk_id}/reset [post]
func (h *ChunkFeedbackHandler) Reset(c *gin.Context) {
	input := &types.ChunkFeedbackResetInput{}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	if err := c.ShouldBindJSON(input); err != nil && !stderrors.Is(err, io.EOF) {
		var maxBytesError *http.MaxBytesError
		if stderrors.As(err, &maxBytesError) {
			_ = c.Error(apperrors.NewBadRequestError("chunk feedback reset request is too large"))
			return
		}
		_ = c.Error(apperrors.NewBadRequestError("invalid chunk feedback reset request").WithDetails(err.Error()))
		return
	}
	if err := input.Validate(); err != nil {
		_ = c.Error(apperrors.NewValidationError(err.Error()))
		return
	}
	detail, err := h.service.ResetChunkFeedback(
		c.Request.Context(), chunkFeedbackKBID(c), c.Param("chunk_id"), input,
	)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
}

func chunkFeedbackKBID(c *gin.Context) string {
	if id := c.Param("id"); id != "" {
		return id
	}
	return c.Param("kb_id")
}

func (h *ChunkFeedbackHandler) handleError(c *gin.Context, err error) {
	switch {
	case stderrors.Is(err, types.ErrChunkFeedbackNotFound):
		_ = c.Error(apperrors.NewNotFoundError("chunk not found"))
	case stderrors.Is(err, types.ErrFeedbackUnauthorized):
		_ = c.Error(apperrors.NewForbiddenError("feedback governance is not authorized"))
	default:
		logger.ErrorWithFields(c.Request.Context(), err, nil)
		_ = c.Error(apperrors.NewInternalServerError("chunk feedback operation failed"))
	}
}
