package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type gapV2OAuthClient struct {
	exchangeCalls, refreshCalls int
	proxyURL                    string
}

func (c *gapV2OAuthClient) ExchangeCode(_ context.Context, _, _, _, proxyURL, _ string) (*openai.TokenResponse, error) {
	c.exchangeCalls++
	c.proxyURL = proxyURL
	return &openai.TokenResponse{AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresIn: 3600}, nil
}
func (c *gapV2OAuthClient) RefreshToken(ctx context.Context, token, proxyURL string) (*openai.TokenResponse, error) {
	return c.RefreshTokenWithClientID(ctx, token, proxyURL, "")
}
func (c *gapV2OAuthClient) RefreshTokenWithClientID(_ context.Context, _, proxyURL, _ string) (*openai.TokenResponse, error) {
	c.refreshCalls++
	c.proxyURL = proxyURL
	return &openai.TokenResponse{AccessToken: "synthetic-access", ExpiresIn: 3600}, nil
}
func gapV2OAuthSession(s *OpenAIOAuthService, proxyURL string) {
	s.sessionStore.Set("synthetic-session", &openai.OAuthSession{
		State: "synthetic-state", CodeVerifier: "synthetic-verifier", RedirectURI: openai.DefaultRedirectURI,
		ProxyURL: proxyURL, CreatedAt: time.Now(),
	})
}

func TestGapV2OAuthBoundProxyFailsClosed(t *testing.T) {
	bound := int64(1)
	cases := []struct {
		name   string
		repo   func() ProxyRepository
		reason string
	}{
		{"missing_repo", func() ProxyRepository { return nil }, "OPENAI_PROXY_UNAVAILABLE"},
		{"missing_record", func() ProxyRepository { return &gapProxyRepository{} }, "OPENAI_PROXY_UNAVAILABLE"},
		{"lookup_error", func() ProxyRepository {
			return &gapProxyRepository{err: errors.New("synthetic_user:synthetic_password@proxy.invalid")}
		}, "OPENAI_PROXY_UNAVAILABLE"},
		{"wrong_record", func() ProxyRepository { p := gapValidProxy(); p.ID = 2; return &gapProxyRepository{result: p} }, "OPENAI_PROXY_UNAVAILABLE"},
		{"invalid_proxy", func() ProxyRepository { p := gapValidProxy(); p.Port = 0; return &gapProxyRepository{result: p} }, "OPENAI_PROXY_INVALID"},
	}
	for _, tc := range cases {
		for _, op := range []string{"authorize", "exchange", "refresh", "enrich_existing"} {
			t.Run(tc.name+"/"+op, func(t *testing.T) {
				client := &gapV2OAuthClient{}
				s := NewOpenAIOAuthService(tc.repo(), client)
				defer s.Stop()
				factoryCalls := 0
				s.SetPrivacyClientFactory(func(string) (*req.Client, error) { factoryCalls++; return nil, errors.New("synthetic factory stop") })
				var err error
				require.NotPanics(t, func() {
					switch op {
					case "authorize":
						_, err = s.GenerateAuthURL(context.Background(), &bound, "", "")
					case "exchange":
						gapV2OAuthSession(s, "http://127.0.0.1:9876")
						_, err = s.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: "synthetic-session", State: "synthetic-state", Code: "synthetic-code", ProxyID: &bound})
					default:
						a := gapQuotaAccount(&bound, nil)
						a.Credentials["access_token"] = "synthetic-access"
						if op == "refresh" {
							a.Credentials["refresh_token"] = "synthetic-refresh"
						}
						_, err = s.RefreshAccountToken(context.Background(), a)
					}
				})
				require.Error(t, err)
				require.Equal(t, tc.reason, infraerrors.Reason(err))
				require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
				require.Zero(t, client.exchangeCalls+client.refreshCalls)
				require.Zero(t, factoryCalls, "enrichment/privacy must not run on a broken binding")
				require.NotContains(t, err.Error(), "synthetic_password")
				require.NotContains(t, err.Error(), "proxy.invalid")
				if op == "exchange" {
					_, ok := s.sessionStore.Get("synthetic-session")
					require.True(t, ok, "failed route validation must retain unconsumed authorization session")
				}
			})
		}
	}
}

func TestGapV2OAuthValidRoutesAndStatePreserved(t *testing.T) {
	id := int64(1)
	for _, op := range []string{"authorize", "exchange_override", "exchange_session", "refresh_bound", "refresh_direct", "latest_lookup"} {
		t.Run(op, func(t *testing.T) {
			p := gapValidProxy()
			p.Username = "synthetic-user"
			p.Password = "synthetic:password"
			client := &gapV2OAuthClient{}
			repo := &gapProxyRepository{result: p}
			s := NewOpenAIOAuthService(repo, client)
			defer s.Stop()
			want := p.URL()
			switch op {
			case "authorize":
				out, err := s.GenerateAuthURL(context.Background(), &id, "", "")
				require.NoError(t, err)
				session, ok := s.sessionStore.Get(out.SessionID)
				require.True(t, ok)
				require.Equal(t, want, session.ProxyURL)
				require.Zero(t, client.exchangeCalls+client.refreshCalls)
			case "exchange_override", "exchange_session":
				gapV2OAuthSession(s, want)
				input := &OpenAIExchangeCodeInput{SessionID: "synthetic-session", State: "wrong", Code: "synthetic-code"}
				_, err := s.ExchangeCode(context.Background(), input)
				require.Error(t, err)
				require.Zero(t, client.exchangeCalls)
				input.State = "synthetic-state"
				if op == "exchange_override" {
					input.ProxyID = &id
					gapV2OAuthSession(s, "http://127.0.0.1:9876")
				}
				_, err = s.ExchangeCode(context.Background(), input)
				require.NoError(t, err)
				require.Equal(t, want, client.proxyURL)
				require.Equal(t, 1, client.exchangeCalls)
				_, ok := s.sessionStore.Get(input.SessionID)
				require.False(t, ok)
			default:
				a := gapQuotaAccount(&id, nil)
				a.Credentials["refresh_token"] = "synthetic-refresh"
				if op == "refresh_direct" {
					a.ProxyID = nil
					want = ""
				}
				if op == "latest_lookup" {
					old := *p
					old.Port = 9876
					a.Proxy = &old
				}
				_, err := s.RefreshAccountToken(context.Background(), a)
				require.NoError(t, err)
				require.Equal(t, want, client.proxyURL)
				require.Equal(t, 1, client.refreshCalls)
			}
		})
	}
}

func TestGapV2OAuthRawProxyValidation(t *testing.T) {
	for _, op := range []string{"refresh", "exchange_session"} {
		t.Run(op, func(t *testing.T) {
			client := &gapV2OAuthClient{}
			s := NewOpenAIOAuthService(nil, client)
			defer s.Stop()
			raw := "http://synthetic_user:synthetic_password@proxy.invalid:bad"
			var err error
			if op == "refresh" {
				_, err = s.RefreshTokenWithClientID(context.Background(), "synthetic-refresh", raw, "")
			} else {
				gapV2OAuthSession(s, raw)
				_, err = s.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: "synthetic-session", State: "synthetic-state", Code: "synthetic-code"})
			}
			require.Error(t, err)
			require.Equal(t, "OPENAI_PROXY_INVALID", infraerrors.Reason(err))
			require.Zero(t, client.exchangeCalls+client.refreshCalls)
			require.NotContains(t, err.Error(), "synthetic_password")
		})
	}
}

func TestGapV2PrivacyBoundProxyFailsClosed(t *testing.T) {
	bound := int64(1)
	for _, op := range []string{"admin_ensure", "admin_force", "background"} {
		for _, kind := range []string{"missing_repo", "missing_record", "lookup_error", "invalid_proxy"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				a := gapQuotaAccount(&bound, nil)
				a.Credentials["access_token"] = "synthetic-access"
				ar := &stubQuotaAccountRepo{accounts: map[int64]*Account{a.ID: a}}
				var pr ProxyRepository
				switch kind {
				case "missing_record":
					pr = &gapProxyRepository{}
				case "lookup_error":
					pr = &gapProxyRepository{err: errors.New("synthetic-user:synthetic-password")}
				case "invalid_proxy":
					p := gapValidProxy()
					p.Port = 0
					pr = &gapProxyRepository{result: p}
				}
				calls := 0
				factory := func(string) (*req.Client, error) { calls++; return nil, errors.New("synthetic stop") }
				require.NotPanics(t, func() {
					if op == "background" {
						s := &TokenRefreshService{accountRepo: ar, proxyRepo: pr, privacyClientFactory: factory}
						s.ensureOpenAIPrivacy(context.Background(), a)
					} else {
						s := &adminServiceImpl{accountRepo: ar, proxyRepo: pr, privacyClientFactory: factory}
						var mode string
						if op == "admin_ensure" {
							mode = s.EnsureOpenAIPrivacy(context.Background(), a)
						} else {
							mode = s.ForceOpenAIPrivacy(context.Background(), a)
						}
						require.Equal(t, PrivacyModeFailed, mode)
					}
				})
				require.Zero(t, calls, "no default-route privacy call on unresolved binding")
				require.Equal(t, PrivacyModeFailed, ar.extraUpdates[a.ID]["privacy_mode"], "failure must remain eligible for privacy retry, not report training_off")
			})
		}
	}
}

func TestGapV2PrivacyValidRoutesAndSkipPreserved(t *testing.T) {
	id := int64(1)
	for _, op := range []string{"admin_ensure", "admin_force", "background"} {
		for _, kind := range []string{"bound", "unbound", "already_off"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				p := gapValidProxy()
				a := gapQuotaAccount(&id, nil)
				a.Credentials["access_token"] = "synthetic-access"
				if kind == "unbound" {
					a.ProxyID = nil
				}
				if kind == "already_off" {
					a.Extra = map[string]any{"privacy_mode": PrivacyModeTrainingOff}
				}
				ar := &stubQuotaAccountRepo{accounts: map[int64]*Account{a.ID: a}}
				calls := 0
				seen := "not-called"
				factory := func(url string) (*req.Client, error) { calls++; seen = url; return nil, errors.New("synthetic stop") }
				pr := &gapProxyRepository{result: p}
				if op == "background" {
					(&TokenRefreshService{accountRepo: ar, proxyRepo: pr, privacyClientFactory: factory}).ensureOpenAIPrivacy(context.Background(), a)
				} else {
					s := &adminServiceImpl{accountRepo: ar, proxyRepo: pr, privacyClientFactory: factory}
					if op == "admin_ensure" {
						s.EnsureOpenAIPrivacy(context.Background(), a)
					} else {
						s.ForceOpenAIPrivacy(context.Background(), a)
					}
				}
				if kind == "already_off" && op != "admin_force" {
					require.Zero(t, calls)
					return
				}
				require.Equal(t, 1, calls)
				want := p.URL()
				if kind == "unbound" {
					want = ""
				}
				require.Equal(t, want, seen)
			})
		}
	}
}
