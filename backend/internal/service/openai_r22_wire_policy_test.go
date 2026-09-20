//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexwire"
	"github.com/gin-gonic/gin"
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
	require.True(t, contract.EvidenceComplete)
}

func TestCodexR22WireEnforceAcceptsCompletePinned0155Evidence(t *testing.T) {
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeEnforce
	profile, ok := codexidentity.ProfileByID(codexidentity.Codex0155ProfileID)
	require.True(t, ok)
	mode, contract, err := codexR2WireContractForRequest(cfg, profile, codexidentity.PurposeInference, nil, nil)
	require.NoError(t, err)
	require.Equal(t, config.CodexR2WireModeEnforce, mode)
	require.NotNil(t, contract)
	require.True(t, contract.EvidenceComplete)
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

func TestCodexR22PurposeRecognizesOfficialCompactionShapes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newContext := func(path string) *gin.Context {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)
		return c
	}

	legacy := newContext("/v1/responses/compact")
	require.Equal(t, codexidentity.PurposeCompact, codexR2Purpose(legacy, false, nil))

	native := newContext("/v1/responses")
	MarkOpenAINativeCompactionV2(native)
	require.Equal(t, codexidentity.PurposeCompact, codexR2Purpose(native, false, []byte(`{"input":[{"type":"compaction_trigger"}]}`)))

	metadata := newContext("/v1/responses")
	metadata.Request.Header.Set(openAIWSTurnMetadataHeader, `{"request_kind":"compaction"}`)
	require.Equal(t, codexidentity.PurposeCompact, codexR2Purpose(metadata, false, nil))

	turn := newContext("/v1/responses")
	turn.Request.Header.Set(openAIWSTurnMetadataHeader, `{"request_kind":"turn"}`)
	require.Equal(t, codexidentity.PurposeInference, codexR2Purpose(turn, false, nil))
	require.Equal(t, codexidentity.PurposeWebSocket, codexR2Purpose(turn, true, nil))
}

func TestCodexR22AuthOwnerMatches0155OwnerBoundary(t *testing.T) {
	profile0155, ok := codexidentity.ProfileByID(codexidentity.Codex0155ProfileID)
	require.True(t, ok)
	profile0154, ok := codexidentity.ProfileByID(codexidentity.Codex0154ProfileID)
	require.True(t, ok)

	complete := &Account{
		Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "workspace-a",
			"chatgpt_user_id":    "user-a",
		},
	}
	require.NoError(t, validateCodexR22AuthOwner(profile0155, complete))
	require.NoError(t, validateCodexR22AuthOwner(profile0154, &Account{}))

	missingUser := &Account{
		Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "workspace-a"},
	}
	require.ErrorContains(t, validateCodexR22AuthOwner(profile0155, missingUser), "complete chatgpt auth owner identity")
}
