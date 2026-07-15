package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type messageFeedbackServiceStub struct {
	interfaces.FeedbackService
	setFn func(context.Context, string, string, *types.MessageFeedbackInput) (*types.MessageFeedbackState, error)
}

func (s *messageFeedbackServiceStub) SetMessageFeedback(
	ctx context.Context,
	sessionID string,
	messageID string,
	input *types.MessageFeedbackInput,
) (*types.MessageFeedbackState, error) {
	return s.setFn(ctx, sessionID, messageID, input)
}

func messageFeedbackHandlerRouter(service interfaces.FeedbackService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &MessageHandler{FeedbackService: service}
	r.PUT("/messages/:session_id/:message_id/feedback", h.SetFeedback)
	return r
}

func TestMessageFeedbackHandlerSetsFeedback(t *testing.T) {
	feedbackAt := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	service := &messageFeedbackServiceStub{
		setFn: func(_ context.Context, sessionID, messageID string, input *types.MessageFeedbackInput) (*types.MessageFeedbackState, error) {
			if sessionID != "session-1" || messageID != "message-1" {
				t.Fatalf("unexpected target: %s/%s", sessionID, messageID)
			}
			if input.FeedbackType != types.FeedbackTypeDislike || input.ReasonCode != types.FeedbackReasonOther || input.ReasonText != "Missing a step" {
				t.Fatalf("unexpected input: %#v", input)
			}
			return &types.MessageFeedbackState{
				FeedbackType: input.FeedbackType,
				ReasonCode:   input.ReasonCode,
				ReasonText:   input.ReasonText,
				FeedbackAt:   feedbackAt,
			}, nil
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPut,
		"/messages/session-1/message-1/feedback",
		strings.NewReader(`{"feedback_type":"dislike","reason_code":"other","reason_text":" Missing a step "}`),
	)
	req.Header.Set("Content-Type", "application/json")
	messageFeedbackHandlerRouter(service).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Success bool                       `json:"success"`
		Data    types.MessageFeedbackState `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Data.FeedbackType != types.FeedbackTypeDislike || !response.Data.FeedbackAt.Equal(feedbackAt) {
		t.Fatalf("response=%#v", response)
	}
}

func TestMessageFeedbackHandlerRejectsInvalidBodies(t *testing.T) {
	called := false
	service := &messageFeedbackServiceStub{
		setFn: func(context.Context, string, string, *types.MessageFeedbackInput) (*types.MessageFeedbackState, error) {
			called = true
			return nil, nil
		},
	}
	tests := []string{
		``,
		`{"feedback_type":"like","unexpected":true}`,
		`{"feedback_type":"like"}{"feedback_type":"none"}`,
		`{"feedback_type":"dislike","reason_code":"unknown"}`,
		`{"feedback_type":"like","reason_code":"incorrect"}`,
	}
	for _, body := range tests {
		called = false
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/messages/session-1/message-1/feedback", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		messageFeedbackHandlerRouter(service).ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest || called {
			t.Fatalf("body=%q status=%d called=%v response=%s", body, w.Code, called, w.Body.String())
		}
	}
}

func TestMessageFeedbackHandlerRejectsOversizedBodyBeforeServiceCall(t *testing.T) {
	called := false
	service := &messageFeedbackServiceStub{
		setFn: func(context.Context, string, string, *types.MessageFeedbackInput) (*types.MessageFeedbackState, error) {
			called = true
			return nil, nil
		},
	}
	body := `{"feedback_type":"dislike","reason_code":"other","reason_text":"` + strings.Repeat("x", 9<<10) + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/messages/session-1/message-1/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	messageFeedbackHandlerRouter(service).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || called {
		t.Fatalf("status=%d called=%v body=%s", w.Code, called, w.Body.String())
	}
}

func TestMessageFeedbackHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "unauthorized", err: types.ErrFeedbackUnauthorized, status: http.StatusForbidden},
		{name: "not found", err: types.ErrFeedbackMessageNotFound, status: http.StatusNotFound},
		{name: "incomplete", err: types.ErrFeedbackMessageIncomplete, status: http.StatusConflict},
		{name: "internal", err: errors.New("database unavailable"), status: http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := &messageFeedbackServiceStub{
				setFn: func(context.Context, string, string, *types.MessageFeedbackInput) (*types.MessageFeedbackState, error) {
					return nil, tc.err
				},
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodPut,
				"/messages/session-1/message-1/feedback",
				strings.NewReader(`{"feedback_type":"like"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			messageFeedbackHandlerRouter(service).ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func TestMessageFeedbackHandlerReturnsNullWhenFeedbackCleared(t *testing.T) {
	service := &messageFeedbackServiceStub{
		setFn: func(context.Context, string, string, *types.MessageFeedbackInput) (*types.MessageFeedbackState, error) {
			return nil, nil
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPut,
		"/messages/session-1/message-1/feedback",
		strings.NewReader(`{"feedback_type":"none"}`),
	)
	messageFeedbackHandlerRouter(service).ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"data":null`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
