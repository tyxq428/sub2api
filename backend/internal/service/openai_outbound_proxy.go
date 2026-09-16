package service

import (
	"context"
	"crypto/sha256"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

// resolveOpenAIAccountProxyURL preserves an explicit account proxy binding.
// An unbound account retains the caller's existing default-route policy; a
// broken binding must never be converted into an empty/default proxy URL.
// The caller must resolve shadow credentials first and call this before token
// acquisition. It does not mutate account state. Legacy expiry/fallback policies
// are unchanged; the opt-in required-proxy policy rejects expired/fallback routes.
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
	required, policyErr := openAIProxyRequired(account)
	if policyErr != nil {
		return "", policyErr
	}
	if account.ProxyID == nil {
		if required {
			return "", infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_REQUIRED", "this account requires an explicit proxy binding")
		}
		return "", nil
	}
	if required && account.ProxyFallbackOriginID != nil {
		return "", infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_FALLBACK_FORBIDDEN", "automatic proxy fallback is not allowed for this account")
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
	if required && (!route.IsActive() || route.IsExpired(time.Now())) {
		return "", infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_UNAVAILABLE", "required proxy is inactive or expired")
	}
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
	if account == nil || account.Platform != PlatformOpenAI {
		return candidate, nil
	}
	required, err := openAIProxyRequired(account)
	if err != nil {
		return "", err
	}
	if account.ProxyID == nil && !required {
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

// ResolveOpenAIProxyBinding validates an already looked-up proxy for handler
// boundaries. Repository errors must be handled before calling this function.
// The same binding validation is used by service and handler paths.
func ResolveOpenAIProxyBinding(ctx context.Context, id *int64, proxy *Proxy) (string, error) {
	return resolveOpenAIAccountProxyURL(ctx, &Account{ProxyID: id, Proxy: proxy}, nil)
}

// resolveOpenAIRequestProxyURL is for auxiliary entrypoints that do not pass
// through doOpenAIUpstream or the pooled WS dialer. It selects the credential
// owner's route while leaving non-OpenAI account routing unchanged.
func (s *OpenAIGatewayService) resolveOpenAIRequestProxyURL(ctx context.Context, account *Account) (string, error) {
	if account == nil {
		return resolveOpenAIAccountProxyURL(ctx, nil, nil)
	}
	if account.Platform != PlatformOpenAI {
		return resolveAccountProxyURL(account), nil
	}
	routeAccount := account
	if account.IsShadow() {
		if s == nil || s.accountRepo == nil {
			return "", infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_UNAVAILABLE", "credential owner route is unavailable")
		}
		var err error
		routeAccount, err = resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return "", err
		}
	}
	return resolveOpenAIAccountProxyURL(ctx, routeAccount, nil)
}

// openAIProxyRequired is opt-in and lives in the existing Extra JSON object.
// A malformed present flag is a policy error, never an implicit opt-out.
// It validates the supplied account snapshot; it is not a real-time DB revoke check.
func openAIProxyRequired(account *Account) (bool, error) {
	if account == nil || account.Platform != PlatformOpenAI || account.Extra == nil {
		return false, nil
	}
	raw, exists := account.Extra["openai_proxy_required"]
	if !exists {
		return false, nil
	}
	required, ok := raw.(bool)
	if !ok {
		return false, infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_POLICY_INVALID", "openai_proxy_required must be a boolean")
	}
	return required, nil
}

// Unverified external plugins must not take over required-route accounts. This
// rejects the selected route rather than silently bypassing the configured plugin.
func validateOpenAIPluginProxyPolicy(account *Account) error {
	required, err := openAIProxyRequired(account)
	if err != nil {
		return err
	}
	if required {
		return infraerrors.New(http.StatusBadGateway, "OPENAI_PROXY_PLUGIN_UNVERIFIED", "selected plugin has not been verified for required proxy routing")
	}
	return nil
}

// Administrative operations intentionally reload the proxy record while
// retaining the account policy and fallback provenance. A synthetic ID-only
// account would otherwise drop openai_proxy_required during refresh/privacy.
func resolveOpenAIAccountProxyURLFresh(ctx context.Context, account *Account, repo ProxyRepository) (string, error) {
	if account == nil {
		return resolveOpenAIAccountProxyURL(ctx, nil, repo)
	}
	copied := *account
	copied.Proxy = nil
	return resolveOpenAIAccountProxyURL(ctx, &copied, repo)
}
