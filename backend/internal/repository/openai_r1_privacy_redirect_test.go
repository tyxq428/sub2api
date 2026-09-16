package repository

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestR1PrivacyClientDoesNotFollowRedirect(t *testing.T) {
	var calls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", second.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer first.Close()
	client, err := CreatePrivacyReqClient("")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := client.R().SetContext(ctx).SetHeader("Authorization", "Bearer synthetic-test-token").SetHeader("X-Api-Key", "synthetic-test-key").Get(first.URL)
	if err == nil {
		require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
	}
	require.Zero(t, calls.Load(), "OpenAI privacy client must not follow an arbitrary redirect")
}

func TestR1ReqRedirectPolicyCacheSeparation(t *testing.T) {
	regular, err := getSharedReqClient(reqClientOptions{Timeout: time.Second})
	require.NoError(t, err)
	protected, err := getSharedReqClient(reqClientOptions{Timeout: time.Second, DisableRedirects: true})
	require.NoError(t, err)
	again, err := getSharedReqClient(reqClientOptions{Timeout: time.Second, DisableRedirects: true})
	require.NoError(t, err)
	require.NotSame(t, regular, protected)
	require.Same(t, protected, again)
	require.NotEqual(t, buildReqClientKey(reqClientOptions{Timeout: time.Second}), buildReqClientKey(reqClientOptions{Timeout: time.Second, DisableRedirects: true}))
}
