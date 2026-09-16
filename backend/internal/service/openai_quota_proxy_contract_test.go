package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type gapProxyRepository struct {
	ProxyRepository
	result *Proxy
	err    error
	calls  int
}

func (r *gapProxyRepository) GetByID(_ context.Context, _ int64) (*Proxy, error) {
	r.calls++
	return r.result, r.err
}

type gapTokenCache struct {
	OpenAITokenCache
	reads int
}

func (c *gapTokenCache) GetAccessToken(_ context.Context, _ string) (string, error) {
	c.reads++
	return "synthetic-gap-token", nil
}

func gapQuotaAccount(proxyID *int64, proxy *Proxy) *Account {
	return &Account{
		ID: 8008, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, ProxyID: proxyID, Proxy: proxy,
		Credentials: map[string]any{"chatgpt_account_id": "synthetic-gap-account"},
	}
}
func gapValidProxy() *Proxy {
	return &Proxy{ID: 1, Protocol: "http", Host: "127.0.0.1", Port: 8083, Status: StatusActive}
}

func TestOpenAIQuotaBoundProxyFailsClosed(t *testing.T) {
	boundID := int64(1)
	tests := []struct {
		name   string
		loaded *Proxy
		lookup *gapProxyRepository
		reason string
	}{
		{name: "missing_repository", reason: "OPENAI_PROXY_UNAVAILABLE"},
		{name: "missing_record", lookup: &gapProxyRepository{}, reason: "OPENAI_PROXY_UNAVAILABLE"},
		{name: "repository_error", lookup: &gapProxyRepository{err: errors.New("database failed for http://probe_user:probe_password@proxy.invalid")}, reason: "OPENAI_PROXY_UNAVAILABLE"},
		{name: "wrong_loaded_id", loaded: &Proxy{ID: 2, Protocol: "http", Host: "127.0.0.1", Port: 8083}, reason: "OPENAI_PROXY_UNAVAILABLE"},
		{name: "wrong_lookup_id", lookup: &gapProxyRepository{result: &Proxy{ID: 2, Protocol: "http", Host: "127.0.0.1", Port: 8083}}, reason: "OPENAI_PROXY_UNAVAILABLE"},
		{name: "empty_host", loaded: &Proxy{ID: 1, Protocol: "http", Port: 8083}, reason: "OPENAI_PROXY_INVALID"},
		{name: "zero_port", loaded: &Proxy{ID: 1, Protocol: "http", Host: "127.0.0.1"}, reason: "OPENAI_PROXY_INVALID"},
		{name: "large_port", loaded: &Proxy{ID: 1, Protocol: "http", Host: "127.0.0.1", Port: 65536}, reason: "OPENAI_PROXY_INVALID"},
		{name: "unsupported_scheme", loaded: &Proxy{ID: 1, Protocol: "ftp", Host: "127.0.0.1", Port: 8083}, reason: "OPENAI_PROXY_INVALID"},
		{name: "invalid_host", loaded: &Proxy{ID: 1, Protocol: "http", Host: "proxy.invalid\n", Port: 8083, Username: "probe_user", Password: "probe_password"}, reason: "OPENAI_PROXY_INVALID"},
	}
	calls := []struct {
		name string
		run  func(*OpenAIQuotaService) error
	}{
		{"query", func(s *OpenAIQuotaService) error { _, err := s.QueryUsage(context.Background(), 8008); return err }},
		{"reset", func(s *OpenAIQuotaService) error { _, err := s.ResetCredit(context.Background(), 8008); return err }},
		{"targeted_reset", func(s *OpenAIQuotaService) error {
			_, err := s.ResetCreditTargeted(context.Background(), 8008, "synthetic-credit", "synthetic-request")
			return err
		}},
	}
	for _, tt := range tests {
		for _, op := range calls {
			t.Run(tt.name+"/"+op.name, func(t *testing.T) {
				account := gapQuotaAccount(&boundID, tt.loaded)
				repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
				var proxyRepo ProxyRepository
				if tt.lookup != nil {
					copy := *tt.lookup
					proxyRepo = &copy
				}
				cache := &gapTokenCache{}
				factoryCalls := 0
				factory := func(_ string) (*req.Client, error) {
					factoryCalls++
					return nil, errors.New("unexpected upstream client creation")
				}
				s := NewOpenAIQuotaService(repo, proxyRepo, NewOpenAITokenProvider(repo, cache, nil), factory)
				err := op.run(s)
				require.Error(t, err)
				require.Equal(t, tt.reason, infraerrors.Reason(err))
				require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
				require.Zero(t, cache.reads, "proxy validation must precede token lookup/refresh")
				require.Zero(t, factoryCalls, "no network client may be created with an unresolved binding")
				for _, marker := range []string{"probe_user", "probe_password", "proxy.invalid"} {
					require.NotContains(t, err.Error(), marker)
				}
			})
		}
	}
}

func TestOpenAIQuotaProxyFailurePrecedesTokenLookup(t *testing.T) {
	boundID := int64(1)
	account := gapQuotaAccount(&boundID, nil)
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	// Nil token provider is deliberate: an unresolved route must be rejected
	// before token-provider validation (and therefore before any OAuth refresh).
	s := NewOpenAIQuotaService(repo, nil, nil, func(_ string) (*req.Client, error) { t.Fatal("factory called"); return nil, nil })
	_, _, _, _, err := s.prepareUpstreamCall(context.Background(), account.ID)
	require.Equal(t, "OPENAI_PROXY_UNAVAILABLE", infraerrors.Reason(err))
}

func TestOpenAIQuotaProxyResolutionPreservesValidRoutes(t *testing.T) {
	boundID := int64(1)
	for _, source := range []string{"eager", "lookup", "unbound"} {
		t.Run(source, func(t *testing.T) {
			p := gapValidProxy()
			p.Username = "synthetic-user"
			p.Password = "synthetic:p@ss"
			account := gapQuotaAccount(&boundID, p)
			lookup := &gapProxyRepository{result: p}
			want := p.URL()
			if source == "lookup" {
				account.Proxy = nil
			}
			if source == "unbound" {
				account.ProxyID = nil
				want = ""
			}
			repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
			cache := &gapTokenCache{}
			s := NewOpenAIQuotaService(repo, lookup, NewOpenAITokenProvider(repo, cache, nil), func(_ string) (*req.Client, error) { t.Fatal("factory called during preflight"); return nil, nil })
			token, accountID, proxyURL, _, err := s.prepareUpstreamCall(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, "synthetic-gap-token", token)
			require.Equal(t, "synthetic-gap-account", accountID)
			require.Equal(t, want, proxyURL)
			require.Equal(t, 1, cache.reads)
			if source == "lookup" {
				require.Equal(t, 1, lookup.calls)
			} else {
				require.Zero(t, lookup.calls)
			}
			require.Equal(t, "synthetic:p@ss", p.Password, "resolution must not mutate the shared proxy")
		})
	}
}

func TestOpenAIQuotaProxyContractReachesFakeUpstream(t *testing.T) {
	boundID := int64(1)
	p := gapValidProxy()
	account := gapQuotaAccount(&boundID, p)
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	cache := &gapTokenCache{}
	seen := []string{}
	var seenMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer synthetic-gap-token", r.Header.Get("Authorization"))
		seenMu.Lock()
		seen = append(seen, r.URL.Path)
		seenMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	localFactory := newQuotaRedirectingFactory(srv)
	factoryCalls := 0
	factory := func(url string) (*req.Client, error) {
		factoryCalls++
		require.Equal(t, p.URL(), url)
		return localFactory(url)
	}
	s := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), factory)
	_, err := s.QueryUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, 1, factoryCalls)
	seenMu.Lock()
	defer seenMu.Unlock()
	require.Equal(t, []string{"/backend-api/wham/usage", "/backend-api/wham/rate-limit-reset-credits"}, seen)
}

func TestOpenAIQuotaShadowUsesParentProxyBinding(t *testing.T) {
	boundID := int64(1)
	parent := gapQuotaAccount(&boundID, nil)
	parentID := parent.ID
	shadow := &Account{ID: 8009, ParentAccountID: &parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, QuotaDimension: QuotaDimensionSpark}
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{parent.ID: parent, shadow.ID: shadow}}
	cache := &gapTokenCache{}
	s := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), func(_ string) (*req.Client, error) { t.Fatal("factory called"); return nil, nil })
	_, _, _, _, err := s.prepareUpstreamCall(context.Background(), shadow.ID)
	require.Equal(t, "OPENAI_PROXY_UNAVAILABLE", infraerrors.Reason(err))
	require.Zero(t, cache.reads)
	parent.Proxy = gapValidProxy()
	_, _, proxyURL, _, err := s.prepareUpstreamCall(context.Background(), shadow.ID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(proxyURL, "http://127.0.0.1:"))
}
