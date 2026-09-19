//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
	"github.com/stretchr/testify/require"
)

func syntheticR2Config() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.CodexR2 = config.GatewayCodexR2Config{
		Mode:                   config.CodexR2ModeShadow,
		ClientUAMode:           config.CodexR2ClientUAModePreserveValidated,
		ReferenceProfile:       "codex-0.154-profile-r1",
		EligibleAccountIDs:     []int64{8},
		MappingHMACKey:         "01234567890123456789012345678901",
		MappingKeyEpoch:        "epoch-1",
		MaxMetadataBytes:       256 * 1024,
		MaxIdentityHeaderBytes: 16 * 1024,
		MaxMetadataDepth:       32,
		MaxIdentityValueBytes:  1024,
		MaxUserAgentBytes:      1024,
	}
	return cfg
}

func TestResolveCodexR2PolicyDefaultsOffAndDoesNotInheritR1(t *testing.T) {
	account := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexR1CanaryExtraKey: true}}
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.Mode = config.CodexR2ModeOff
	policy := resolveCodexR2EffectivePolicy(cfg, account)
	require.Equal(t, codexidentity.ModeOff, policy.Mode)
	require.Equal(t, codexR2ReasonOff, policy.Reason)
}

func TestResolveCodexR2PolicyRequiresExplicitOAuthAccountAllowlist(t *testing.T) {
	cfg := syntheticR2Config()
	policy := resolveCodexR2EffectivePolicy(cfg, &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.Equal(t, codexidentity.ModeShadow, policy.Mode)
	require.Equal(t, "explicit_r2_policy", policy.Reason)
	require.Equal(t, config.CodexR2ClientUAModePreserveValidated, policy.ClientUAMode)

	notListed := resolveCodexR2EffectivePolicy(cfg, &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.Equal(t, codexidentity.ModeOff, notListed.Mode)
	require.Equal(t, "account_not_allowlisted", notListed.Reason)

	apiKey := resolveCodexR2EffectivePolicy(cfg, &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeAPIKey})
	require.Equal(t, codexidentity.ModeOff, apiKey.Mode)
	require.Equal(t, "unsupported_account_type", apiKey.Reason)
}

func TestResolveCodexR2PolicyRejectsFingerprintConvergenceConflict(t *testing.T) {
	cfg := syntheticR2Config()
	account := &Account{
		ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Extra: map[string]any{codexFingerprintModeExtraKey: string(codexFingerprintSession)},
	}
	policy := resolveCodexR2EffectivePolicy(cfg, account)
	require.Equal(t, codexidentity.ModeOff, policy.Mode)
	require.Equal(t, "fingerprint_convergence_conflict", policy.Reason)
}

func TestResolveCodexR2EnforceAdmissionSwitchDoesNotChangeExistingSessionPolicy(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.Mode = config.CodexR2ModeEnforce
	account := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	policy := resolveCodexR2EffectivePolicy(cfg, account)
	require.Equal(t, codexidentity.ModeEnforce, policy.Mode)
	require.Equal(t, "explicit_r2_policy", policy.Reason)

	cfg.Gateway.CodexR2.NewSessionAdmission = true
	policy = resolveCodexR2EffectivePolicy(cfg, account)
	require.Equal(t, codexidentity.ModeEnforce, policy.Mode)
	require.Equal(t, "explicit_r2_policy", policy.Reason)
}

func TestResolveCodexR2EnforceRequiresMappingKeyMaterial(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.Mode = config.CodexR2ModeEnforce
	cfg.Gateway.CodexR2.NewSessionAdmission = true
	account := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	cfg.Gateway.CodexR2.MappingHMACKey = ""
	policy := resolveCodexR2EffectivePolicy(cfg, account)
	require.Equal(t, codexidentity.ModeOff, policy.Mode)
	require.Equal(t, "mapping_key_missing", policy.Reason)

	cfg.Gateway.CodexR2.MappingHMACKey = "01234567890123456789012345678901"
	cfg.Gateway.CodexR2.MappingKeyEpoch = ""
	policy = resolveCodexR2EffectivePolicy(cfg, account)
	require.Equal(t, codexidentity.ModeOff, policy.Mode)
	require.Equal(t, "mapping_key_epoch_missing", policy.Reason)
}

func TestCodexR2SnapshotLimitsUseDefaultsForZeroValues(t *testing.T) {
	require.Equal(t, codexidentity.DefaultLimits(), codexR2SnapshotLimits(nil))
	limits := codexR2SnapshotLimits(&config.Config{})
	// Capture normalizes these zero values to its immutable defaults.
	snapshot := codexidentity.Capture(nil, nil, limits)
	require.True(t, snapshot.Coverage().Complete)
}
