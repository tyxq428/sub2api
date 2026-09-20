package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
)

var (
	ErrCodexR2BindingConflict = errors.New("codex r2 policy binding conflict")
	ErrCodexR2BindingLost     = errors.New("codex r2 policy binding compare-and-swap lost")
	ErrCodexR2LineageConflict = errors.New("codex r2 lineage anchor conflict")
	ErrCodexR2WireIntegrity   = errors.New("codex r2 wire contract integrity failure")
)

var codexR2DigestPattern = regexp.MustCompile(`^[0-9a-f]{32,64}$`)

type CodexR2BindingStatus string

const (
	CodexR2BindingActive   CodexR2BindingStatus = "active"
	CodexR2BindingDraining CodexR2BindingStatus = "draining"
	CodexR2BindingRetired  CodexR2BindingStatus = "retired"
)

type CodexR2PolicyBinding struct {
	ID               int64                `json:"id"`
	AccountID        int64                `json:"account_id"`
	AuthScopeDigest  string               `json:"-"`
	SessionDigest    string               `json:"-"`
	ProfileRevision  string               `json:"profile_revision"`
	PolicyRevision   string               `json:"policy_revision"`
	MappingAlgorithm string               `json:"mapping_algorithm"`
	MappingKeyEpoch  string               `json:"mapping_key_epoch"`
	NamespaceDigest  string               `json:"-"`
	UAPolicy         string               `json:"ua_policy"`
	Status           CodexR2BindingStatus `json:"status"`
	Version          int64                `json:"version"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type CodexR2Admission struct {
	AccountID        int64
	AuthScopeDigest  string
	SessionDigest    string
	ProfileRevision  string
	PolicyRevision   string
	MappingAlgorithm string
	MappingKeyEpoch  string
	NamespaceDigest  string
	UAPolicy         string
}

type CodexR2WireContractBinding struct {
	BindingID       int64     `json:"binding_id"`
	ContractID      string    `json:"contract_id"`
	ContractSHA256  string    `json:"contract_sha256"`
	ReferenceCommit string    `json:"reference_commit"`
	GraphRevision   string    `json:"graph_revision"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CodexR2WireContractAdmission struct {
	ContractID      string
	ContractSHA256  string
	ReferenceCommit string
	GraphRevision   string
}

type CodexR2LineageAnchor struct {
	BindingID    int64
	EntityKind   string
	EntityDigest string
	Relation     string
	TargetDigest string
}

type CodexR2ShadowEdge struct {
	Relation   string
	FromClass  codexidentity.IdentityClass
	FromDigest string
	ToClass    codexidentity.IdentityClass
	ToDigest   string
}

type CodexR2BindingCounts struct {
	Active   int64 `json:"active"`
	Draining int64 `json:"draining"`
	Retired  int64 `json:"retired"`
}

type CodexR2ShadowDaily struct {
	BucketDate       string `json:"bucket_date"`
	Stage            string `json:"stage"`
	Purpose          string `json:"purpose"`
	Profile          string `json:"profile"`
	Result           string `json:"result"`
	TotalEvents      int64  `json:"total_events"`
	CompleteEvents   int64  `json:"complete_events"`
	IncompleteEvents int64  `json:"incomplete_events"`
	ConflictEvents   int64  `json:"conflict_events"`
	UnknownEvents    int64  `json:"unknown_events"`
}

type CodexR2AccountState struct {
	AccountID            int64                `json:"account_id"`
	ConfiguredMode       string               `json:"configured_mode"`
	EffectiveMode        string               `json:"effective_mode"`
	Reason               string               `json:"reason"`
	ClientUAMode         string               `json:"client_ua_mode"`
	ReferenceProfile     string               `json:"reference_profile"`
	ShadowTelemetry      bool                 `json:"shadow_telemetry"`
	NewSessionAdmission  bool                 `json:"new_session_admission"`
	Eligible             bool                 `json:"eligible"`
	FingerprintMode      string               `json:"fingerprint_mode"`
	MappingKeyConfigured bool                 `json:"mapping_key_configured"`
	MappingKeyEpoch      string               `json:"mapping_key_epoch"`
	Bindings             CodexR2BindingCounts `json:"bindings"`
	Shadow               []CodexR2ShadowDaily `json:"shadow"`
}

// CodexR2StateService owns durable correctness state. It intentionally stores
// only keyed digests/derived namespaces; raw client identifiers never cross
// this API.
type CodexR2StateService struct {
	db  *sql.DB
	cfg *config.Config
}

// WriteBatch implements codexidentity.BatchSink. Account ownership is carried
// outside the serialized observer payload and only pseudonymized fields are
// persisted.
func (s *CodexR2StateService) WriteBatch(ctx context.Context, events []codexidentity.Event) error {
	if len(events) == 0 {
		return nil
	}
	groups := make(map[int64][]codexidentity.Event)
	for _, event := range events {
		if event.AccountID <= 0 {
			return errors.New("codex r2 shadow event is missing account owner")
		}
		groups[event.AccountID] = append(groups[event.AccountID], event)
	}
	for accountID, batch := range groups {
		if err := s.RecordShadowBatch(ctx, accountID, batch); err != nil {
			return err
		}
	}
	return nil
}

func NewCodexR2StateService(db *sql.DB, cfg *config.Config) *CodexR2StateService {
	return &CodexR2StateService{db: db, cfg: cfg}
}

func validateCodexR2Digest(value, name string) error {
	if !codexR2DigestPattern.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("%s must be a 32-64 character lowercase hex digest", name)
	}
	return nil
}

func validateCodexR2Admission(in CodexR2Admission) error {
	if in.AccountID <= 0 {
		return errors.New("account id is required")
	}
	for name, value := range map[string]string{
		"auth scope digest": in.AuthScopeDigest,
		"session digest":    in.SessionDigest,
		"namespace digest":  in.NamespaceDigest,
	} {
		if err := validateCodexR2Digest(value, name); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"profile revision":  in.ProfileRevision,
		"policy revision":   in.PolicyRevision,
		"mapping algorithm": in.MappingAlgorithm,
		"mapping key epoch": in.MappingKeyEpoch,
		"ua policy":         in.UAPolicy,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

var (
	codexR2WireSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	codexR2WireCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func validateCodexR2WireAdmission(in CodexR2WireContractAdmission) error {
	if strings.TrimSpace(in.ContractID) == "" || strings.TrimSpace(in.GraphRevision) == "" {
		return errors.New("codex r2 wire contract id and graph revision are required")
	}
	if !codexR2WireSHA256Pattern.MatchString(strings.TrimSpace(in.ContractSHA256)) {
		return errors.New("codex r2 wire contract sha256 must be lowercase hex")
	}
	if !codexR2WireCommitPattern.MatchString(strings.TrimSpace(in.ReferenceCommit)) {
		return errors.New("codex r2 wire reference commit must be lowercase hex")
	}
	return nil
}

const codexR2BindingColumns = `id, account_id, auth_scope_digest, session_digest,
profile_revision, policy_revision, mapping_algorithm, mapping_key_epoch,
namespace_digest, ua_policy, status, version, created_at, updated_at`

type codexR2RowScanner interface {
	Scan(dest ...any) error
}

func scanCodexR2Binding(row codexR2RowScanner) (*CodexR2PolicyBinding, error) {
	var binding CodexR2PolicyBinding
	if err := row.Scan(
		&binding.ID, &binding.AccountID, &binding.AuthScopeDigest, &binding.SessionDigest,
		&binding.ProfileRevision, &binding.PolicyRevision, &binding.MappingAlgorithm,
		&binding.MappingKeyEpoch, &binding.NamespaceDigest, &binding.UAPolicy,
		&binding.Status, &binding.Version, &binding.CreatedAt, &binding.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &binding, nil
}

func scanCodexR2WireBinding(row codexR2RowScanner) (*CodexR2WireContractBinding, error) {
	var binding CodexR2WireContractBinding
	if err := row.Scan(
		&binding.BindingID, &binding.ContractID, &binding.ContractSHA256,
		&binding.ReferenceCommit, &binding.GraphRevision, &binding.CreatedAt, &binding.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &binding, nil
}

func sameCodexR2Admission(binding *CodexR2PolicyBinding, in CodexR2Admission) bool {
	return binding != nil &&
		binding.ProfileRevision == in.ProfileRevision &&
		binding.PolicyRevision == in.PolicyRevision &&
		binding.MappingAlgorithm == in.MappingAlgorithm &&
		binding.MappingKeyEpoch == in.MappingKeyEpoch &&
		binding.NamespaceDigest == in.NamespaceDigest &&
		binding.UAPolicy == in.UAPolicy
}

func sameCodexR2WireAdmission(binding *CodexR2WireContractBinding, in CodexR2WireContractAdmission) bool {
	return binding != nil &&
		binding.ContractID == in.ContractID &&
		binding.ContractSHA256 == in.ContractSHA256 &&
		binding.ReferenceCommit == in.ReferenceCommit &&
		binding.GraphRevision == in.GraphRevision
}

func (s *CodexR2StateService) Admit(ctx context.Context, in CodexR2Admission) (*CodexR2PolicyBinding, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, errors.New("codex r2 state database unavailable")
	}
	if err := validateCodexR2Admission(in); err != nil {
		return nil, false, err
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO codex_r2_policy_bindings (
    account_id, auth_scope_digest, session_digest, profile_revision, policy_revision,
    mapping_algorithm, mapping_key_epoch, namespace_digest, ua_policy
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (account_id, auth_scope_digest, session_digest) DO NOTHING
RETURNING `+codexR2BindingColumns,
		in.AccountID, in.AuthScopeDigest, in.SessionDigest, in.ProfileRevision, in.PolicyRevision,
		in.MappingAlgorithm, in.MappingKeyEpoch, in.NamespaceDigest, in.UAPolicy,
	)
	binding, err := scanCodexR2Binding(row)
	if err == nil {
		return binding, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	existing, err := s.GetBinding(ctx, in.AccountID, in.AuthScopeDigest, in.SessionDigest)
	if err != nil {
		return nil, false, err
	}
	if existing.ProfileRevision != in.ProfileRevision ||
		existing.PolicyRevision != in.PolicyRevision ||
		existing.MappingAlgorithm != in.MappingAlgorithm ||
		existing.MappingKeyEpoch != in.MappingKeyEpoch ||
		existing.NamespaceDigest != in.NamespaceDigest ||
		existing.UAPolicy != in.UAPolicy {
		return nil, false, ErrCodexR2BindingConflict
	}
	return existing, false, nil
}

// AdmitWithWireContract atomically creates a new R2 policy binding and its
// R2.2 wire-contract record. Existing legacy R2 bindings are never upgraded by
// this method: a conflicting pre-existing policy row is returned as a binding
// conflict so runtime code can keep that session on the old contract.
func (s *CodexR2StateService) AdmitWithWireContract(
	ctx context.Context,
	in CodexR2Admission,
	wire CodexR2WireContractAdmission,
) (*CodexR2PolicyBinding, *CodexR2WireContractBinding, bool, error) {
	if s == nil || s.db == nil {
		return nil, nil, false, errors.New("codex r2 state database unavailable")
	}
	if err := validateCodexR2Admission(in); err != nil {
		return nil, nil, false, err
	}
	if err := validateCodexR2WireAdmission(wire); err != nil {
		return nil, nil, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
INSERT INTO codex_r2_policy_bindings (
    account_id, auth_scope_digest, session_digest, profile_revision, policy_revision,
    mapping_algorithm, mapping_key_epoch, namespace_digest, ua_policy
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (account_id, auth_scope_digest, session_digest) DO NOTHING
RETURNING `+codexR2BindingColumns,
		in.AccountID, in.AuthScopeDigest, in.SessionDigest, in.ProfileRevision, in.PolicyRevision,
		in.MappingAlgorithm, in.MappingKeyEpoch, in.NamespaceDigest, in.UAPolicy,
	)
	binding, err := scanCodexR2Binding(row)
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := scanCodexR2Binding(tx.QueryRowContext(ctx, `
SELECT `+codexR2BindingColumns+`
FROM codex_r2_policy_bindings
WHERE account_id=$1 AND auth_scope_digest=$2 AND session_digest=$3`,
			in.AccountID, in.AuthScopeDigest, in.SessionDigest))
		if getErr != nil {
			return nil, nil, false, getErr
		}
		if !sameCodexR2Admission(existing, in) {
			return nil, nil, false, ErrCodexR2BindingConflict
		}
		wireBinding, wireErr := scanCodexR2WireBinding(tx.QueryRowContext(ctx, `
SELECT binding_id, contract_id, contract_sha256, reference_commit, graph_revision, created_at, updated_at
FROM codex_r2_wire_contract_bindings WHERE binding_id=$1`, existing.ID))
		if wireErr != nil {
			if errors.Is(wireErr, sql.ErrNoRows) {
				return nil, nil, false, ErrCodexR2WireIntegrity
			}
			return nil, nil, false, wireErr
		}
		if !sameCodexR2WireAdmission(wireBinding, wire) {
			return nil, nil, false, ErrCodexR2WireIntegrity
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, false, err
		}
		return existing, wireBinding, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}

	wireBinding, err := scanCodexR2WireBinding(tx.QueryRowContext(ctx, `
INSERT INTO codex_r2_wire_contract_bindings (
    binding_id, contract_id, contract_sha256, reference_commit, graph_revision
) VALUES ($1,$2,$3,$4,$5)
RETURNING binding_id, contract_id, contract_sha256, reference_commit, graph_revision, created_at, updated_at`,
		binding.ID, wire.ContractID, wire.ContractSHA256, wire.ReferenceCommit, wire.GraphRevision))
	if err != nil {
		return nil, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	return binding, wireBinding, true, nil
}

func (s *CodexR2StateService) GetWireContractBinding(ctx context.Context, bindingID int64) (*CodexR2WireContractBinding, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("codex r2 state database unavailable")
	}
	if bindingID <= 0 {
		return nil, errors.New("binding id is required")
	}
	return scanCodexR2WireBinding(s.db.QueryRowContext(ctx, `
SELECT binding_id, contract_id, contract_sha256, reference_commit, graph_revision, created_at, updated_at
FROM codex_r2_wire_contract_bindings
WHERE binding_id=$1`, bindingID))
}

func (s *CodexR2StateService) GetBinding(ctx context.Context, accountID int64, authScopeDigest, sessionDigest string) (*CodexR2PolicyBinding, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("codex r2 state database unavailable")
	}
	return scanCodexR2Binding(s.db.QueryRowContext(ctx, `
SELECT `+codexR2BindingColumns+`
FROM codex_r2_policy_bindings
WHERE account_id=$1 AND auth_scope_digest=$2 AND session_digest=$3`,
		accountID, authScopeDigest, sessionDigest))
}

func (s *CodexR2StateService) TransitionBinding(ctx context.Context, id, expectedVersion int64, from, to CodexR2BindingStatus) error {
	if s == nil || s.db == nil {
		return errors.New("codex r2 state database unavailable")
	}
	if id <= 0 || expectedVersion <= 0 || from == "" || to == "" {
		return errors.New("invalid codex r2 binding transition")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE codex_r2_policy_bindings
SET status=$4, version=version+1, updated_at=NOW()
WHERE id=$1 AND version=$2 AND status=$3`, id, expectedVersion, from, to)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrCodexR2BindingLost
	}
	return nil
}

func (s *CodexR2StateService) DrainAccount(ctx context.Context, accountID int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("codex r2 state database unavailable")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE codex_r2_policy_bindings
SET status='draining', version=version+1, updated_at=NOW()
WHERE account_id=$1 AND status='active'`, accountID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *CodexR2StateService) PutLineageAnchor(ctx context.Context, anchor CodexR2LineageAnchor) (bool, error) {
	if s == nil || s.db == nil {
		return false, errors.New("codex r2 state database unavailable")
	}
	if anchor.BindingID <= 0 {
		return false, errors.New("binding id is required")
	}
	if anchor.EntityKind != "thread" && anchor.EntityKind != "turn" {
		return false, errors.New("unsupported lineage entity kind")
	}
	if anchor.Relation != "parent" && anchor.Relation != "forked_from" && anchor.Relation != "root" {
		return false, errors.New("unsupported lineage relation")
	}
	if err := validateCodexR2Digest(anchor.EntityDigest, "entity digest"); err != nil {
		return false, err
	}
	if err := validateCodexR2Digest(anchor.TargetDigest, "target digest"); err != nil {
		return false, err
	}
	var storedTarget string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO codex_r2_lineage_anchors (
    binding_id, entity_kind, entity_digest, relation, target_digest
) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (binding_id, entity_kind, entity_digest, relation) DO UPDATE
SET updated_at=codex_r2_lineage_anchors.updated_at
RETURNING target_digest`,
		anchor.BindingID, anchor.EntityKind, anchor.EntityDigest, anchor.Relation, anchor.TargetDigest,
	).Scan(&storedTarget)
	if err != nil {
		return false, err
	}
	if storedTarget != anchor.TargetDigest {
		return false, ErrCodexR2LineageConflict
	}
	return true, nil
}

func (s *CodexR2StateService) RecordShadowBatch(ctx context.Context, accountID int64, events []codexidentity.Event) error {
	if s == nil || s.db == nil || len(events) == 0 {
		return nil
	}
	if accountID <= 0 {
		return errors.New("account id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, event := range events {
		observed := time.UnixMilli(event.ObservedAtUnixMS).UTC()
		if event.ObservedAtUnixMS <= 0 {
			observed = time.Now().UTC()
		}
		bucket := observed.Format("2006-01-02")
		complete, incomplete := int64(0), int64(0)
		if event.CoverageComplete {
			complete = 1
		} else {
			incomplete = 1
		}
		conflict, unknown := int64(0), int64(0)
		if strings.Contains(event.Result, "conflict") {
			conflict = 1
		}
		if !event.CoverageComplete || strings.Contains(event.Result, "unknown") {
			unknown = 1
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO codex_r2_shadow_daily (
    bucket_date, account_id, stage, purpose, profile, result,
    total_events, complete_events, incomplete_events, conflict_events, unknown_events
) VALUES ($1,$2,$3,$4,$5,$6,1,$7,$8,$9,$10)
ON CONFLICT (bucket_date, account_id, stage, purpose, profile, result) DO UPDATE SET
    total_events=codex_r2_shadow_daily.total_events+1,
    complete_events=codex_r2_shadow_daily.complete_events+EXCLUDED.complete_events,
    incomplete_events=codex_r2_shadow_daily.incomplete_events+EXCLUDED.incomplete_events,
    conflict_events=codex_r2_shadow_daily.conflict_events+EXCLUDED.conflict_events,
    unknown_events=codex_r2_shadow_daily.unknown_events+EXCLUDED.unknown_events,
    updated_at=NOW()`,
			bucket, accountID, event.Stage, event.Purpose, event.Profile, event.Result,
			complete, incomplete, conflict, unknown,
		); err != nil {
			return err
		}
		for _, field := range event.Fields {
			if !codexR2DigestPattern.MatchString(field.Digest) {
				return errors.New("shadow event contains invalid identity digest")
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO codex_r2_shadow_entities (
    bucket_date, account_id, stage, identity_class, identity_digest, first_seen_at, last_seen_at
) VALUES ($1,$2,$3,$4,$5,$6,$6)
ON CONFLICT (bucket_date, account_id, stage, identity_class, identity_digest) DO UPDATE SET
    seen_count=codex_r2_shadow_entities.seen_count+1,
    last_seen_at=GREATEST(codex_r2_shadow_entities.last_seen_at, EXCLUDED.last_seen_at)`,
				bucket, accountID, event.Stage, field.Class, field.Digest, observed,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *CodexR2StateService) RecordShadowEdge(ctx context.Context, accountID int64, observed time.Time, edge CodexR2ShadowEdge) error {
	if s == nil || s.db == nil {
		return errors.New("codex r2 state database unavailable")
	}
	if accountID <= 0 || strings.TrimSpace(edge.Relation) == "" {
		return errors.New("invalid shadow edge")
	}
	for name, value := range map[string]string{"from digest": edge.FromDigest, "to digest": edge.ToDigest} {
		if err := validateCodexR2Digest(value, name); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO codex_r2_shadow_edges (
    bucket_date, account_id, relation, from_class, from_digest, to_class, to_digest,
    first_seen_at, last_seen_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
ON CONFLICT (bucket_date, account_id, relation, from_class, from_digest, to_class, to_digest)
DO UPDATE SET
    seen_count=codex_r2_shadow_edges.seen_count+1,
    last_seen_at=GREATEST(codex_r2_shadow_edges.last_seen_at, EXCLUDED.last_seen_at)`,
		observed.UTC().Format("2006-01-02"), accountID, edge.Relation,
		edge.FromClass, edge.FromDigest, edge.ToClass, edge.ToDigest, observed.UTC())
	return err
}

func (s *CodexR2StateService) CleanupShadow(ctx context.Context, entityBefore, dailyBefore time.Time, limit int) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("codex r2 state database unavailable")
	}
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var total int64
	for _, table := range []string{"codex_r2_shadow_entities", "codex_r2_shadow_edges"} {
		query := fmt.Sprintf(`WITH doomed AS (
SELECT ctid FROM %s WHERE bucket_date < $1 ORDER BY bucket_date LIMIT $2
) DELETE FROM %s WHERE ctid IN (SELECT ctid FROM doomed)`, table, table)
		result, execErr := tx.ExecContext(ctx, query, entityBefore.UTC().Format("2006-01-02"), limit)
		if execErr != nil {
			return 0, execErr
		}
		rows, _ := result.RowsAffected()
		total += rows
	}
	result, err := tx.ExecContext(ctx, `WITH doomed AS (
SELECT ctid FROM codex_r2_shadow_daily WHERE bucket_date < $1 ORDER BY bucket_date LIMIT $2
) DELETE FROM codex_r2_shadow_daily WHERE ctid IN (SELECT ctid FROM doomed)`,
		dailyBefore.UTC().Format("2006-01-02"), limit)
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	total += rows
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *CodexR2StateService) AccountState(ctx context.Context, account *Account, days int) (*CodexR2AccountState, error) {
	if account == nil {
		return nil, errors.New("account is required")
	}
	policy := resolveCodexR2EffectivePolicy(s.cfg, account)
	r2 := config.GatewayCodexR2Config{}
	if s != nil && s.cfg != nil {
		r2 = s.cfg.Gateway.CodexR2
	}
	state := &CodexR2AccountState{
		AccountID:            account.ID,
		ConfiguredMode:       config.NormalizeCodexR2Mode(r2.Mode),
		EffectiveMode:        string(policy.Mode),
		Reason:               policy.Reason,
		ClientUAMode:         policy.ClientUAMode,
		ReferenceProfile:     policy.ReferenceProfile,
		ShadowTelemetry:      policy.ShadowTelemetry,
		NewSessionAdmission:  r2.NewSessionAdmission,
		Eligible:             containsCodexR2AccountID(r2.EligibleAccountIDs, account.ID),
		FingerprintMode:      string(account.GetCodexFingerprintMode()),
		MappingKeyConfigured: len([]byte(r2.MappingHMACKey)) >= 32,
		MappingKeyEpoch:      strings.TrimSpace(r2.MappingKeyEpoch),
	}
	if s == nil || s.db == nil {
		return state, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT status, COUNT(*) FROM codex_r2_policy_bindings
WHERE account_id=$1 GROUP BY status`, account.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			_ = rows.Close()
			return nil, err
		}
		switch CodexR2BindingStatus(status) {
		case CodexR2BindingActive:
			state.Bindings.Active = count
		case CodexR2BindingDraining:
			state.Bindings.Draining = count
		case CodexR2BindingRetired:
			state.Bindings.Retired = count
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}
	shadowRows, err := s.db.QueryContext(ctx, `
SELECT bucket_date::text, stage, purpose, profile, result,
       total_events, complete_events, incomplete_events, conflict_events, unknown_events
FROM codex_r2_shadow_daily
WHERE account_id=$1 AND bucket_date >= (CURRENT_DATE - ($2::int - 1))
ORDER BY bucket_date DESC, stage, purpose, profile, result`, account.ID, days)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = shadowRows.Close()
	}()
	for shadowRows.Next() {
		var row CodexR2ShadowDaily
		if err := shadowRows.Scan(
			&row.BucketDate, &row.Stage, &row.Purpose, &row.Profile, &row.Result,
			&row.TotalEvents, &row.CompleteEvents, &row.IncompleteEvents,
			&row.ConflictEvents, &row.UnknownEvents,
		); err != nil {
			return nil, err
		}
		state.Shadow = append(state.Shadow, row)
	}
	return state, shadowRows.Err()
}
