package service

import "context"

// OpenAICodexR1CanaryExtraKey gates the R1 Codex observable compatibility
// surface on an individual OpenAI account. Missing/false is deliberately the
// safe rollout default so an upgrade cannot opt existing accounts in.
const OpenAICodexR1CanaryExtraKey = "codex_r1_canary_enabled"

type codexR1CanaryContextKey struct{}

// IsCodexR1CanaryEnabled reports whether this OpenAI account is explicitly in
// the staged R1 rollout. Only a real JSON boolean true is accepted.
func (a *Account) IsCodexR1CanaryEnabled() bool {
	return a != nil && a.IsOpenAI() && a.getExtraBool(OpenAICodexR1CanaryExtraKey)
}

// withCodexR1CanaryAccountContext carries only the rollout bit into account-
// aware credential refreshes. It never carries account ids or credentials.
func withCodexR1CanaryAccountContext(ctx context.Context, account *Account) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if account == nil || !account.IsCodexR1CanaryEnabled() {
		return ctx
	}
	return context.WithValue(ctx, codexR1CanaryContextKey{}, true)
}

func codexR1CanaryFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(codexR1CanaryContextKey{}).(bool)
	return enabled
}

// CodexAuthIdentityForContext lets the repository credential plane select the
// same staged identity as the account-aware runtime refresh path without
// exposing the Account across package boundaries.
func CodexAuthIdentityForContext(ctx context.Context) (userAgent, originator string) {
	if codexR1CanaryFromContext(ctx) {
		identity := resolveCodexR1OutboundIdentity("")
		return identity.userAgent, identity.originator
	}
	return CodexCanonicalAuthIdentity()
}

func allowOpenAIR1IngressHeader(account *Account, lowerKey string) bool {
	switch lowerKey {
	case "session-id", "conversation-id":
		return account != nil && account.IsCodexR1CanaryEnabled()
	default:
		return true
	}
}
