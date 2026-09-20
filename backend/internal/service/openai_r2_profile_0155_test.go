//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexwire"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type r22ExistingBindingStore struct {
	binding     *CodexR2PolicyBinding
	wireBinding *CodexR2WireContractBinding
}

func (s *r22ExistingBindingStore) GetBinding(context.Context, int64, string, string) (*CodexR2PolicyBinding, error) {
	return s.binding, nil
}

func (s *r22ExistingBindingStore) Admit(context.Context, CodexR2Admission) (*CodexR2PolicyBinding, bool, error) {
	panic("legacy binding test must not admit a new session")
}

func (s *r22ExistingBindingStore) WriteBatch(context.Context, []codexidentity.Event) error {
	return nil
}

func (s *r22ExistingBindingStore) AdmitWithWireContract(
	context.Context,
	CodexR2Admission,
	CodexR2WireContractAdmission,
) (*CodexR2PolicyBinding, *CodexR2WireContractBinding, bool, error) {
	panic("existing binding test must not admit a new R2.2 session")
}

func (s *r22ExistingBindingStore) GetWireContractBinding(context.Context, int64) (*CodexR2WireContractBinding, error) {
	return s.wireBinding, nil
}

func TestPrepareCodexR2AttemptAcceptsExplicit0155Profile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.ReferenceProfile = codexidentity.Codex0155ProfileID
	cfg.Gateway.CodexR2.ShadowTelemetry = false
	svc := &OpenAIGatewayService{cfg: cfg}
	account := &Account{
		ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"chatgpt_account_id": "synthetic-account"},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: 77})
	headers := http.Header{
		"User-Agent":          {"codex-tui/0.155.1 (Windows 10.0.26200; x86_64) WindowsTerminal"},
		"Originator":          {"codex-tui"},
		"Version":             {"0.155.1"},
		"Session-Id":          {"0199a1b2-c3d4-7e5f-8a9b-111111111111"},
		"Thread-Id":           {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
		"X-Client-Request-Id": {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
	}
	body := []byte(`{"prompt_cache_key":"0199a1b2-c3d4-7e5f-8a9b-111111111111","client_metadata":{"session_id":"0199a1b2-c3d4-7e5f-8a9b-111111111111","thread_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222"}}`)

	attempt, err := svc.prepareCodexR2Attempt(context.Background(), c, account, headers, body, codexidentity.PurposeInference)
	require.NoError(t, err)
	require.NotNil(t, attempt)
	require.Equal(t, codexidentity.ModeShadow, attempt.Policy.Mode)
	require.Equal(t, codexidentity.Codex0155ProfileID, attempt.Policy.ReferenceProfile)
	require.Equal(t, codexidentity.Codex0155ProfileID, attempt.Plan.ProfileID)
	require.True(t, attempt.Plan.Client.Recognized)
	require.Equal(t, "0.155.1", attempt.Plan.Client.Version)
	require.NoError(t, attempt.Plan.Validate())
}

func TestPrepareCodexR2AttemptCompatibilityProfileAcceptsQualifiedMixedVersions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		version string
		wantID  string
	}{
		{version: "0.154.0", wantID: codexidentity.Codex0154ProfileID},
		{version: "0.155.1", wantID: codexidentity.Codex0155ProfileID},
	} {
		t.Run(tc.version, func(t *testing.T) {
			cfg := syntheticR2Config()
			cfg.Gateway.CodexR2.ReferenceProfile = codexidentity.Codex0154To0155CompatibilityID
			cfg.Gateway.CodexR2.ShadowTelemetry = false
			svc := &OpenAIGatewayService{cfg: cfg}
			account := &Account{
				ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"chatgpt_account_id": "synthetic-account"},
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set("api_key", &APIKey{ID: 77})
			headers := http.Header{
				"User-Agent":          {"codex-tui/" + tc.version + " (Windows 10.0.26200; x86_64) WindowsTerminal"},
				"Originator":          {"codex-tui"},
				"Version":             {tc.version},
				"Session-Id":          {"0199a1b2-c3d4-7e5f-8a9b-111111111111"},
				"Thread-Id":           {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
				"X-Client-Request-Id": {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
			}
			body := []byte(`{"prompt_cache_key":"0199a1b2-c3d4-7e5f-8a9b-111111111111","client_metadata":{"session_id":"0199a1b2-c3d4-7e5f-8a9b-111111111111","thread_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222"}}`)
			attempt, err := svc.prepareCodexR2Attempt(context.Background(), c, account, headers, body, codexidentity.PurposeInference)
			require.NoError(t, err)
			require.NotNil(t, attempt)
			require.Equal(t, tc.wantID, attempt.Plan.ProfileID)
			require.Equal(t, tc.version, attempt.Plan.Client.Version)
		})
	}
}

func TestR22EnforceDoesNotReinterpretExistingR2V1BindingWithIncomplete0155Evidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.Mode = config.CodexR2ModeEnforce
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeEnforce
	cfg.Gateway.CodexR2.ReferenceProfile = codexidentity.Codex0155ProfileID
	cfg.Gateway.CodexR2.ShadowTelemetry = false

	account := &Account{
		ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"chatgpt_account_id": "synthetic-account"},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: 77})
	headers := http.Header{
		"User-Agent":          {"codex-tui/0.155.1 (Windows 10.0.26200; x86_64) WindowsTerminal"},
		"Originator":          {"codex-tui"},
		"Version":             {"0.155.1"},
		"Session-Id":          {"0199a1b2-c3d4-7e5f-8a9b-111111111111"},
		"Thread-Id":           {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
		"X-Client-Request-Id": {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
	}
	body := []byte(`{"prompt_cache_key":"0199a1b2-c3d4-7e5f-8a9b-111111111111","client_metadata":{"session_id":"0199a1b2-c3d4-7e5f-8a9b-111111111111","thread_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222"}}`)
	credentialScope := codexAccountIdentityNamespace(codexAccountIdentitySource(c, account))
	store := &r22ExistingBindingStore{binding: &CodexR2PolicyBinding{
		ID:               55,
		AccountID:        account.ID,
		ProfileRevision:  codexidentity.Codex0155ProfileID,
		PolicyRevision:   codexR2PolicyRevision,
		MappingAlgorithm: codexR2MappingAlgorithm,
		MappingKeyEpoch:  cfg.Gateway.CodexR2.MappingKeyEpoch,
		NamespaceDigest:  codexR2Digest([]byte(cfg.Gateway.CodexR2.MappingHMACKey), "credential-scope", credentialScope),
		UAPolicy:         config.CodexR2ClientUAModePreserveValidated,
		Status:           CodexR2BindingActive,
	}}
	svc := &OpenAIGatewayService{cfg: cfg, codexR2State: store}

	attempt, err := svc.prepareCodexR2Attempt(context.Background(), c, account, headers, body, codexidentity.PurposeInference)
	require.NoError(t, err)
	require.NotNil(t, attempt)
	require.Equal(t, codexR2PolicyRevision, attempt.PolicyRevision)
	require.Equal(t, config.CodexR2WireModeOff, attempt.WireMode)
	require.Nil(t, attempt.WireContract)
	require.Equal(t, codexidentity.Codex0155ProfileID, attempt.Plan.ProfileID)
}

func TestExistingR22BindingKeepsWireValidationWhenGlobalWireModeIsOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := syntheticR2Config()
	cfg.Gateway.CodexR2.Mode = config.CodexR2ModeEnforce
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeOff
	cfg.Gateway.CodexR2.ReferenceProfile = codexidentity.Codex0154ProfileID
	cfg.Gateway.CodexR2.ShadowTelemetry = false

	account := &Account{
		ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"chatgpt_account_id": "synthetic-account"},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: 77})
	headers := http.Header{
		"User-Agent":            {"codex-tui/0.154.0 (Windows 10.0.26200; x86_64) WindowsTerminal"},
		"Originator":            {"codex-tui"},
		"Version":               {"0.154.0"},
		"Session-Id":            {"0199a1b2-c3d4-7e5f-8a9b-111111111111"},
		"Thread-Id":             {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
		"X-Client-Request-Id":   {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
		"X-Codex-Turn-Metadata": {"{malformed"},
	}
	body := []byte(`{"prompt_cache_key":"0199a1b2-c3d4-7e5f-8a9b-111111111111","client_metadata":{"session_id":"0199a1b2-c3d4-7e5f-8a9b-111111111111","thread_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222"}}`)
	credentialScope := codexAccountIdentityNamespace(codexAccountIdentitySource(c, account))
	contract, ok := codexwire.ContractForProfile(codexidentity.Codex0154ProfileID)
	require.True(t, ok)
	store := &r22ExistingBindingStore{
		binding: &CodexR2PolicyBinding{
			ID:               56,
			AccountID:        account.ID,
			ProfileRevision:  codexidentity.Codex0154ProfileID,
			PolicyRevision:   codexR22WirePolicyRevision,
			MappingAlgorithm: codexR2MappingAlgorithm,
			MappingKeyEpoch:  cfg.Gateway.CodexR2.MappingKeyEpoch,
			NamespaceDigest:  codexR2Digest([]byte(cfg.Gateway.CodexR2.MappingHMACKey), "credential-scope", credentialScope),
			UAPolicy:         config.CodexR2ClientUAModePreserveValidated,
			Status:           CodexR2BindingActive,
		},
		wireBinding: &CodexR2WireContractBinding{
			BindingID:       56,
			ContractID:      contract.ID,
			ContractSHA256:  contract.Digest(),
			ReferenceCommit: contract.ReferenceCommit,
			GraphRevision:   contract.GraphRevision,
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg, codexR2State: store}

	_, err := svc.prepareCodexR2Attempt(context.Background(), c, account, headers, body, codexidentity.PurposeInference)
	require.ErrorContains(t, err, "invalid canonical codex turn metadata header")
}
