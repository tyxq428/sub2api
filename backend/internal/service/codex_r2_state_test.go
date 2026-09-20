//go:build unit

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
	"github.com/stretchr/testify/require"
)

func syntheticCodexR2Admission() CodexR2Admission {
	return CodexR2Admission{
		AccountID:        8,
		AuthScopeDigest:  strings.Repeat("a", 32),
		SessionDigest:    strings.Repeat("b", 32),
		ProfileRevision:  "codex-0.154-profile-r1",
		PolicyRevision:   "r2-v1",
		MappingAlgorithm: "hmac-sha256-v1",
		MappingKeyEpoch:  "epoch-1",
		NamespaceDigest:  strings.Repeat("c", 32),
		UAPolicy:         config.CodexR2ClientUAModePreserveValidated,
	}
}

func codexR2BindingRows(in CodexR2Admission, policy string) *sqlmock.Rows {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"id", "account_id", "auth_scope_digest", "session_digest", "profile_revision",
		"policy_revision", "mapping_algorithm", "mapping_key_epoch", "namespace_digest",
		"ua_policy", "status", "version", "created_at", "updated_at",
	}).AddRow(
		41, in.AccountID, in.AuthScopeDigest, in.SessionDigest, in.ProfileRevision,
		policy, in.MappingAlgorithm, in.MappingKeyEpoch, in.NamespaceDigest,
		in.UAPolicy, "active", 1, now, now,
	)
}

func syntheticCodexR2WireAdmission() CodexR2WireContractAdmission {
	return CodexR2WireContractAdmission{
		ContractID:      "codex-wire-0.154-r1",
		ContractSHA256:  strings.Repeat("d", 64),
		ReferenceCommit: strings.Repeat("e", 40),
		GraphRevision:   "r2.2-graph-v1",
	}
}

func codexR2WireBindingRows(bindingID int64, in CodexR2WireContractAdmission) *sqlmock.Rows {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"binding_id", "contract_id", "contract_sha256", "reference_commit", "graph_revision",
		"created_at", "updated_at",
	}).AddRow(bindingID, in.ContractID, in.ContractSHA256, in.ReferenceCommit, in.GraphRevision, now, now)
}

func TestCodexR2AdmitCreatesBindingWithoutExposingDigests(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	in := syntheticCodexR2Admission()
	mock.ExpectQuery("INSERT INTO codex_r2_policy_bindings").
		WithArgs(
			in.AccountID, in.AuthScopeDigest, in.SessionDigest, in.ProfileRevision,
			in.PolicyRevision, in.MappingAlgorithm, in.MappingKeyEpoch, in.NamespaceDigest, in.UAPolicy,
		).
		WillReturnRows(codexR2BindingRows(in, in.PolicyRevision))
	svc := NewCodexR2StateService(db, &config.Config{})
	binding, created, err := svc.Admit(context.Background(), in)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, int64(41), binding.ID)
	encoded, err := json.Marshal(binding)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), in.SessionDigest)
	require.NotContains(t, string(encoded), in.AuthScopeDigest)
	require.NotContains(t, string(encoded), in.NamespaceDigest)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexR2AdmitExistingPolicyMismatchFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	in := syntheticCodexR2Admission()
	mock.ExpectQuery("INSERT INTO codex_r2_policy_bindings").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "account_id", "auth_scope_digest", "session_digest", "profile_revision",
			"policy_revision", "mapping_algorithm", "mapping_key_epoch", "namespace_digest",
			"ua_policy", "status", "version", "created_at", "updated_at",
		}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+codexR2BindingColumns)).
		WithArgs(in.AccountID, in.AuthScopeDigest, in.SessionDigest).
		WillReturnRows(codexR2BindingRows(in, "older-policy"))
	svc := NewCodexR2StateService(db, &config.Config{})
	_, created, err := svc.Admit(context.Background(), in)
	require.False(t, created)
	require.ErrorIs(t, err, ErrCodexR2BindingConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexR2AdmitWithWireContractIsAtomic(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	in := syntheticCodexR2Admission()
	in.PolicyRevision = "r2.2-wire-v1"
	wire := syntheticCodexR2WireAdmission()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO codex_r2_policy_bindings").
		WithArgs(
			in.AccountID, in.AuthScopeDigest, in.SessionDigest, in.ProfileRevision,
			in.PolicyRevision, in.MappingAlgorithm, in.MappingKeyEpoch, in.NamespaceDigest, in.UAPolicy,
		).
		WillReturnRows(codexR2BindingRows(in, in.PolicyRevision))
	mock.ExpectQuery("INSERT INTO codex_r2_wire_contract_bindings").
		WithArgs(int64(41), wire.ContractID, wire.ContractSHA256, wire.ReferenceCommit, wire.GraphRevision).
		WillReturnRows(codexR2WireBindingRows(41, wire))
	mock.ExpectCommit()

	svc := NewCodexR2StateService(db, &config.Config{})
	binding, wireBinding, created, err := svc.AdmitWithWireContract(context.Background(), in, wire)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, int64(41), binding.ID)
	require.Equal(t, binding.ID, wireBinding.BindingID)
	require.Equal(t, wire.ContractSHA256, wireBinding.ContractSHA256)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexR2AdmitWithWireContractMissingSidecarFailsIntegrity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	in := syntheticCodexR2Admission()
	in.PolicyRevision = "r2.2-wire-v1"
	wire := syntheticCodexR2WireAdmission()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO codex_r2_policy_bindings").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "account_id", "auth_scope_digest", "session_digest", "profile_revision",
			"policy_revision", "mapping_algorithm", "mapping_key_epoch", "namespace_digest",
			"ua_policy", "status", "version", "created_at", "updated_at",
		}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+codexR2BindingColumns)).
		WithArgs(in.AccountID, in.AuthScopeDigest, in.SessionDigest).
		WillReturnRows(codexR2BindingRows(in, in.PolicyRevision))
	mock.ExpectQuery("SELECT binding_id, contract_id, contract_sha256").
		WithArgs(int64(41)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	svc := NewCodexR2StateService(db, &config.Config{})
	_, _, created, err := svc.AdmitWithWireContract(context.Background(), in, wire)
	require.False(t, created)
	require.ErrorIs(t, err, ErrCodexR2WireIntegrity)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexR2TransitionBindingUsesCAS(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectExec("UPDATE codex_r2_policy_bindings").
		WithArgs(int64(41), int64(7), CodexR2BindingActive, CodexR2BindingDraining).
		WillReturnResult(sqlmock.NewResult(0, 0))
	svc := NewCodexR2StateService(db, &config.Config{})
	err = svc.TransitionBinding(context.Background(), 41, 7, CodexR2BindingActive, CodexR2BindingDraining)
	require.ErrorIs(t, err, ErrCodexR2BindingLost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexR2LineageAnchorRejectsChangedTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	anchor := CodexR2LineageAnchor{
		BindingID:    41,
		EntityKind:   "thread",
		EntityDigest: strings.Repeat("d", 32),
		Relation:     "parent",
		TargetDigest: strings.Repeat("e", 32),
	}
	mock.ExpectQuery("INSERT INTO codex_r2_lineage_anchors").
		WithArgs(anchor.BindingID, anchor.EntityKind, anchor.EntityDigest, anchor.Relation, anchor.TargetDigest).
		WillReturnRows(sqlmock.NewRows([]string{"target_digest"}).AddRow(strings.Repeat("f", 32)))
	svc := NewCodexR2StateService(db, &config.Config{})
	_, err = svc.PutLineageAnchor(context.Background(), anchor)
	require.ErrorIs(t, err, ErrCodexR2LineageConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexR2AccountStateWithoutDatabaseDoesNotFabricateStats(t *testing.T) {
	cfg := syntheticR2Config()
	svc := NewCodexR2StateService(nil, cfg)
	state, err := svc.AccountState(context.Background(), &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, 7)
	require.NoError(t, err)
	require.Equal(t, "shadow", state.EffectiveMode)
	require.True(t, state.MappingKeyConfigured)
	require.Empty(t, state.Shadow)
	require.Zero(t, state.Bindings.Active)
}

func TestCodexR2GetBindingPropagatesNoRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + codexR2BindingColumns)).
		WillReturnError(sql.ErrNoRows)
	svc := NewCodexR2StateService(db, &config.Config{})
	_, err = svc.GetBinding(context.Background(), 8, strings.Repeat("a", 32), strings.Repeat("b", 32))
	require.True(t, errors.Is(err, sql.ErrNoRows))
}

func TestCodexR2BindingCanBeReloadedByFreshServiceInstance(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	in := syntheticCodexR2Admission()
	for range 2 {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT "+codexR2BindingColumns)).
			WithArgs(in.AccountID, in.AuthScopeDigest, in.SessionDigest).
			WillReturnRows(codexR2BindingRows(in, in.PolicyRevision))
	}
	first := NewCodexR2StateService(db, &config.Config{})
	one, err := first.GetBinding(context.Background(), in.AccountID, in.AuthScopeDigest, in.SessionDigest)
	require.NoError(t, err)
	second := NewCodexR2StateService(db, &config.Config{})
	two, err := second.GetBinding(context.Background(), in.AccountID, in.AuthScopeDigest, in.SessionDigest)
	require.NoError(t, err)
	require.Equal(t, one.ID, two.ID)
	require.Equal(t, one.PolicyRevision, two.PolicyRevision)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexR2StateSinkRejectsOwnerlessObserverBatch(t *testing.T) {
	svc := NewCodexR2StateService(nil, &config.Config{})
	err := svc.WriteBatch(context.Background(), []codexidentity.Event{{Result: "shadow"}})
	require.ErrorContains(t, err, "missing account owner")
}
