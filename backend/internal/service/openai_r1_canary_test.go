package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexR1CanaryGateDefaultsOffAndRequiresBooleanTrue(t *testing.T) {
	t.Setenv(OpenAICodexR1CanaryAccountIDsEnv, "")
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{"nil", nil, false},
		{"missing", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, false},
		{"false", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexR1CanaryExtraKey: false}}, false},
		{"string_true_rejected", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexR1CanaryExtraKey: "true"}}, false},
		{"other_platform_rejected", &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexR1CanaryExtraKey: true}}, false},
		{"openai_true", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexR1CanaryExtraKey: true}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.IsCodexR1CanaryEnabled())
		})
	}
}

func TestCodexR1CanaryDeploymentAllowlistIsSelectiveAndFailClosed(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"unset_equivalent", "", false},
		{"whitespace", "   ", false},
		{"single_match", "42", true},
		{"multiple_match", "7, 42, 99", true},
		{"other_ids", "7,41,99", false},
		{"empty_token_fails_closed", "42,,99", false},
		{"leading_empty_token_fails_closed", ",42", false},
		{"trailing_empty_token_fails_closed", "42,", false},
		{"non_numeric_fails_closed", "42,nope", false},
		{"zero_fails_closed", "42,0", false},
		{"negative_fails_closed", "42,-1", false},
		{"overflow_fails_closed", "42,9223372036854775808", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(OpenAICodexR1CanaryAccountIDsEnv, tt.raw)
			require.Equal(t, tt.want, account.IsCodexR1CanaryEnabled())
		})
	}
}

func TestCodexR1CanaryDeploymentAllowlistStillRequiresOpenAIAndPositiveAccountID(t *testing.T) {
	t.Setenv(OpenAICodexR1CanaryAccountIDsEnv, "42")
	require.False(t, (&Account{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeOAuth}).IsCodexR1CanaryEnabled())
	require.False(t, (&Account{ID: 0, Platform: PlatformOpenAI, Type: AccountTypeOAuth}).IsCodexR1CanaryEnabled())
	require.False(t, (&Account{ID: -42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}).IsCodexR1CanaryEnabled())
}

func TestCodexR1CanaryExtraTrueRemainsPrimaryOptIn(t *testing.T) {
	t.Setenv(OpenAICodexR1CanaryAccountIDsEnv, "malformed")
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{OpenAICodexR1CanaryExtraKey: true},
	}
	require.True(t, account.IsCodexR1CanaryEnabled())
}

func TestCodexR1CanaryStringExtraDoesNotBypassDeploymentAllowlist(t *testing.T) {
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{OpenAICodexR1CanaryExtraKey: "true"},
	}
	t.Setenv(OpenAICodexR1CanaryAccountIDsEnv, "")
	require.False(t, account.IsCodexR1CanaryEnabled())
	t.Setenv(OpenAICodexR1CanaryAccountIDsEnv, "42")
	require.True(t, account.IsCodexR1CanaryEnabled())
}

func TestCodexR1CanaryIdentityIsAccountScoped(t *testing.T) {
	SetCodexCanonicalUserAgentResolver(nil)
	t.Cleanup(func() { SetCodexCanonicalUserAgentResolver(nil) })
	legacy := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	canary := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexR1CanaryExtraKey: true}}

	legacyIdentity := resolveCodexOutboundIdentityForAccount(legacy, "")
	require.Equal(t, "0.146.0", legacyIdentity.version)
	require.Contains(t, legacyIdentity.userAgent, "Ubuntu 22.4.0")
	require.NotContains(t, legacyIdentity.userAgent, "Ubuntu 24.04.3")

	canaryIdentity := resolveCodexOutboundIdentityForAccount(canary, "")
	require.Equal(t, "0.154.0", canaryIdentity.version)
	require.Contains(t, canaryIdentity.userAgent, "Ubuntu 24.04.3")
	require.NotContains(t, canaryIdentity.userAgent, "Ubuntu 22.4.0")
}

func TestCodexR1CanaryDoesNotDisableLegacyFingerprintOptIn(t *testing.T) {
	legacy := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			codexFingerprintModeExtraKey: "session",
		},
	}
	require.False(t, legacy.IsCodexR1CanaryEnabled())
	require.Equal(t, codexFingerprintSession, legacy.GetCodexFingerprintMode())
}

func TestCodexR1CanaryIgnoresGlobalCanonicalResolver(t *testing.T) {
	SetCodexCanonicalUserAgentResolver(func() string {
		return "codex-tui/0.200.0 (Ubuntu 22.4.0; x86_64) xterm-256color"
	})
	t.Cleanup(func() { SetCodexCanonicalUserAgentResolver(nil) })

	legacy := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	canary := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexR1CanaryExtraKey: true}}
	require.Equal(t, "0.200.0", CodexCanonicalClientVersionForAccount(legacy))
	require.Equal(t, "0.154.0", CodexCanonicalClientVersionForAccount(canary))
}

func TestCodexR1CanaryCredentialContextCarriesOnlyRolloutBit(t *testing.T) {
	legacy := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	canary := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexR1CanaryExtraKey: true}}
	require.False(t, codexR1CanaryFromContext(withCodexR1CanaryAccountContext(context.Background(), legacy)))
	ctx := withCodexR1CanaryAccountContext(context.Background(), canary)
	require.True(t, codexR1CanaryFromContext(ctx))
	ua, originator := CodexAuthIdentityForContext(ctx)
	require.Contains(t, ua, "/0.154.0 ")
	require.Equal(t, "codex-tui", originator)
}
