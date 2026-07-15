package handler

import (
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// SetFeedback records or clears the caller's rating for a completed answer.
func (h *MessageHandler) SetFeedback(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("session_id"))
	messageID := strings.TrimSpace(c.Param("message_id"))
	if sessionID == "" || messageID == "" {
		_ = c.Error(apperrors.NewBadRequestError("session_id and message_id are required"))
		return
	}

	input := &types.MessageFeedbackInput{}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		var maxBytesError *http.MaxBytesError
		if stderrors.As(err, &maxBytesError) {
			_ = c.Error(apperrors.NewBadRequestError("message feedback request is too large"))
			return
		}
		_ = c.Error(apperrors.NewBadRequestError("invalid message feedback request").WithDetails(err.Error()))
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		_ = c.Error(apperrors.NewBadRequestError("invalid message feedback request").WithDetails(err.Error()))
		return
	}
	if err := input.Validate(); err != nil {
		_ = c.Error(apperrors.NewValidationError(err.Error()))
		return
	}

	state, err := h.FeedbackService.SetMessageFeedback(
		c.Request.Context(), sessionID, messageID, input,
	)
	if err != nil {
		h.handleMessageFeedbackError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": state})
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if stderrors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return stderrors.New("request body must contain exactly one JSON object")
	}
	return err
}

func (h *MessageHandler) handleMessageFeedbackError(c *gin.Context, err error) {
	switch {
	case stderrors.Is(err, types.ErrFeedbackUnauthorized):
		_ = c.Error(apperrors.NewForbiddenError("message feedback is not authorized"))
	case stderrors.Is(err, types.ErrFeedbackMessageNotFound):
		_ = c.Error(apperrors.NewNotFoundError("message not found"))
	case stderrors.Is(err, types.ErrFeedbackMessageIncomplete):
		_ = c.Error(apperrors.NewConflictError("only completed assistant messages can be rated"))
	default:
		logger.ErrorWithFields(c.Request.Context(), err, nil)
		_ = c.Error(apperrors.NewInternalServerError("message feedback operation failed"))
	}
}
