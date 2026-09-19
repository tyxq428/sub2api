package service

import (
	"context"
	"os"
	"strconv"
	"strings"
)

// OpenAICodexR1CanaryExtraKey gates the R1 Codex observable compatibility
// surface on an individual OpenAI account. Missing/false is deliberately the
// safe rollout default so an upgrade cannot opt existing accounts in.
const OpenAICodexR1CanaryExtraKey = "codex_r1_canary_enabled"

// OpenAICodexR1CanaryAccountIDsEnv is an optional deployment-level fallback
// for staged rollouts when mutating account Extra is intentionally unavailable.
// It is default-empty and therefore preserves the Extra-only behavior unless an
// operator explicitly supplies a strict comma-separated list of positive IDs.
const OpenAICodexR1CanaryAccountIDsEnv = "SUB2API_CODEX_R1_CANARY_ACCOUNT_IDS"

type codexR1CanaryContextKey struct{}

// IsCodexR1CanaryEnabled reports whether this OpenAI account is explicitly in
// the staged R1 rollout. A real JSON boolean true remains the primary opt-in;
// the deployment allowlist is an explicit fallback and fails closed if its
// syntax is malformed.
func (a *Account) IsCodexR1CanaryEnabled() bool {
	if a == nil || !a.IsOpenAI() {
		return false
	}
	if a.getExtraBool(OpenAICodexR1CanaryExtraKey) {
		return true
	}
	return codexR1CanaryAccountIDAllowed(os.Getenv(OpenAICodexR1CanaryAccountIDsEnv), a.ID)
}

func codexR1CanaryAccountIDAllowed(raw string, accountID int64) bool {
	if accountID <= 0 {
		return false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	matched := false
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			return false
		}
		id, err := strconv.ParseInt(token, 10, 64)
		if err != nil || id <= 0 {
			return false
		}
		if id == accountID {
			matched = true
		}
	}
	return matched
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
