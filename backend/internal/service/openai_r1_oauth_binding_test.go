package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestR1OAuthSessionRejectsRevokedOrChangedBinding(t *testing.T) {
	for _, kind := range []string{"deleted", "changed_route", "wrong_record"} {
		t.Run(kind, func(t *testing.T) {
			id := int64(1)
			repo := &gapProxyRepository{result: gapValidProxy()}
			client := &gapV2OAuthClient{}
			s := NewOpenAIOAuthService(repo, client)
			defer s.Stop()
			out, err := s.GenerateAuthURL(context.Background(), &id, "", "")
			require.NoError(t, err)
			session, ok := s.sessionStore.Get(out.SessionID)
			require.True(t, ok)
			switch kind {
			case "deleted":
				repo.result = nil
			case "changed_route":
				repo.result.Port++
			case "wrong_record":
				repo.result.ID = 2
			}
			_, err = s.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: out.SessionID, State: session.State, Code: "synthetic-code"})
			require.Error(t, err)
			require.Zero(t, client.exchangeCalls, "revocation must not reuse the saved URL")
			_, ok = s.sessionStore.Get(out.SessionID)
			require.True(t, ok, "validation failure must not consume the session")
		})
	}
}

func TestR1OAuthSessionBindingCopiesCallerID(t *testing.T) {
	id := int64(1)
	repo := &gapProxyRepository{result: gapValidProxy()}
	client := &gapV2OAuthClient{}
	s := NewOpenAIOAuthService(repo, client)
	defer s.Stop()
	out, err := s.GenerateAuthURL(context.Background(), &id, "", "")
	require.NoError(t, err)
	session, ok := s.sessionStore.Get(out.SessionID)
	require.True(t, ok)
	id = 2
	_, err = s.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: out.SessionID, State: session.State, Code: "synthetic-code"})
	require.NoError(t, err)
	require.Equal(t, 1, client.exchangeCalls)
	require.Equal(t, 2, repo.calls, "authorization and exchange both verify the original ID")
}
