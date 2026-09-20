package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func validR2ConfigForWireTest() GatewayCodexR2Config {
	return GatewayCodexR2Config{
		Mode:                   CodexR2ModeEnforce,
		ClientUAMode:           CodexR2ClientUAModePreserveValidated,
		ReferenceProfile:       "codex-0.154-profile-r1",
		EligibleAccountIDs:     []int64{8},
		MappingHMACKey:         strings.Repeat("m", 32),
		MappingKeyEpoch:        "epoch-1",
		MaxMetadataBytes:       256 * 1024,
		MaxIdentityHeaderBytes: 16 * 1024,
		MaxMetadataDepth:       32,
		MaxIdentityValueBytes:  1024,
		MaxUserAgentBytes:      1024,
		ObserverQueueCapacity:  4096,
		ObserverEventMaxBytes:  16 * 1024,
		ObserverBatchSize:      128,
		ObserverFlushSeconds:   5,
	}
}

func TestNormalizeCodexR2WireModeDefaultsOff(t *testing.T) {
	require.Equal(t, CodexR2WireModeOff, NormalizeCodexR2WireMode(""))
	require.Equal(t, CodexR2WireModeOff, NormalizeCodexR2WireMode("unknown"))
	require.Equal(t, CodexR2WireModeShadow, NormalizeCodexR2WireMode(" SHADOW "))
	require.Equal(t, CodexR2WireModeEnforce, NormalizeCodexR2WireMode("Enforce"))
}

func TestValidateCodexR2WireModeRequiresActiveR2(t *testing.T) {
	cfg := validR2ConfigForWireTest()
	cfg.Mode = CodexR2ModeOff
	cfg.WireContractMode = CodexR2WireModeShadow
	require.ErrorContains(t, validateCodexR2Config(cfg), "requires R2 mode to be active")
}

func TestValidateCodexR2WireEnforceRequiresR2Enforce(t *testing.T) {
	cfg := validR2ConfigForWireTest()
	cfg.Mode = CodexR2ModeShadow
	cfg.WireContractMode = CodexR2WireModeEnforce
	require.ErrorContains(t, validateCodexR2Config(cfg), "requires gateway.codex_r2.mode=enforce")
}

func TestValidateCodexR2WireOffRemainsBackwardCompatibleWhenOmitted(t *testing.T) {
	cfg := validR2ConfigForWireTest()
	cfg.WireContractMode = ""
	require.NoError(t, validateCodexR2Config(cfg))
}
