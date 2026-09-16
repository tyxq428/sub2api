package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Tokens and PKCE material here are synthetic. Both hosts are loopback, and
// observing the second request is sufficient; credential bodies are not logged.
func TestR1OAuthTokenClientRefusesRedirects(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, method := range []string{"exchange", "refresh"} {
			t.Run(http.StatusText(status)+"/"+method, func(t *testing.T) {
				var redirected atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					redirected.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"access_token":"synthetic-access","expires_in":3600}`))
				}))
				defer target.Close()
				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Location", target.URL)
					w.WriteHeader(status)
				}))
				defer source.Close()
				client := &openaiOAuthService{tokenURL: source.URL}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				var err error
				if method == "exchange" {
					_, err = client.ExchangeCode(ctx, "synthetic-code", "synthetic-verifier", "http://localhost/callback", "", "synthetic-client")
				} else {
					_, err = client.RefreshTokenWithClientID(ctx, "synthetic-refresh", "", "synthetic-client")
				}
				require.Zero(t, redirected.Load(), "OAuth token client must not replay credentialed form data or follow arbitrary redirects")
				require.Error(t, err)
			})
		}
	}
}

func TestR1OAuthTokenRedirectPolicyKeepsNormalSuccess(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"synthetic-access","expires_in":3600}`))
	}))
	defer target.Close()
	client := &openaiOAuthService{tokenURL: target.URL}
	response, err := client.RefreshTokenWithClientID(context.Background(), "synthetic-refresh", "", "synthetic-client")
	require.NoError(t, err)
	require.Equal(t, "synthetic-access", response.AccessToken)
	require.Equal(t, int32(1), calls.Load())
}
