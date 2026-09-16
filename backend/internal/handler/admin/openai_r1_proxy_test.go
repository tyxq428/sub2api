package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type r1AdminProxyLookup struct {
	service.AdminService
	proxy *service.Proxy
	err   error
}

func (s *r1AdminProxyLookup) GetProxy(context.Context, int64) (*service.Proxy, error) {
	return s.proxy, s.err
}

type r1HandlerOAuthClient struct {
	service.OpenAIOAuthClient
	calls int
	proxy string
}

func (s *r1HandlerOAuthClient) RefreshTokenWithClientID(_ context.Context, _ string, proxy string, _ string) (*openai.TokenResponse, error) {
	s.calls++
	s.proxy = proxy
	return &openai.TokenResponse{AccessToken: "synthetic-access", ExpiresIn: 3600}, nil
}

// This invokes the real route and handler, not a proxy resolver helper.
// All credentials are fixed synthetic markers; the fake OAuth client cannot connect.
func TestR1AdminRefreshRejectsBrokenProxyBeforeOAuth(t *testing.T) {
	for _, kind := range []string{"lookup_error", "missing", "wrong_id", "bad_port"} {
		t.Run(kind, func(t *testing.T) {
			lookup := &r1AdminProxyLookup{}
			switch kind {
			case "lookup_error":
				lookup.err = errors.New("synthetic_proxy_password")
			case "wrong_id":
				lookup.proxy = &service.Proxy{ID: 2, Protocol: "http", Host: "127.0.0.1", Port: 8083}
			case "bad_port":
				lookup.proxy = &service.Proxy{ID: 1, Protocol: "http", Host: "127.0.0.1", Port: 0}
			}
			client := &r1HandlerOAuthClient{}
			oauth := service.NewOpenAIOAuthService(nil, client)
			defer oauth.Stop()
			h := NewOpenAIOAuthHandler(oauth, lookup, nil, nil)
			router := gin.New()
			router.POST("/api/v1/admin/openai/refresh-token", h.RefreshToken)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/openai/refresh-token", strings.NewReader(`{"refresh_token":"synthetic-refresh","proxy_id":1}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.Zero(t, client.calls, "a requested but unresolved proxy must stop before OAuth")
			require.NotContains(t, rec.Body.String(), "synthetic_proxy_password")
		})
	}
}

func TestR1AdminRefreshPreservesValidAndUnboundRoutes(t *testing.T) {
	for _, bound := range []bool{false, true} {
		lookup := &r1AdminProxyLookup{proxy: &service.Proxy{ID: 1, Protocol: "http", Host: "127.0.0.1", Port: 8083}}
		client := &r1HandlerOAuthClient{}
		oauth := service.NewOpenAIOAuthService(nil, client)
		h := NewOpenAIOAuthHandler(oauth, lookup, nil, nil)
		router := gin.New()
		router.POST("/refresh", h.RefreshToken)
		body := `{"refresh_token":"synthetic-refresh"}`
		want := ""
		if bound {
			body = `{"refresh_token":"synthetic-refresh","proxy_id":1}`
			want = lookup.proxy.URL()
		}
		req := httptest.NewRequest(http.MethodPost, "/refresh", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, 1, client.calls)
		require.Equal(t, want, client.proxy)
		oauth.Stop()
	}
}
