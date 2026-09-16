package service

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type r1StoppingWSDialer struct {
	calls atomic.Int32
	route string
}

func (d *r1StoppingWSDialer) Dial(_ context.Context, _ string, _ http.Header, route string) (openAIWSClientConn, int, http.Header, error) {
	d.route = route
	d.calls.Add(1)
	return nil, http.StatusBadGateway, nil, errors.New("synthetic local dial stop")
}

func TestR1DedicatedWSPassthroughProxyBinding(t *testing.T) {
	for _, kind := range []string{"missing", "wrong_id", "bad_port", "valid"} {
		t.Run(kind, func(t *testing.T) {
			account := passthroughLifecycleAccount()
			id := int64(1)
			account.ProxyID = &id
			if kind != "missing" {
				account.Proxy = gapValidProxy()
			}
			if kind == "wrong_id" {
				account.Proxy.ID = 2
			}
			if kind == "bad_port" {
				account.Proxy.Port = 0
			}
			dialer := &r1StoppingWSDialer{}
			svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), newStagedPassthroughConn())
			svc.openaiWSPassthroughDialer = dialer
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			server, done := startPassthroughLifecycleServer(t, ctx, svc, account)
			defer server.Close()
			conn := dialPassthroughLifecycleClient(t, server)
			defer conn.CloseNow()
			select {
			case err := <-done:
				require.Error(t, err)
				if kind == "valid" {
					require.Positive(t, dialer.calls.Load())
					require.Equal(t, account.Proxy.URL(), dialer.route)
				} else {
					require.Zero(t, dialer.calls.Load(), "broken binding must stop before the independent WS dialer")
				}
			case <-ctx.Done():
				t.Fatal("local passthrough did not terminate")
			}
		})
	}
}

func TestR1ImageDownloadRequiresBoundProxy(t *testing.T) {
	id := int64(1)
	for _, valid := range []bool{false, true} {
		account := b64BackfillAccount(true)
		account.ProxyID = &id
		if valid {
			account.Proxy = gapValidProxy()
		}
		calls := 0
		upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
			calls++
			require.Equal(t, "", req.Header.Get("Authorization"))
			require.Equal(t, "", req.Header.Get("Cookie"))
			if valid {
				require.Equal(t, account.Proxy.URL(), proxy)
			}
			return b64BackfillImageResponse(http.StatusOK, "image/png", b64BackfillPNGBytes), nil
		}}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		value, err := svc.fetchOpenAIImageURLBase64(context.Background(), account, "https://cdn.example.com/synthetic.png")
		if valid {
			require.NoError(t, err)
			require.NotEmpty(t, value)
			require.Equal(t, 1, calls)
		} else {
			require.Error(t, err)
			require.Empty(t, value)
			require.Zero(t, calls)
		}
	}
}

func TestR1ImageDownloadUsesParentProxy(t *testing.T) {
	id := int64(1)
	parent := gapQuotaAccount(&id, gapValidProxy())
	parentID := parent.ID
	shadow := &Account{ID: 8009, ParentAccountID: &parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, QuotaDimension: QuotaDimensionSpark}
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{parent.ID: parent, shadow.ID: shadow}}
	seen := ""
	calls := 0
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
		calls++
		seen = proxy
		require.Empty(t, req.Header.Get("Authorization"))
		return b64BackfillImageResponse(http.StatusOK, "image/png", b64BackfillPNGBytes), nil
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: repo}
	_, err := svc.fetchOpenAIImageURLBase64(context.Background(), shadow, "https://cdn.example.com/synthetic.png")
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, parent.Proxy.URL(), seen)
}
