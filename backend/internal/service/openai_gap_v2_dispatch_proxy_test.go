package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestGapV2ModelsBrokenBindingDoesNotDispatch(t *testing.T) {
	var directCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer target.Close()
	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = target.URL
	t.Cleanup(func() { chatgptCodexModelsURL = original })
	id := int64(1)
	for _, kind := range []string{"missing", "wrong_id", "bad_port"} {
		t.Run(kind, func(t *testing.T) {
			a := gapQuotaAccount(&id, nil)
			a.Credentials["access_token"] = "synthetic-access"
			want := "OPENAI_PROXY_UNAVAILABLE"
			if kind == "wrong_id" {
				u, _ := url.Parse(target.URL)
				port, _ := strconv.Atoi(u.Port())
				a.Proxy = &Proxy{ID: 2, Protocol: "http", Host: u.Hostname(), Port: port, Status: StatusActive}
			}
			if kind == "bad_port" {
				p := gapValidProxy()
				p.Port = 0
				a.Proxy = p
				want = "OPENAI_PROXY_INVALID"
			}
			s := &OpenAIGatewayService{cfg: &config.Config{}}
			_, err := s.FetchCodexModelsManifest(context.Background(), a, "0.154.0", "")
			require.Error(t, err)
			require.Equal(t, want, infraerrors.Reason(err))
			require.Zero(t, directCalls.Load())
		})
	}
}

func TestGapV2ModelsShadowUsesCredentialRoute(t *testing.T) {
	var directCalls, proxyCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if !r.URL.IsAbs() {
			t.Error("expected absolute-form request to controlled HTTP proxy")
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-access" {
			t.Error("credential selection changed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"synthetic"`)
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer proxy.Close()
	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = target.URL
	t.Cleanup(func() { chatgptCodexModelsURL = original })
	u, _ := url.Parse(proxy.URL)
	port, _ := strconv.Atoi(u.Port())
	id := int64(1)
	parent := gapQuotaAccount(&id, &Proxy{ID: 1, Protocol: "http", Host: u.Hostname(), Port: port, Status: StatusActive})
	parent.Credentials["access_token"] = "synthetic-access"
	pid := parent.ID
	shadow := &Account{ID: 8009, ParentAccountID: &pid, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, QuotaDimension: QuotaDimensionSpark}
	ar := &stubQuotaAccountRepo{accounts: map[int64]*Account{parent.ID: parent, shadow.ID: shadow}}
	s := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: ar}
	manifest, err := s.FetchCodexModelsManifest(context.Background(), shadow, "0.154.0", "")
	require.NoError(t, err)
	require.JSONEq(t, `{"models":[]}`, string(manifest.Body))
	require.Equal(t, int32(1), proxyCalls.Load())
	require.Zero(t, directCalls.Load())
	// A fresh cache entry must not bypass a newly broken route binding.
	parent.Proxy = nil
	_, err = s.FetchCodexModelsManifest(context.Background(), shadow, "0.154.0", "")
	require.Equal(t, "OPENAI_PROXY_UNAVAILABLE", infraerrors.Reason(err))
	require.Equal(t, int32(1), proxyCalls.Load())
}

func TestGapV2GatewayTokenPreflight(t *testing.T) {
	id := int64(1)
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
			a := gapQuotaAccount(&id, nil)
			a.Type = kind
			a.Credentials["api_key"] = "synthetic-key"
			a.Credentials["access_token"] = "synthetic-access"
			cache := &gapTokenCache{}
			s := &OpenAIGatewayService{openAITokenProvider: NewOpenAITokenProvider(nil, cache, nil)}
			_, _, err := s.GetAccessToken(context.Background(), a)
			require.Equal(t, "OPENAI_PROXY_UNAVAILABLE", infraerrors.Reason(err))
			require.Zero(t, cache.reads)
			a.Proxy = gapValidProxy()
			token, _, err := s.GetAccessToken(context.Background(), a)
			require.NoError(t, err)
			require.NotEmpty(t, token)
		})
	}
}

func TestGapV2HTTPDispatchBinding(t *testing.T) {
	id := int64(1)
	for _, entry := range []string{"gateway", "account_test"} {
		t.Run(entry, func(t *testing.T) {
			calls := 0
			seen := "unset"
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
				calls++
				seen = proxyURL
				body, _ := io.ReadAll(req.Body)
				require.Equal(t, `{"unchanged":true}`, string(body))
				require.Equal(t, "synthetic-ua", req.Header.Get("User-Agent"))
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}}
			a := gapQuotaAccount(&id, nil)
			invoke := func() error {
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/responses", strings.NewReader(`{"unchanged":true}`))
				req.Header.Set("User-Agent", "synthetic-ua")
				var resp *http.Response
				var err error
				if entry == "gateway" {
					resp, err = (&OpenAIGatewayService{httpUpstream: upstream}).doOpenAIUpstream(req, "", a)
				} else {
					resp, err = (&AccountTestService{httpUpstream: upstream}).doOpenAIAccountTestUpstream(req, "", a, false)
				}
				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				return err
			}
			err := invoke()
			require.Equal(t, "OPENAI_PROXY_UNAVAILABLE", infraerrors.Reason(err))
			require.Zero(t, calls)
			a.Proxy = gapValidProxy()
			require.NoError(t, invoke())
			require.Equal(t, 1, calls)
			require.Equal(t, a.Proxy.URL(), seen)
		})
	}
}

type gapV2WSDialer struct {
	mu     sync.Mutex
	routes []string
}

func (d *gapV2WSDialer) Dial(_ context.Context, _ string, _ http.Header, route string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	d.routes = append(d.routes, route)
	d.mu.Unlock()
	return &openAIWSFakeConn{}, 101, make(http.Header), nil
}
func (d *gapV2WSDialer) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.routes) }
func gapV2Pool(t *testing.T) (*openAIWSConnPool, *gapV2WSDialer) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	pool := newOpenAIWSConnPool(cfg)
	d := &gapV2WSDialer{}
	pool.setClientDialerForTest(d)
	t.Cleanup(pool.Close)
	return pool, d
}

func TestGapV2WSBrokenBindingBeforeDialOrPrewarm(t *testing.T) {
	id := int64(1)
	for _, entry := range []string{"acquire", "dial_prewarm"} {
		t.Run(entry, func(t *testing.T) {
			pool, d := gapV2Pool(t)
			a := gapQuotaAccount(&id, nil)
			factoryCalls := 0
			req := openAIWSAcquireRequest{Account: a, WSURL: "wss://example.invalid/responses", HeadersFactory: func(_ context.Context, h http.Header) (http.Header, error) { factoryCalls++; return h, nil }}
			var err error
			if entry == "acquire" {
				var lease *openAIWSConnLease
				lease, err = pool.Acquire(context.Background(), req)
				if lease != nil {
					lease.Release()
				}
			} else {
				var conn *openAIWSConn
				conn, err = pool.dialConn(context.Background(), req)
				if conn != nil {
					conn.close()
				}
			}
			require.Equal(t, "OPENAI_PROXY_UNAVAILABLE", infraerrors.Reason(err))
			require.Zero(t, factoryCalls)
			require.Zero(t, d.count())
		})
	}
}

func TestGapV2WSPoolRouteChangeDoesNotReuseConnection(t *testing.T) {
	id := int64(1)
	pool, d := gapV2Pool(t)
	a := gapQuotaAccount(&id, gapValidProxy())
	req := openAIWSAcquireRequest{Account: a, WSURL: "wss://example.invalid/responses", ProxyURL: a.Proxy.URL()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first, err := pool.Acquire(ctx, req)
	require.NoError(t, err)
	oldID := first.ConnID()
	first.Release()
	same, err := pool.Acquire(ctx, req)
	require.NoError(t, err)
	require.Equal(t, oldID, same.ConnID())
	same.Release()
	require.Equal(t, 1, d.count())
	broken := *a
	broken.Proxy = nil
	brokenReq := req
	brokenReq.Account = &broken
	_, bindingErr := pool.Acquire(ctx, brokenReq)
	require.Equal(t, "OPENAI_PROXY_UNAVAILABLE", infraerrors.Reason(bindingErr), "a cached socket cannot bypass missing binding metadata")
	require.Equal(t, 1, d.count())
	changed := *a
	newProxy := *a.Proxy
	newProxy.Port++
	changed.Proxy = &newProxy
	req.Account = &changed
	req.ProxyURL = newProxy.URL()
	next, err := pool.Acquire(ctx, req)
	require.NoError(t, err)
	defer next.Release()
	require.NotEqual(t, oldID, next.ConnID(), "same account with a new egress must not reuse old-route socket")
	require.Equal(t, 2, d.count())
}

func TestGapV2WSQueuedProxySnapshotIsIndependent(t *testing.T) {
	id := int64(1)
	a := gapQuotaAccount(&id, gapValidProxy())
	req := openAIWSAcquireRequest{Account: a, WSURL: "wss://example.invalid/responses", ProxyURL: a.Proxy.URL()}
	copied := cloneOpenAIWSAcquireRequest(req)
	a.Proxy.Port = 9876
	*a.ProxyID = 2
	require.Equal(t, 8083, copied.Account.Proxy.Port)
	require.Equal(t, int64(1), *copied.Account.ProxyID)
	require.Equal(t, "http://127.0.0.1:8083", copied.ProxyURL)
}

func TestGapV2HTTPUnboundAndOtherPlatformsUnchanged(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			a := gapQuotaAccount(nil, nil)
			a.Platform = platform
			sentinel := errors.New("synthetic stop")
			seen := ""
			upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, p string, _ int64, _ int) (*http.Response, error) {
				seen = p
				return nil, sentinel
			}}
			req, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
			_, err := (&OpenAIGatewayService{httpUpstream: upstream}).doOpenAIUpstream(req, "http://127.0.0.1:9888", a)
			require.ErrorIs(t, err, sentinel)
			require.Equal(t, "http://127.0.0.1:9888", seen)
		})
	}
}
