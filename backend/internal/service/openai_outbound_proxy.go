package service

import (
	"context"
	"net/http"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

// resolveOpenAIAccountProxyURL preserves an explicit account proxy binding.
// An unbound account retains the caller's existing default-route policy; a
// broken binding must never be converted into an empty/default proxy URL.
// The caller must resolve shadow credentials first and call this before token
// acquisition. It does not mutate account state or apply expiry/fallback policy.
// This first integration covers quota calls, not every OpenAI outbound path.
func resolveOpenAIAccountProxyURL(ctx context.Context, account *Account, repo ProxyRepository) (string, error) {
	unavailable := func() error {
		// Do not wrap repository errors: they can contain connection credentials.
		return infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_UNAVAILABLE", "configured OpenAI proxy binding is unavailable")
	}
	invalid := func() error {
		return infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_INVALID", "configured OpenAI proxy is invalid")
	}
	if account == nil {
		return "", unavailable()
	}
	if account.ProxyID == nil {
		return "", nil
	}
	id := *account.ProxyID
	if id <= 0 {
		return "", unavailable()
	}
	proxy := account.Proxy
	if proxy == nil {
		if repo == nil {
			return "", unavailable()
		}
		var err error
		proxy, err = repo.GetByID(ctx, id)
		if err != nil {
			return "", unavailable()
		}
	}
	if proxy == nil || proxy.ID != id {
		return "", unavailable()
	}
	// Work from a value copy; resolving a route must not rewrite ORM objects.
	route := *proxy
	if strings.TrimSpace(route.Host) == "" || route.Port < 1 || route.Port > 65535 {
		return "", invalid()
	}
	normalized, parsed, err := proxyurl.Parse(route.URL())
	if err != nil || parsed == nil || normalized == "" {
		return "", invalid()
	}
	return normalized, nil
}
