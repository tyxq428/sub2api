package service

import (
	"context"
	"errors"
	"github.com/imroc/req/v3"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// The optional policy lives in existing account Extra; tests never modify a
// production account and a missing policy must preserve legacy routing.
func TestR1RequiredProxyRejectsLostBinding(t *testing.T) {
	account := &Account{ID: 8008, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_proxy_required": true}}
	_, err := resolveOpenAIAccountProxyURL(context.Background(), account, nil)
	require.Equal(t, "OPENAI_PROXY_REQUIRED", infraerrors.Reason(err))
	_, err = resolveOpenAIDispatchProxyURL(context.Background(), account, "http://127.0.0.1:19090")
	require.Equal(t, "OPENAI_PROXY_REQUIRED", infraerrors.Reason(err), "a default candidate cannot substitute for a revoked required binding")
}

func TestR1RequiredProxyRejectsMalformedPolicy(t *testing.T) {
	for _, value := range []any{"true", 1, nil, map[string]any{}} {
		account := &Account{ID: 8008, Platform: PlatformOpenAI, Extra: map[string]any{"openai_proxy_required": value}}
		_, err := resolveOpenAIAccountProxyURL(context.Background(), account, nil)
		require.Equal(t, "OPENAI_PROXY_POLICY_INVALID", infraerrors.Reason(err))
		_, err = resolveOpenAIDispatchProxyURL(context.Background(), account, "")
		require.Equal(t, "OPENAI_PROXY_POLICY_INVALID", infraerrors.Reason(err))
	}
}

func TestR1RequiredProxyRejectsExpiredDisabledAndAutomaticFallback(t *testing.T) {
	id := int64(1)
	expired := time.Now().Add(-time.Minute)
	for _, kind := range []string{"expired", "disabled", "fallback"} {
		t.Run(kind, func(t *testing.T) {
			proxy := &Proxy{ID: id, Protocol: "http", Host: "127.0.0.1", Port: 8083, Status: StatusActive}
			account := &Account{ID: 8008, Platform: PlatformOpenAI, ProxyID: &id, Proxy: proxy, Extra: map[string]any{"openai_proxy_required": true}}
			if kind == "expired" {
				proxy.ExpiresAt = &expired
			}
			if kind == "disabled" {
				proxy.Status = StatusDisabled
			}
			if kind == "fallback" {
				old := int64(2)
				account.ProxyFallbackOriginID = &old
			}
			_, err := resolveOpenAIAccountProxyURL(context.Background(), account, nil)
			require.Error(t, err)
			require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
		})
	}
}

func TestR1RequiredProxyKeepsLegacyAndValidRoutes(t *testing.T) {
	id := int64(1)
	for _, enabled := range []bool{false, true} {
		account := &Account{ID: 8008, Platform: PlatformOpenAI, ProxyID: &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "127.0.0.1", Port: 8083, Status: StatusActive}, Extra: map[string]any{"openai_proxy_required": enabled}}
		route, err := resolveOpenAIAccountProxyURL(context.Background(), account, nil)
		require.NoError(t, err)
		require.Equal(t, "http://127.0.0.1:8083", route)
	}
	for _, extra := range []map[string]any{nil, {"openai_proxy_required": false}} {
		route, err := resolveOpenAIDispatchProxyURL(context.Background(), &Account{Platform: PlatformOpenAI, Extra: extra}, "http://127.0.0.1:19090")
		require.NoError(t, err)
		require.Equal(t, "http://127.0.0.1:19090", route)
	}
	route, err := resolveOpenAIDispatchProxyURL(context.Background(), &Account{Platform: PlatformGrok, Extra: map[string]any{"openai_proxy_required": "not-applicable"}}, "original-route")
	require.NoError(t, err)
	require.Equal(t, "original-route", route)
}

func TestR1RequiredProxyBlocksUnverifiedPlugin(t *testing.T) {
	id := int64(1)
	account := &Account{ID: 8008, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "127.0.0.1", Port: 8083, Status: StatusActive}, Extra: map[string]any{"openai_proxy_required": true}}
	manager := &PluginManager{}
	// No external process is launched. A selected but unavailable route still
	// must fail the strict-policy gate before any plugin exchange is attempted.
	manager.route.Store(&pluginRoute{rolloutPercent: 100, unavailable: "synthetic plugin not started"})
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/responses", nil)
	require.NoError(t, err)
	_, handled, err := manager.RoundTripOpenAIOAuth(context.Background(), request, account.Proxy.URL(), account)
	require.True(t, handled)
	require.Equal(t, "OPENAI_PROXY_PLUGIN_UNVERIFIED", infraerrors.Reason(err))
}

type r1PrivacyUpdateRepo struct {
	AccountRepository
	mode string
}

func (r *r1PrivacyUpdateRepo) UpdateExtra(_ context.Context, _ int64, extra map[string]any) error {
	r.mode, _ = extra["privacy_mode"].(string)
	return nil
}

func TestR1RequiredProxySurvivesAdministrativeReload(t *testing.T) {
	for _, op := range []string{"refresh", "ensure", "force", "background"} {
		t.Run(op, func(t *testing.T) {
			account := &Account{ID: 8008, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "synthetic-token"}, Extra: map[string]any{"openai_proxy_required": true}}
			calls := 0
			factory := func(_ string) (*req.Client, error) {
				calls++
				return nil, errors.New("synthetic stop before networking")
			}
			repo := &r1PrivacyUpdateRepo{}
			switch op {
			case "refresh":
				service := NewOpenAIOAuthService(nil, &openaiOAuthClientRefreshStub{})
				defer service.Stop()
				service.SetPrivacyClientFactory(factory)
				_, err := service.RefreshAccountToken(context.Background(), account)
				require.Equal(t, "OPENAI_PROXY_REQUIRED", infraerrors.Reason(err))
			case "ensure", "force":
				service := &adminServiceImpl{accountRepo: repo, privacyClientFactory: factory}
				if op == "ensure" {
					require.Equal(t, PrivacyModeFailed, service.EnsureOpenAIPrivacy(context.Background(), account))
				} else {
					require.Equal(t, PrivacyModeFailed, service.ForceOpenAIPrivacy(context.Background(), account))
				}
				require.Equal(t, PrivacyModeFailed, repo.mode)
			case "background":
				service := &TokenRefreshService{accountRepo: repo, privacyClientFactory: factory}
				service.ensureOpenAIPrivacy(context.Background(), account)
				require.Equal(t, PrivacyModeFailed, repo.mode)
			}
			require.Zero(t, calls, "reloading a proxy by ID must not discard the account's required policy")
		})
	}
}

func TestR1AgentTaskRegistrationPreservesBindingAndRedirectBoundary(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	var sourceCalls, redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"synthetic-task"}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	old := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = source.URL
	defer func() { openAIAgentIdentityAuthAPIBaseURL = old }()
	account := &Account{ID: 8008, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": key.runtimeID, "agent_private_key": privateKey}}
	t.Run("broken_binding_zero_dispatch", func(t *testing.T) {
		id := int64(1)
		account.ProxyID = &id
		_, err := registerAgentIdentityTask(context.Background(), account)
		require.Equal(t, "OPENAI_PROXY_UNAVAILABLE", infraerrors.Reason(err))
		require.Zero(t, sourceCalls.Load())
		require.Zero(t, redirected.Load())
	})
	t.Run("valid_unbound_no_redirect", func(t *testing.T) {
		sourceCalls.Store(0)
		redirected.Store(0)
		account.ProxyID = nil
		_, err := registerAgentIdentityTask(context.Background(), account)
		require.Error(t, err)
		require.Equal(t, int32(1), sourceCalls.Load())
		require.Zero(t, redirected.Load())
	})
}
