//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPrepareCodexR2AttemptAcceptsExplicit0155Profile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.ReferenceProfile = codexidentity.Codex0155ProfileID
	cfg.Gateway.CodexR2.ShadowTelemetry = false
	svc := &OpenAIGatewayService{cfg: cfg}
	account := &Account{
		ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"chatgpt_account_id": "synthetic-account"},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: 77})
	headers := http.Header{
		"User-Agent":          {"codex-tui/0.155.1 (Windows 10.0.26200; x86_64) WindowsTerminal"},
		"Originator":          {"codex-tui"},
		"Version":             {"0.155.1"},
		"Session-Id":          {"0199a1b2-c3d4-7e5f-8a9b-111111111111"},
		"Thread-Id":           {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
		"X-Client-Request-Id": {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
	}
	body := []byte(`{"prompt_cache_key":"0199a1b2-c3d4-7e5f-8a9b-111111111111","client_metadata":{"session_id":"0199a1b2-c3d4-7e5f-8a9b-111111111111","thread_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222"}}`)

	attempt, err := svc.prepareCodexR2Attempt(context.Background(), c, account, headers, body, codexidentity.PurposeInference)
	require.NoError(t, err)
	require.NotNil(t, attempt)
	require.Equal(t, codexidentity.ModeShadow, attempt.Policy.Mode)
	require.Equal(t, codexidentity.Codex0155ProfileID, attempt.Policy.ReferenceProfile)
	require.Equal(t, codexidentity.Codex0155ProfileID, attempt.Plan.ProfileID)
	require.True(t, attempt.Plan.Client.Recognized)
	require.Equal(t, "0.155.1", attempt.Plan.Client.Version)
	require.NoError(t, attempt.Plan.Validate())
}
