package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestR1APIKeyResponsesProbeBrokenBindingDoesNotDispatch(t *testing.T) {
	proxyID := int64(701)
	account := Account{
		ID:          9701,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		ProxyID:     &proxyID,
		Proxy:       nil, // explicit binding exists but its route was not hydrated/resolved
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "synthetic-r1-api-key",
			"base_url": "https://compat-upstream.example/v1",
		},
	}
	repo := &snapshotUpdateAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"output":[{"type":"function_call"}]}`)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)

	require.Nil(t, upstream.lastReq, "an unresolved explicit OpenAI proxy binding must stop the background probe before dispatch")
	require.Empty(t, upstream.requests)
}

func TestR1CodexPATWhoamiDoesNotFollowRedirect(t *testing.T) {
	statuses := []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirected.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"email":"redirected@example.invalid","chatgpt_user_id":"u","chatgpt_account_id":"a","chatgpt_plan_type":"plus"}`))
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer at-synthetic-r1", r.Header.Get("Authorization"))
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
			}))
			defer source.Close()

			original := openAICodexPATWhoamiURL
			openAICodexPATWhoamiURL = source.URL
			defer func() { openAICodexPATWhoamiURL = original }()

			svc := NewOpenAIOAuthService(nil, nil)
			defer svc.Stop()
			_, err := svc.ValidateCodexPersonalAccessToken(context.Background(), "at-synthetic-r1", "")

			require.Error(t, err, "a redirect response must remain a validation failure")
			require.Zero(t, redirected.Load(), "credentialed PAT validation must not follow an arbitrary redirect target")
		})
	}
}

type r1BlockingHTTPUpstream struct {
	*httpUpstreamRecorder
	entered chan string
	release chan struct{}
	calls   atomic.Int32
}

func (u *r1BlockingHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	u.calls.Add(1)
	u.entered <- proxyURL
	<-u.release
	return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
}

func TestR1RequiredProxyAttemptBoundary(t *testing.T) {
	proxyID := int64(702)
	account := &Account{
		ID:          9702,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		ProxyID:     &proxyID,
		Proxy:       &Proxy{ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 8083, Status: StatusActive},
		Extra:       map[string]any{"openai_proxy_required": true},
	}
	upstream := &r1BlockingHTTPUpstream{
		httpUpstreamRecorder: &httpUpstreamRecorder{},
		entered:              make(chan string, 1),
		release:              make(chan struct{}),
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	request := httptest.NewRequest(http.MethodPost, "https://chatgpt.example.invalid/responses", strings.NewReader(`{}`))
	done := make(chan error, 1)
	go func() {
		resp, err := svc.doOpenAIUpstream(request, "", account)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()

	firstRoute := <-upstream.entered
	require.Equal(t, "http://127.0.0.1:8083", firstRoute)

	// Configuration changes after the network attempt crossed the dispatch
	// boundary do not rewrite the already-started attempt's route.
	account.ProxyID = nil
	account.Proxy = nil
	close(upstream.release)
	require.NoError(t, <-done)
	require.Equal(t, int32(1), upstream.calls.Load())

	// A new attempt sees the revoked binding and fails before network dispatch.
	next := httptest.NewRequest(http.MethodPost, "https://chatgpt.example.invalid/responses", strings.NewReader(`{}`))
	_, err := svc.doOpenAIUpstream(next, "", account)
	require.Error(t, err)
	require.Equal(t, "OPENAI_PROXY_REQUIRED", infraerrors.Reason(err))
	require.Equal(t, int32(1), upstream.calls.Load())
}

func TestR1OpenAIAccountTestDirectAPIKeyPathsBrokenBindingDoNotDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	proxyID := int64(703)
	cases := []struct {
		name string
		run  func(*AccountTestService, *gin.Context, *Account) error
	}{
		{
			name: "chat_completions",
			run: func(s *AccountTestService, c *gin.Context, account *Account) error {
				return s.testOpenAIChatCompletionsConnection(c, account, "gpt-r1-test", "hi", "https://api.openai.com", "synthetic-r1-api-key")
			},
		},
		{
			name: "image_generation",
			run: func(s *AccountTestService, c *gin.Context, account *Account) error {
				return s.testOpenAIImageAPIKey(c, c.Request.Context(), account, "gpt-image-1", "synthetic image prompt")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				ID:          9703,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				ProxyID:     &proxyID,
				Proxy:       nil,
				Concurrency: 1,
				Credentials: map[string]any{
					"api_key":  "synthetic-r1-api-key",
					"base_url": "https://api.openai.com",
				},
			}
			upstream := &httpUpstreamRecorder{err: errors.New("synthetic upstream should not be reached")}
			svc := &AccountTestService{
				httpUpstream: upstream,
				cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/admin/r1-account-test", nil)

			_ = tc.run(svc, c, account)

			require.Nil(t, upstream.lastReq, "an unresolved explicit OpenAI proxy binding must stop admin account-test dispatch")
			require.Empty(t, upstream.requests)
		})
	}
}
