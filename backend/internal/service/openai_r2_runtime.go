package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexwire"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	codexR2PolicyRevision      = "r2-v1"
	codexR22WirePolicyRevision = "r2.2-wire-v1"
	codexR2MappingAlgorithm    = "hmac-sha256-v1"
	codexR2AttemptContextKey   = "openai_codex_r2_attempt"
)

var (
	ErrCodexR2AdmissionRequired = errors.New("codex r2 session requires explicit admission")
	ErrCodexR2BindingInvalid    = errors.New("codex r2 policy binding is incompatible with the active policy")
)

type codexR2RuntimeStateStore interface {
	GetBinding(ctx context.Context, accountID int64, authScopeDigest, sessionDigest string) (*CodexR2PolicyBinding, error)
	Admit(ctx context.Context, in CodexR2Admission) (*CodexR2PolicyBinding, bool, error)
	WriteBatch(ctx context.Context, events []codexidentity.Event) error
}

type codexR2WireRuntimeStateStore interface {
	AdmitWithWireContract(
		ctx context.Context,
		in CodexR2Admission,
		wire CodexR2WireContractAdmission,
	) (*CodexR2PolicyBinding, *CodexR2WireContractBinding, bool, error)
	GetWireContractBinding(ctx context.Context, bindingID int64) (*CodexR2WireContractBinding, error)
}

type codexR2Attempt struct {
	Policy          codexR2EffectivePolicy
	Purpose         codexidentity.Purpose
	Raw             codexidentity.RawSnapshot
	Plan            codexidentity.OutboundPlan
	Binding         *CodexR2PolicyBinding
	WireBinding     *CodexR2WireContractBinding
	WireContract    *codexwire.Contract
	WireMode        string
	PolicyRevision  string
	WSCompatibility string
}

func codexR2Purpose(c *gin.Context, websocket bool, body []byte) codexidentity.Purpose {
	if websocket {
		return codexidentity.PurposeWebSocket
	}
	if isOpenAIResponsesCompactPath(c) ||
		isOpenAINativeCompactionV2(c) ||
		openAIWSExecutionTurnMetadata(c, body).RequestKind == openAIWSRequestKindCompaction {
		return codexidentity.PurposeCompact
	}
	return codexidentity.PurposeInference
}

func validateCodexR22AuthOwner(profile codexidentity.ProtocolProfile, account *Account) error {
	if profile.ID != codexidentity.Codex0155ProfileID {
		return nil
	}
	if account == nil ||
		strings.TrimSpace(account.GetChatGPTAccountID()) == "" ||
		strings.TrimSpace(account.GetCredential("chatgpt_user_id")) == "" {
		return errors.New("codex r2.2 0.155.1 requires complete chatgpt auth owner identity")
	}
	return nil
}

func codexR2AuthScope(c *gin.Context) string {
	if id := getAPIKeyIDFromContext(c); id > 0 {
		return fmt.Sprintf("api-key:%d", id)
	}
	return ""
}

func codexR2Digest(secret []byte, domain, value string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("sub2api-codex-r2-state-v1\x00"))
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

func codexR2WireContractForRequest(
	cfg *config.Config,
	profile codexidentity.ProtocolProfile,
	purpose codexidentity.Purpose,
	headers http.Header,
	body []byte,
) (string, *codexwire.Contract, error) {
	if cfg == nil {
		return config.CodexR2WireModeOff, nil, nil
	}
	mode := config.NormalizeCodexR2WireMode(cfg.Gateway.CodexR2.WireContractMode)
	if mode == config.CodexR2WireModeOff {
		return mode, nil, nil
	}
	contract, ok := codexwire.ContractForProfile(profile.ID)
	if !ok {
		return mode, nil, fmt.Errorf("no codex r2.2 wire contract for profile %q", profile.ID)
	}
	if err := validateCodexR2WireContractRequest(
		contract, purpose, headers, body, mode == config.CodexR2WireModeEnforce,
	); err != nil {
		return mode, nil, err
	}
	return mode, &contract, nil
}

func validateCodexR2WireContractRequest(
	contract codexwire.Contract,
	purpose codexidentity.Purpose,
	headers http.Header,
	body []byte,
	requireCompleteEvidence bool,
) error {
	if requireCompleteEvidence && !contract.EvidenceComplete {
		return fmt.Errorf("codex r2.2 wire contract %q lacks complete pinned reference evidence", contract.ID)
	}
	if !contract.SupportsPurpose(string(purpose)) {
		return fmt.Errorf("codex r2.2 wire contract %q does not support purpose %q", contract.ID, purpose)
	}
	if raw := strings.TrimSpace(headers.Get("x-codex-turn-metadata")); raw != "" {
		if _, err := codexwire.ParseTurnMetadata(raw); err != nil {
			return fmt.Errorf("invalid canonical codex turn metadata header: %w", err)
		}
	}
	if _, present, err := codexwire.ParseEmbeddedTurnMetadata(body); present && err != nil {
		return fmt.Errorf("invalid canonical codex turn metadata: %w", err)
	}
	return nil
}

func codexR2WSCompatibilityDigest(
	r2 config.GatewayCodexR2Config,
	policyRevision string,
	profileID string,
	authScope string,
	credentialScope string,
	plan codexidentity.OutboundPlan,
	wire *codexwire.Contract,
) string {
	wireDigest := ""
	if wire != nil {
		wireDigest = wire.Digest()
	}
	return codexR2Digest(
		[]byte(r2.MappingHMACKey),
		"ws-compatibility",
		strings.Join([]string{
			policyRevision,
			profileID,
			r2.MappingKeyEpoch,
			authScope,
			credentialScope,
			plan.Client.UserAgent,
			plan.Client.Originator,
			plan.Client.Version,
			wireDigest,
		}, "\x00"),
	)
}

func (s *OpenAIGatewayService) getCodexR2Observer() *codexidentity.Observer {
	if s == nil || s.cfg == nil {
		return nil
	}
	r2 := s.cfg.Gateway.CodexR2
	if !r2.ShadowTelemetry || codexidentity.ParseMode(r2.Mode) == codexidentity.ModeOff || s.codexR2State == nil {
		return nil
	}
	s.codexR2ObserverOnce.Do(func() {
		s.codexR2Observer, s.codexR2ObserverErr = codexidentity.NewObserver(codexidentity.ObserverOptions{
			Mode:          codexidentity.ParseMode(r2.Mode),
			Secret:        []byte(r2.TelemetryHMACKey),
			QueueCapacity: r2.ObserverQueueCapacity,
			MaxEventBytes: r2.ObserverEventMaxBytes,
			BatchSize:     r2.ObserverBatchSize,
			FlushInterval: time.Duration(r2.ObserverFlushSeconds) * time.Second,
			Sink:          s.codexR2State,
			OnWriteError: func(err error) {
				logger.L().Warn("codex_r2_observer_write_failed", zap.Error(err))
			},
		})
		if s.codexR2ObserverErr != nil {
			logger.L().Error("codex_r2_observer_init_failed",
				zap.Int("queue_capacity", r2.ObserverQueueCapacity),
				zap.Int("event_max_bytes", r2.ObserverEventMaxBytes),
				zap.Int("batch_size", r2.ObserverBatchSize),
				zap.Error(s.codexR2ObserverErr),
			)
		}
	})
	if s.codexR2ObserverErr != nil {
		return nil
	}
	return s.codexR2Observer
}

func recordCodexR2Observation(
	observer *codexidentity.Observer,
	accountID int64,
	snapshot codexidentity.RawSnapshot,
	stage codexidentity.Stage,
	purpose codexidentity.Purpose,
	profile string,
	result string,
) {
	if observer == nil {
		return
	}
	recordResult := observer.RecordForAccount(accountID, snapshot, stage, purpose, profile, result)
	if recordResult == codexidentity.RecordEnqueued {
		return
	}
	logger.L().Warn("codex_r2_observer_record_not_enqueued",
		zap.Int64("account_id", accountID),
		zap.String("stage", string(stage)),
		zap.String("purpose", string(purpose)),
		zap.String("result", result),
		zap.String("record_result", string(recordResult)),
	)
}

func stageCodexR2Attempt(c *gin.Context, attempt *codexR2Attempt) {
	if c == nil {
		return
	}
	if attempt == nil {
		c.Set(codexR2AttemptContextKey, (*codexR2Attempt)(nil))
		return
	}
	copyAttempt := *attempt
	copyAttempt.Raw = attempt.Raw.Clone()
	c.Set(codexR2AttemptContextKey, &copyAttempt)
}

func stagedCodexR2Attempt(c *gin.Context) *codexR2Attempt {
	if c == nil {
		return nil
	}
	value, ok := c.Get(codexR2AttemptContextKey)
	if !ok {
		return nil
	}
	attempt, _ := value.(*codexR2Attempt)
	return attempt
}

func (s *OpenAIGatewayService) prepareCodexR2Attempt(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	headers http.Header,
	body []byte,
	purpose codexidentity.Purpose,
) (*codexR2Attempt, error) {
	policy := resolveCodexR2EffectivePolicy(s.cfg, account)
	if policy.Mode == codexidentity.ModeOff {
		stageCodexR2Attempt(c, nil)
		return nil, nil
	}
	raw := codexidentity.Capture(headers, body, codexR2SnapshotLimits(s.cfg))
	profile, ok := codexidentity.ResolveProfileReference(policy.ReferenceProfile, raw)
	if !ok {
		if observer := s.getCodexR2Observer(); observer != nil {
			recordCodexR2Observation(observer, account.ID, raw, codexidentity.StageInput, purpose, policy.ReferenceProfile, "unknown_profile")
		}
		if policy.Mode == codexidentity.ModeEnforce {
			return nil, fmt.Errorf("unsupported codex r2 profile %q", policy.ReferenceProfile)
		}
		return nil, nil
	}

	if observer := s.getCodexR2Observer(); observer != nil {
		recordCodexR2Observation(observer, account.ID, raw, codexidentity.StageInput, purpose, profile.ID, "observed")
	}
	authScope := codexR2AuthScope(c)
	identitySource := codexAccountIdentitySource(c, account)
	credentialScope := codexAccountIdentityNamespace(identitySource)
	if authScope == "" || credentialScope == "" {
		if observer := s.getCodexR2Observer(); observer != nil {
			recordCodexR2Observation(observer, account.ID, raw, codexidentity.StageProposed, purpose, profile.ID, "scope_unknown")
		}
		if policy.Mode == codexidentity.ModeEnforce {
			return nil, ErrCodexR2BindingInvalid
		}
		return nil, nil
	}

	r2 := s.cfg.Gateway.CodexR2
	mapper, err := codexidentity.NewMapper(codexidentity.MappingScope{
		Secret:          []byte(r2.MappingHMACKey),
		AuthScope:       authScope,
		CredentialScope: credentialScope,
		Algorithm:       codexR2MappingAlgorithm,
		KeyEpoch:        r2.MappingKeyEpoch,
	})
	if err != nil {
		if policy.Mode == codexidentity.ModeEnforce {
			return nil, err
		}
		return nil, nil
	}
	plan, err := codexidentity.BuildPlan(raw, profile, mapper, codexidentity.BuildOptions{
		RequireValidatedClient: policy.ClientUAMode == config.CodexR2ClientUAModePreserveValidated,
	})
	if err == nil {
		err = plan.Validate()
	}
	if err != nil {
		for _, conflict := range plan.Conflicts {
			logger.L().Warn("codex_r2_plan_conflict",
				zap.Int64("account_id", account.ID),
				zap.String("code", conflict.Code),
				zap.String("role", string(conflict.Role)),
				zap.String("reason", conflict.Message),
			)
		}
		if observer := s.getCodexR2Observer(); observer != nil {
			recordCodexR2Observation(observer, account.ID, raw, codexidentity.StageProposed, purpose, profile.ID, "plan_conflict")
		}
		if policy.Mode == codexidentity.ModeEnforce {
			return nil, err
		}
		return nil, nil
	}

	wireMode, wireContract, wireErr := codexR2WireContractForRequest(s.cfg, profile, purpose, headers, body)
	if wireErr != nil {
		if observer := s.getCodexR2Observer(); observer != nil {
			recordCodexR2Observation(observer, account.ID, raw, codexidentity.StageProposed, purpose, profile.ID, "wire_contract_invalid")
		}
		if wireMode == config.CodexR2WireModeShadow {
			// A wire-shadow evidence gap must not modify the already-deployed R2
			// behavior. It stays visible in diagnostics and the real request uses the
			// current R2 contract.
			wireMode = config.CodexR2WireModeOff
			wireContract = nil
		}
	}
	attempt := &codexR2Attempt{
		Policy:         policy,
		Purpose:        purpose,
		Raw:            raw,
		Plan:           plan,
		WireMode:       wireMode,
		WireContract:   wireContract,
		PolicyRevision: codexR2PolicyRevision,
	}
	// Until a new R2.2 binding is admitted the real transport remains on the
	// current R2 compatibility key. Shadow never fragments the live pool merely
	// because a candidate contract was computed.
	attempt.WSCompatibility = codexR2WSCompatibilityDigest(
		r2, codexR2PolicyRevision, profile.ID, authScope, credentialScope, plan, nil,
	)
	if policy.Mode == codexidentity.ModeShadow {
		if observer := s.getCodexR2Observer(); observer != nil {
			projectedHeaders, projectedBody, projectErr := codexidentity.ProjectHTTP(headers, body, plan)
			if projectErr == nil {
				proposed := codexidentity.Capture(projectedHeaders, projectedBody, codexR2SnapshotLimits(s.cfg))
				recordCodexR2Observation(observer, account.ID, proposed, codexidentity.StageProposed, purpose, profile.ID, "planned")
			}
		}
		stageCodexR2Attempt(c, attempt)
		return attempt, nil
	}

	if s.codexR2State == nil {
		return nil, ErrCodexR2AdmissionRequired
	}
	sessionRaw := codexidentity.BuildGraph(raw, profile).Current(codexidentity.RoleSession)
	if sessionRaw == "" {
		return nil, ErrCodexR2AdmissionRequired
	}
	authDigest := codexR2Digest([]byte(r2.MappingHMACKey), "auth-scope", authScope)
	sessionDigest := codexR2Digest([]byte(r2.MappingHMACKey), "session", sessionRaw)
	namespaceDigest := codexR2Digest([]byte(r2.MappingHMACKey), "credential-scope", credentialScope)
	binding, getErr := s.codexR2State.GetBinding(ctx, account.ID, authDigest, sessionDigest)
	if errors.Is(getErr, sql.ErrNoRows) {
		if !r2.NewSessionAdmission {
			return nil, ErrCodexR2AdmissionRequired
		}
		if wireMode == config.CodexR2WireModeEnforce && wireErr != nil {
			return nil, wireErr
		}
		admission := CodexR2Admission{
			AccountID:        account.ID,
			AuthScopeDigest:  authDigest,
			SessionDigest:    sessionDigest,
			ProfileRevision:  profile.ID,
			MappingAlgorithm: codexR2MappingAlgorithm,
			MappingKeyEpoch:  r2.MappingKeyEpoch,
			NamespaceDigest:  namespaceDigest,
			UAPolicy:         policy.ClientUAMode,
		}
		if wireMode == config.CodexR2WireModeEnforce && wireContract != nil {
			if err := validateCodexR22AuthOwner(profile, identitySource); err != nil {
				return nil, err
			}
			wireStore, ok := s.codexR2State.(codexR2WireRuntimeStateStore)
			if !ok {
				return nil, ErrCodexR2WireIntegrity
			}
			admission.PolicyRevision = codexR22WirePolicyRevision
			wireAdmission := CodexR2WireContractAdmission{
				ContractID:      wireContract.ID,
				ContractSHA256:  wireContract.Digest(),
				ReferenceCommit: wireContract.ReferenceCommit,
				GraphRevision:   wireContract.GraphRevision,
			}
			var wireBinding *CodexR2WireContractBinding
			binding, wireBinding, _, getErr = wireStore.AdmitWithWireContract(ctx, admission, wireAdmission)
			if getErr == nil {
				attempt.WireBinding = wireBinding
				attempt.PolicyRevision = codexR22WirePolicyRevision
			}
		} else {
			admission.PolicyRevision = codexR2PolicyRevision
			binding, _, getErr = s.codexR2State.Admit(ctx, admission)
		}
	}
	if getErr != nil {
		return nil, getErr
	}
	if binding.Status != CodexR2BindingActive && binding.Status != CodexR2BindingDraining {
		return nil, ErrCodexR2BindingInvalid
	}
	switch binding.PolicyRevision {
	case codexR2PolicyRevision:
		// Existing R2 sessions remain pinned to the already deployed contract,
		// even if an operator later enables R2.2 for new admissions. R2.2 shadow
		// may still inspect the existing request, but cannot alter its persisted
		// policy or live WS compatibility key.
		if attempt.WireMode != config.CodexR2WireModeShadow {
			attempt.WireMode = config.CodexR2WireModeOff
			attempt.WireContract = nil
		}
		attempt.WireBinding = nil
		attempt.PolicyRevision = codexR2PolicyRevision
	case codexR22WirePolicyRevision:
		contract, ok := codexwire.ContractForProfile(profile.ID)
		if !ok {
			return nil, ErrCodexR2WireIntegrity
		}
		if err := validateCodexR22AuthOwner(profile, identitySource); err != nil {
			return nil, err
		}
		if err := validateCodexR2WireContractRequest(contract, purpose, headers, body, true); err != nil {
			return nil, err
		}
		wireStore, ok := s.codexR2State.(codexR2WireRuntimeStateStore)
		if !ok {
			return nil, ErrCodexR2WireIntegrity
		}
		wireBinding := attempt.WireBinding
		if wireBinding == nil {
			wireBinding, getErr = wireStore.GetWireContractBinding(ctx, binding.ID)
			if getErr != nil {
				return nil, ErrCodexR2WireIntegrity
			}
		}
		if wireBinding.ContractID != contract.ID ||
			wireBinding.ContractSHA256 != contract.Digest() ||
			wireBinding.ReferenceCommit != contract.ReferenceCommit ||
			wireBinding.GraphRevision != contract.GraphRevision {
			return nil, ErrCodexR2WireIntegrity
		}
		attempt.WireMode = config.CodexR2WireModeEnforce
		attempt.WireContract = &contract
		attempt.WireBinding = wireBinding
		attempt.PolicyRevision = codexR22WirePolicyRevision
	default:
		return nil, ErrCodexR2BindingInvalid
	}
	if binding.ProfileRevision != profile.ID ||
		binding.MappingAlgorithm != codexR2MappingAlgorithm ||
		binding.MappingKeyEpoch != r2.MappingKeyEpoch ||
		binding.NamespaceDigest != namespaceDigest ||
		binding.UAPolicy != policy.ClientUAMode {
		return nil, ErrCodexR2BindingInvalid
	}
	attempt.Binding = binding
	compatContract := attempt.WireContract
	if attempt.WireMode == config.CodexR2WireModeShadow {
		compatContract = nil
	}
	attempt.WSCompatibility = codexR2WSCompatibilityDigest(
		r2, attempt.PolicyRevision, profile.ID, authScope, credentialScope, plan, compatContract,
	)
	stageCodexR2Attempt(c, attempt)
	return attempt, nil
}

func (s *OpenAIGatewayService) recordCodexR2Actual(account *Account, attempt *codexR2Attempt, headers http.Header, body []byte, result string) {
	if account == nil || attempt == nil {
		return
	}
	if observer := s.getCodexR2Observer(); observer != nil {
		actual := codexidentity.Capture(headers, body, codexR2SnapshotLimits(s.cfg))
		recordCodexR2Observation(observer, account.ID, actual, codexidentity.StageActual, attempt.Purpose, attempt.Plan.ProfileID, result)
	}
}

func codexR2SemanticReadyResult(c *gin.Context) string {
	attempt := stagedCodexR2Attempt(c)
	if attempt != nil && attempt.WireMode != config.CodexR2WireModeOff {
		return string(codexwire.EvidenceSemanticReady)
	}
	return "prepared"
}

func (s *OpenAIGatewayService) finalizeCodexR2TransportEnvelope(
	c *gin.Context,
	account *Account,
	req *http.Request,
	semanticBody []byte,
) error {
	attempt := stagedCodexR2Attempt(c)
	if attempt == nil || attempt.WireMode == config.CodexR2WireModeOff {
		return nil
	}
	if err := validateOpenAIResponsesFinalEnvelope(req, semanticBody); err != nil {
		s.recordCodexR2Actual(account, attempt, req.Header, semanticBody, "transport_invalid")
		if attempt.WireMode == config.CodexR2WireModeEnforce {
			return err
		}
		logger.L().Warn("codex_r2_wire_transport_invalid",
			zap.Int64("account_id", account.ID),
			zap.String("contract_id", func() string {
				if attempt.WireContract == nil {
					return ""
				}
				return attempt.WireContract.ID
			}()),
			zap.Error(err),
		)
		return nil
	}
	s.recordCodexR2Actual(account, attempt, req.Header, semanticBody, string(codexwire.EvidenceTransportReady))
	return nil
}

func applyCodexR2ClientIdentity(headers http.Header, plan codexidentity.OutboundPlan) error {
	if headers == nil {
		return nil
	}
	if !plan.Client.Recognized {
		return errors.New("codex r2 enforce requires a validated client identity")
	}
	headers.Set("user-agent", plan.Client.UserAgent)
	headers.Set("originator", plan.Client.Originator)
	headers.Set("version", plan.Client.Version)
	return nil
}

func applyCodexR2HeaderPlan(c *gin.Context, headers http.Header) error {
	attempt := stagedCodexR2Attempt(c)
	if attempt == nil || attempt.Policy.Mode != codexidentity.ModeEnforce {
		return nil
	}
	projected, err := codexidentity.ProjectHeaders(headers, attempt.Plan)
	if err != nil {
		return err
	}
	for name := range headers {
		delete(headers, name)
	}
	for name, values := range projected {
		headers[name] = append([]string(nil), values...)
	}
	return applyCodexR2ClientIdentity(headers, attempt.Plan)
}

func codexR2WSCompatibility(c *gin.Context) string {
	attempt := stagedCodexR2Attempt(c)
	if attempt == nil || attempt.Policy.Mode != codexidentity.ModeEnforce {
		return ""
	}
	return attempt.WSCompatibility
}

func allowCodexR2IdentityIngressHeader(c *gin.Context, lowerKey string) bool {
	if !codexR2Enforcing(c) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(lowerKey)) {
	case "session-id", "session_id",
		"conversation-id", "conversation_id",
		"thread-id", "turn-id", "x-client-request-id",
		"x-codex-installation-id", "x-codex-window-id",
		"x-codex-turn-metadata", "x-codex-parent-thread-id",
		"x-openai-subagent":
		return true
	default:
		return false
	}
}

func codexR2Enforcing(c *gin.Context) bool {
	attempt := stagedCodexR2Attempt(c)
	return attempt != nil && attempt.Policy.Mode == codexidentity.ModeEnforce
}
