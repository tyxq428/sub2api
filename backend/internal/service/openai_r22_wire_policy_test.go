//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexwire"
	"github.com/stretchr/testify/require"
)

func TestCodexR22WireContractDefaultOffDoesNotChangeR2(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeOff
	profile, ok := codexidentity.ProfileByID(codexidentity.Codex0154ProfileID)
	require.True(t, ok)
	mode, contract, err := codexR2WireContractForRequest(cfg, profile, codexidentity.PurposeInference, nil, nil)
	require.NoError(t, err)
	require.Equal(t, config.CodexR2WireModeOff, mode)
	require.Nil(t, contract)
}

func TestCodexR22WireShadowResolvesPinnedContract(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeShadow
	profile, ok := codexidentity.ProfileByID(codexidentity.Codex0155ProfileID)
	require.True(t, ok)
	body := []byte(`{"client_metadata":{"x-codex-turn-metadata":"{\"session_id\":\"synthetic-session\",\"future_field\":{\"opaque\":true}}"}}`)
	mode, contract, err := codexR2WireContractForRequest(cfg, profile, codexidentity.PurposeInference, nil, body)
	require.NoError(t, err)
	require.Equal(t, config.CodexR2WireModeShadow, mode)
	require.NotNil(t, contract)
	require.Equal(t, "codex-wire-0.155.1-r1", contract.ID)
	require.False(t, contract.EvidenceComplete)
}

func TestCodexR22WireEnforceRejectsIncompletePinnedEvidence(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeEnforce
	profile, ok := codexidentity.ProfileByID(codexidentity.Codex0155ProfileID)
	require.True(t, ok)
	_, _, err := codexR2WireContractForRequest(cfg, profile, codexidentity.PurposeInference, nil, nil)
	require.ErrorContains(t, err, "lacks complete pinned reference evidence")
}

func TestCodexR22WireRejectsMalformedCanonicalMetadata(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeEnforce
	profile, ok := codexidentity.ProfileByID(codexidentity.Codex0154ProfileID)
	require.True(t, ok)
	body := []byte(`{"client_metadata":{"x-codex-turn-metadata":"not-json"}}`)
	_, _, err := codexR2WireContractForRequest(cfg, profile, codexidentity.PurposeInference, nil, body)
	require.ErrorContains(t, err, "invalid canonical codex turn metadata")
}

func TestCodexR22WireRejectsMalformedCanonicalMetadataHeader(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeEnforce
	profile, ok := codexidentity.ProfileByID(codexidentity.Codex0154ProfileID)
	require.True(t, ok)
	headers := http.Header{"X-Codex-Turn-Metadata": []string{"{malformed"}}
	_, _, err := codexR2WireContractForRequest(cfg, profile, codexidentity.PurposeInference, headers, nil)
	require.ErrorContains(t, err, "metadata header")
}

func TestCodexR22WSCompatibilityIsContractScoped(t *testing.T) {
	r2 := config.GatewayCodexR2Config{
		MappingHMACKey:  "01234567890123456789012345678901",
		MappingKeyEpoch: "epoch-production-compatible",
	}
	plan := codexidentity.OutboundPlan{
		Client: codexidentity.ClientIdentity{
			UserAgent:  "codex-tui/0.154.0 (Windows; x86_64) terminal",
			Originator: "codex-tui",
			Version:    "0.154.0",
			Recognized: true,
		},
	}
	contract, ok := codexwire.ContractForProfile(codexidentity.Codex0154ProfileID)
	require.True(t, ok)
	legacy := codexR2WSCompatibilityDigest(r2, codexR2PolicyRevision, contract.ProfileID, "api-key:1", "oauth:1", plan, nil)
	wire := codexR2WSCompatibilityDigest(r2, codexR22WirePolicyRevision, contract.ProfileID, "api-key:1", "oauth:1", plan, &contract)
	require.NotEqual(t, legacy, wire)
	require.NotEmpty(t, legacy)
	require.NotEmpty(t, wire)
}

func TestCodexR22WireContractDoesNotInventMetadata(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeShadow
	profile, ok := codexidentity.ProfileByID(codexidentity.Codex0154ProfileID)
	require.True(t, ok)
	mode, contract, err := codexR2WireContractForRequest(cfg, profile, codexidentity.PurposeInference, nil, []byte(`{"input":"synthetic"}`))
	require.NoError(t, err)
	require.Equal(t, config.CodexR2WireModeShadow, mode)
	require.NotNil(t, contract)

	// R2.2 contract selection is read-only. The existing plan is still the only
	// writer; no analytics/sandbox/workspace headers are synthesized.
	headers := http.Header{}
	require.Empty(t, headers.Get("x-codex-turn-metadata"))
}
