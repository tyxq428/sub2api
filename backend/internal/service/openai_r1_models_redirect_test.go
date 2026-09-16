package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestR1ModelsDoNotFollowCredentialedRedirect(t *testing.T) {
	var calls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", second.URL+"/must-not-receive-credentials")
		w.WriteHeader(http.StatusFound)
	}))
	defer first.Close()
	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = first.URL
	t.Cleanup(func() { chatgptCodexModelsURL = original })
	account := gapQuotaAccount(nil, nil)
	account.Credentials["access_token"] = "synthetic-model-access"
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	_, err := svc.FetchCodexModelsManifest(context.Background(), account, "0.154.0", "")
	require.Error(t, err, "an upstream redirect is not a successful model manifest")
	require.Zero(t, calls.Load(), "do not send any request to the redirect target")
}
