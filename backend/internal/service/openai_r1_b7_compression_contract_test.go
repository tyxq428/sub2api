package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func r1ReadRequestBody(t *testing.T, req *http.Request) []byte {
	t.Helper()
	require.NotNil(t, req)
	require.NotNil(t, req.Body)
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return body
}

func r1DecodeZstd(t *testing.T, compressed []byte) []byte {
	t.Helper()
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(compressed, nil)
	require.NoError(t, err)
	return decoded
}

func r1CodexOAuthAccount() *Account {
	return &Account{
		ID: 8008, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "synthetic-account",
			"access_token":       "synthetic-token",
		},
	}
}

func TestR1B7PassthroughResponsesCompressesCodexBackendAndPreservesBodyBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := []byte("{\n  \"model\": \"gpt-5.6-sol\", \"input\": \"hello\\u0020world\", \"stream\": false\n}\n")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(original))
	c.Request.Header.Set("Content-Type", "application/json")
	req, err := (&OpenAIGatewayService{}).buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, r1CodexOAuthAccount(), original, "synthetic-token")
	require.NoError(t, err)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))
	compressed := r1ReadRequestBody(t, req)
	require.Equal(t, int64(len(compressed)), req.ContentLength)
	require.Equal(t, original, r1DecodeZstd(t, compressed), "no-mutation passthrough must preserve original JSON bytes before compression")
}

func TestR1B7ReconstructedResponsesCompressesCodexBackend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6-sol","input":"hello","stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	req, err := (&OpenAIGatewayService{}).buildUpstreamRequest(c.Request.Context(), c, r1CodexOAuthAccount(), body, "synthetic-token", false, "cache-key", true)
	require.NoError(t, err)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))
	compressed := r1ReadRequestBody(t, req)
	require.Equal(t, body, r1DecodeZstd(t, compressed))
}

func TestR1B7ResponsesCompressionScopeExcludesDirectAPIKeyAndOtherProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6-sol","input":"hello"}`)
	tests := []struct {
		name    string
		account *Account
	}{
		{"openai_api_key", &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic-key", "base_url": "https://api.example.invalid"}}},
		{"other_oauth", &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "synthetic-token"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			req, err := (&OpenAIGatewayService{}).buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, tt.account, body, "synthetic-token")
			require.NoError(t, err)
			require.Empty(t, req.Header.Get("Content-Encoding"))
			require.Equal(t, body, r1ReadRequestBody(t, req))
		})
	}
}

func TestR1B7ResponsesCompressionDoesNotExpandToUnverifiedSubpaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6-sol","input":"hello"}`)
	for _, path := range []string{"/v1/responses/compact", "/v1/responses/input_tokens"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			req, err := (&OpenAIGatewayService{}).buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, r1CodexOAuthAccount(), body, "synthetic-token")
			require.NoError(t, err)
			require.Empty(t, req.Header.Get("Content-Encoding"))
			require.Equal(t, body, r1ReadRequestBody(t, req))
		})
	}
}
