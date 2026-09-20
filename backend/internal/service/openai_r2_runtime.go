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
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	codexR2PolicyRevision    = "r2-v1"
	codexR2MappingAlgorithm  = "hmac-sha256-v1"
	codexR2AttemptContextKey = "openai_codex_r2_attempt"
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

type codexR2Attempt struct {
	Policy          codexR2EffectivePolicy
	Purpose         codexidentity.Purpose
	Raw             codexidentity.RawSnapshot
	Plan            codexidentity.OutboundPlan
	Binding         *CodexR2PolicyBinding
	WSCompatibility string
}

func codexR2Purpose(c *gin.Context, websocket bool) codexidentity.Purpose {
	if websocket {
		return codexidentity.PurposeWebSocket
	}
	if isOpenAIResponsesCompactPath(c) {
		return codexidentity.PurposeCompact
	}
	return codexidentity.PurposeInference
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
	credentialScope := codexAccountIdentityNamespace(codexAccountIdentitySource(c, account))
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

	attempt := &codexR2Attempt{Policy: policy, Purpose: purpose, Raw: raw, Plan: plan}
	attempt.WSCompatibility = codexR2Digest(
		[]byte(r2.MappingHMACKey),
		"ws-compatibility",
		strings.Join([]string{
			codexR2PolicyRevision,
			profile.ID,
			r2.MappingKeyEpoch,
			authScope,
			credentialScope,
			plan.Client.UserAgent,
			plan.Client.Originator,
			plan.Client.Version,
		}, "\x00"),
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
		binding, _, getErr = s.codexR2State.Admit(ctx, CodexR2Admission{
			AccountID:        account.ID,
			AuthScopeDigest:  authDigest,
			SessionDigest:    sessionDigest,
			ProfileRevision:  profile.ID,
			PolicyRevision:   codexR2PolicyRevision,
			MappingAlgorithm: codexR2MappingAlgorithm,
			MappingKeyEpoch:  r2.MappingKeyEpoch,
			NamespaceDigest:  namespaceDigest,
			UAPolicy:         policy.ClientUAMode,
		})
	}
	if getErr != nil {
		return nil, getErr
	}
	if binding.Status != CodexR2BindingActive && binding.Status != CodexR2BindingDraining {
		return nil, ErrCodexR2BindingInvalid
	}
	if binding.ProfileRevision != profile.ID ||
		binding.PolicyRevision != codexR2PolicyRevision ||
		binding.MappingAlgorithm != codexR2MappingAlgorithm ||
		binding.MappingKeyEpoch != r2.MappingKeyEpoch ||
		binding.NamespaceDigest != namespaceDigest ||
		binding.UAPolicy != policy.ClientUAMode {
		return nil, ErrCodexR2BindingInvalid
	}
	attempt.Binding = binding
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
