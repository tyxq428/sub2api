package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
)

const codexR2ReasonOff = "configured_off"

type codexR2EffectivePolicy struct {
	Mode             codexidentity.Mode
	ClientUAMode     string
	ReferenceProfile string
	ShadowTelemetry  bool
	Reason           string
}

func resolveCodexR2EffectivePolicy(cfg *config.Config, account *Account) codexR2EffectivePolicy {
	policy := codexR2EffectivePolicy{
		Mode:             codexidentity.ModeOff,
		ClientUAMode:     config.CodexR2ClientUAModeLegacyCanonical,
		ReferenceProfile: "codex-0.154-profile-r1",
		Reason:           codexR2ReasonOff,
	}
	if cfg == nil {
		policy.Reason = "config_missing"
		return policy
	}
	r2 := cfg.Gateway.CodexR2
	policy.ClientUAMode = config.NormalizeCodexR2ClientUAMode(r2.ClientUAMode)
	if profile := strings.TrimSpace(r2.ReferenceProfile); profile != "" {
		policy.ReferenceProfile = profile
	}
	policy.ShadowTelemetry = r2.ShadowTelemetry
	mode := codexidentity.ParseMode(r2.Mode)
	if mode == codexidentity.ModeOff {
		return policy
	}
	if account == nil || !account.IsOpenAI() || account.Type != AccountTypeOAuth {
		policy.Reason = "unsupported_account_type"
		return policy
	}
	if !containsCodexR2AccountID(r2.EligibleAccountIDs, account.ID) {
		policy.Reason = "account_not_allowlisted"
		return policy
	}
	if account.GetCodexFingerprintMode() != codexFingerprintOff {
		policy.Reason = "fingerprint_convergence_conflict"
		return policy
	}
	if strings.TrimSpace(account.GetOpenAIUserAgent()) != "" {
		policy.Reason = "custom_ua_conflict"
		return policy
	}
	if cfg.Gateway.ForceCodexCLI {
		policy.Reason = "force_codex_cli_conflict"
		return policy
	}
	if len([]byte(r2.MappingHMACKey)) < 32 {
		policy.Reason = "mapping_key_missing"
		return policy
	}
	if strings.TrimSpace(r2.MappingKeyEpoch) == "" {
		policy.Reason = "mapping_key_epoch_missing"
		return policy
	}
	policy.Mode = mode
	policy.Reason = "explicit_r2_policy"
	return policy
}

func containsCodexR2AccountID(ids []int64, accountID int64) bool {
	if accountID <= 0 {
		return false
	}
	for _, id := range ids {
		if id == accountID {
			return true
		}
	}
	return false
}

func codexR2SnapshotLimits(cfg *config.Config) codexidentity.Limits {
	if cfg == nil {
		return codexidentity.DefaultLimits()
	}
	r2 := cfg.Gateway.CodexR2
	return codexidentity.Limits{
		MaxMetadataBytes:       r2.MaxMetadataBytes,
		MaxIdentityHeaderBytes: r2.MaxIdentityHeaderBytes,
		MaxMetadataDepth:       r2.MaxMetadataDepth,
		MaxIdentityValueBytes:  r2.MaxIdentityValueBytes,
		MaxUserAgentBytes:      r2.MaxUserAgentBytes,
	}
}
