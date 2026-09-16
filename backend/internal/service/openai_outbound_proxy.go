package service

import (
	"context"
	"crypto/sha256"
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
// Coverage is tracked per entry point in docs/codex-gap; this is not a global network sandbox.
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

// resolveOpenAIProxyIDURL deliberately reloads a binding for administrative and
// OAuth operations, preserving their existing preference for current DB state.
func resolveOpenAIProxyIDURL(ctx context.Context, id *int64, repo ProxyRepository) (string, error) {
	return resolveOpenAIAccountProxyURL(ctx, &Account{ProxyID: id}, repo)
}

// validateOpenAIProxyURL is for APIs with a URL snapshot, not a database ID.
// It cannot discover whether a previously captured binding has been revoked.
func validateOpenAIProxyURL(raw string) (string, error) {
	normalized, _, err := proxyurl.Parse(raw)
	if err != nil {
		return "", infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_INVALID", "configured OpenAI proxy is invalid")
	}
	return normalized, nil
}

// resolveOpenAIDispatchProxyURL protects the actual dispatch boundary. Explicit
// OpenAI account bindings are authoritative; unbound/other-provider policies
// remain unchanged. Selected accounts must already carry their hydrated proxy.
func resolveOpenAIDispatchProxyURL(ctx context.Context, account *Account, candidate string) (string, error) {
	if account == nil || account.Platform != PlatformOpenAI || account.ProxyID == nil {
		return candidate, nil
	}
	return resolveOpenAIAccountProxyURL(ctx, account, nil)
}

// openAIProxyBindingHash distinguishes physical connections without storing
// proxy URLs or credentials in a printable handshake compatibility key.
func openAIProxyBindingHash(account *Account) [32]byte {
	if account == nil || account.Platform != PlatformOpenAI || account.ProxyID == nil {
		return [32]byte{}
	}
	route, err := resolveOpenAIAccountProxyURL(context.Background(), account, nil)
	if err != nil {
		return [32]byte{}
	} // rejected before pool reuse or dial
	return sha256.Sum256([]byte(route))
}
