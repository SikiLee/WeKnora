package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type feedbackRepositoryStub struct {
	interfaces.FeedbackRepository
	input *types.ApplyMessageFeedbackInput
}

func (s *feedbackRepositoryStub) ApplyMessageFeedback(
	_ context.Context,
	input types.ApplyMessageFeedbackInput,
) (*types.MessageFeedbackState, error) {
	s.input = &input
	return &types.MessageFeedbackState{Type: input.Type, ReasonCode: input.ReasonCode}, nil
}

func feedbackServiceContext(principal types.Principal) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	return types.WithPrincipal(ctx, principal)
}

func TestFeedbackServiceRejectsNonWebPrincipals(t *testing.T) {
	for _, principalType := range []string{
		types.PrincipalAPITenant,
		types.PrincipalAPIPlatform,
		types.PrincipalAPIExternalUser,
		types.PrincipalIMUser,
		types.PrincipalEmbedVisitor,
	} {
		t.Run(principalType, func(t *testing.T) {
			repo := &feedbackRepositoryStub{}
			svc := NewFeedbackService(repo)
			_, err := svc.ApplyMessageFeedback(
				feedbackServiceContext(types.Principal{Type: principalType, ID: "caller"}),
				"session", "message", types.FeedbackTypeLike, nil,
			)
			assert.ErrorIs(t, err, ErrFeedbackForbidden)
			assert.Nil(t, repo.input)
		})
	}
}

func TestFeedbackServiceUsesAuthenticatedWebActor(t *testing.T) {
	repo := &feedbackRepositoryStub{}
	svc := NewFeedbackService(repo)
	state, err := svc.ApplyMessageFeedback(
		feedbackServiceContext(types.Principal{Type: types.PrincipalWebUser, ID: "user-1"}),
		"session-1", "message-1", types.FeedbackTypeDislike,
		func() *types.FeedbackReasonCode {
			reason := types.FeedbackReasonInaccurate
			return &reason
		}(),
	)
	require.NoError(t, err)
	assert.Equal(t, types.FeedbackTypeDislike, state.Type)
	require.NotNil(t, repo.input)
	assert.Equal(t, uint64(7), repo.input.MessageTenantID)
	assert.Equal(t, "user-1", repo.input.ActorUserID)
	assert.Equal(t, "session-1", repo.input.SessionID)
	assert.Equal(t, "message-1", repo.input.MessageID)
}

func TestFeedbackServiceRejectsInvalidReasonCombination(t *testing.T) {
	repo := &feedbackRepositoryStub{}
	svc := NewFeedbackService(repo)
	reason := types.FeedbackReasonInaccurate
	_, err := svc.ApplyMessageFeedback(
		feedbackServiceContext(types.Principal{Type: types.PrincipalWebUser, ID: "user-1"}),
		"session", "message", types.FeedbackTypeLike, &reason,
	)
	assert.ErrorIs(t, err, ErrInvalidFeedback)
	assert.Nil(t, repo.input)
}
