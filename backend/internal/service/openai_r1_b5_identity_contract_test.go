package service

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestR1B5PassthroughUsesHyphenatedIngressSessionAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"body-cache","input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("session-id", "client-session")
	c.Request.Header.Set("conversation-id", "client-conversation")

	account := &Account{
		ID: 8008, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "synthetic-account"},
	}
	svc := &OpenAIGatewayService{}
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, body, "synthetic-token")
	require.NoError(t, err)

	identitySource := codexAccountIdentitySource(c, account)
	require.Equal(t, isolateOpenAIUpstreamSessionID(0, identitySource, "client-session"), req.Header.Get("session_id"))
	require.Equal(t, isolateOpenAIUpstreamSessionID(0, identitySource, "client-conversation"), req.Header.Get("conversation_id"))
	require.NotEqual(t, isolateOpenAIUpstreamSessionID(0, identitySource, "body-cache"), req.Header.Get("session_id"), "hyphenated ingress must not silently fall back to prompt_cache_key")
}

func TestR1B5PassthroughPreservesConfiguredOfficialClientSurface(t *testing.T) {
	tests := []struct{ name, ua, originator string }{
		{"exec", "codex_exec/0.151.0 (Ubuntu 24.04.3; x86_64) xterm-256color", "codex_exec"},
		{"tui", "codex-tui/0.151.0 (Ubuntu 24.04.3; x86_64) xterm-256color", "codex-tui"},
		{"vscode", "codex_vscode/0.151.0 (Windows NT 10.0; x86_64) vscode", "codex_vscode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"surface-contract","input":"hello"}`)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header.Set("User-Agent", "third-party/9.9")
			c.Request.Header.Set("originator", "third-party")
			c.Request.Header.Set("version", "9.9")
			account := &Account{
				ID: 8008, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{
					"chatgpt_account_id": "synthetic-account",
					"user_agent":         tt.ua,
				},
			}
			svc := &OpenAIGatewayService{}
			req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, body, "synthetic-token")
			require.NoError(t, err)
			require.Equal(t, tt.originator, req.Header.Get("originator"))
			require.Equal(t, CodexCanonicalClientVersion(), req.Header.Get("version"))
			require.True(t, strings.HasPrefix(req.Header.Get("user-agent"), tt.originator+"/"+CodexCanonicalClientVersion()+" "), req.Header.Get("user-agent"))
			require.NotContains(t, req.Header.Get("user-agent"), "0.151.0")
		})
	}
}

func TestR1B5DefaultLinuxFallbackMatchesLockedReferenceEnvironment(t *testing.T) {
	require.Equal(t, "0.154.0", codexCLIVersion)
	ua := buildCodexCLIUserAgent(codexCLIVersion)
	require.Contains(t, ua, "(Ubuntu 24.04.3; x86_64) xterm-256color")
	require.NotContains(t, ua, "Ubuntu 22.4.0")
}
