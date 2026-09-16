package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type r1B5HeaderCaptureUpstream struct {
	service.HTTPUpstream
	mu     sync.Mutex
	header http.Header
}

func (u *r1B5HeaderCaptureUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.header = req.Header.Clone()
	u.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_r1_b5_wire","object":"response","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}, nil
}

func (u *r1B5HeaderCaptureUpstream) snapshot() http.Header {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.header.Clone()
}

func TestR1B5HandlerPreservesHyphenatedSessionAliasesToFakeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(5205)
	account := service.Account{
		ID: 9951, Name: "r1-b5-wire", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "synthetic-key", "base_url": "https://api.example.invalid"},
		Extra:       map[string]any{"openai_passthrough": true},
	}
	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}
	upstream := &r1B5HeaderCaptureUpstream{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCacheSvc.Stop)
	gatewaySvc := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCacheSvc, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	h := NewOpenAIGatewayHandler(
		gatewaySvc, service.NewConcurrencyService(nil), billingCacheSvc,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg,
	)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol","input":"hello","stream":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("session-id", "wire-session")
	c.Request.Header.Set("conversation-id", "wire-conversation")
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1905, GroupID: &groupID, User: &service.User{ID: 1805, Status: service.StatusActive}, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive}})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1805, Concurrency: 0})

	h.Responses(c)
	require.Equal(t, http.StatusOK, rec.Code)
	got := upstream.snapshot()
	require.Equal(t, "wire-session", got.Get("session-id"))
	require.Equal(t, "wire-conversation", got.Get("conversation-id"))
	require.Empty(t, got.Get("session_id"), "API-key passthrough must not invent OAuth session identity")
}
