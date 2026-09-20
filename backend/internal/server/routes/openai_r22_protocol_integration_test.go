//go:build unit

package routes

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/codexwire"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestR22AuthenticatedNewSessionPersistsPinnedWireContract(t *testing.T) {
	const (
		accountID        = int64(9022)
		mappingKey       = "01234567890123456789012345678901"
		sessionID        = "0199a1b2-c3d4-7e5f-8a9b-242424242424"
		threadID         = "0199a1b2-c3d4-7e5f-8a9b-252525252525"
		installID        = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		windowID         = "0199a1b2-c3d4-7e5f-8a9b-262626262626"
		clientUA         = "codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm"
		chatgptAccountID = "synthetic-r22-account"
	)
	clientKey := b9ProtocolSecret(t)
	cfg := r2ProtocolConfig(accountID, mappingKey)
	cfg.Gateway.CodexR2.NewSessionAdmission = true
	cfg.Gateway.CodexR2.WireContractMode = config.CodexR2WireModeEnforce
	account := service.Account{
		ID: accountID, Name: "r22-admission", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{
			"access_token":       "synthetic-r22-token",
			"chatgpt_account_id": chatgptAccountID,
		},
	}

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	authDigest := r2StateDigest(mappingKey, "auth-scope", "api-key:4444")
	sessionDigest := r2StateDigest(mappingKey, "session", sessionID)
	namespaceDigest := r2StateDigest(mappingKey, "credential-scope", "chatgpt:"+chatgptAccountID)
	mock.ExpectQuery(`(?s)SELECT .*FROM codex_r2_policy_bindings.*WHERE account_id=\$1`).
		WithArgs(accountID, authDigest, sessionDigest).
		WillReturnError(sql.ErrNoRows)

	contract, ok := codexwire.ContractForProfile("codex-0.154-profile-r1")
	require.True(t, ok)
	now := time.Unix(1_700_000_200, 0).UTC()
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO codex_r2_policy_bindings").
		WithArgs(
			accountID, authDigest, sessionDigest, "codex-0.154-profile-r1", "r2.2-wire-v1",
			"hmac-sha256-v1", "epoch-1", namespaceDigest, config.CodexR2ClientUAModePreserveValidated,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "account_id", "auth_scope_digest", "session_digest", "profile_revision", "policy_revision",
			"mapping_algorithm", "mapping_key_epoch", "namespace_digest", "ua_policy", "status", "version", "created_at", "updated_at",
		}).AddRow(
			22, accountID, authDigest, sessionDigest, "codex-0.154-profile-r1", "r2.2-wire-v1",
			"hmac-sha256-v1", "epoch-1", namespaceDigest, config.CodexR2ClientUAModePreserveValidated,
			"active", 1, now, now,
		))
	mock.ExpectQuery("INSERT INTO codex_r2_wire_contract_bindings").
		WithArgs(int64(22), contract.ID, contract.Digest(), contract.ReferenceCommit, contract.GraphRevision).
		WillReturnRows(sqlmock.NewRows([]string{
			"binding_id", "contract_id", "contract_sha256", "reference_commit", "graph_revision", "created_at", "updated_at",
		}).AddRow(22, contract.ID, contract.Digest(), contract.ReferenceCommit, contract.GraphRevision, now, now))
	mock.ExpectCommit()

	state := service.NewCodexR2StateService(db, cfg)
	upstream := &r2ProtocolHTTPUpstream{}
	router := r2ProtocolRouter(t, cfg, account, clientKey, upstream, state)

	metadataBytes, err := json.Marshal(map[string]any{
		"installation_id":   installID,
		"session_id":        sessionID,
		"thread_id":         threadID,
		"turn_id":           "0199a1b2-c3d4-7e5f-8a9b-272727272727",
		"window_id":         windowID,
		"request_kind":      "turn",
		"analytics_enabled": false,
		"future_opaque":     map[string]any{"n": json.Number("9007199254740993")},
	})
	require.NoError(t, err)
	metadata := string(metadataBytes)
	bodyBytes, err := json.Marshal(map[string]any{
		"model": "gpt-5.6-sol", "input": "synthetic-r22", "stream": false,
		"prompt_cache_key": sessionID,
		"client_metadata": map[string]any{
			"session_id": sessionID, "thread_id": threadID,
			"x-codex-installation-id": installID, "x-codex-window-id": windowID,
			"x-codex-turn-metadata": metadata,
		},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Authorization", "Bearer "+clientKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", clientUA)
	req.Header.Set("originator", "codex-tui")
	req.Header.Set("version", "0.154.0")
	req.Header.Set("session-id", sessionID)
	req.Header.Set("thread-id", threadID)
	req.Header.Set("x-client-request-id", threadID)
	req.Header.Set("x-codex-installation-id", installID)
	req.Header.Set("x-codex-window-id", windowID)
	req.Header.Set("x-codex-turn-metadata", metadata)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	headers, capturedBody, calls := upstream.captured()
	require.Equal(t, 1, calls, "R2.2 validation must not create a second upstream request")
	require.Equal(t, "0.154.0", headers.Get("version"))
	require.Contains(t, string(capturedBody), "synthetic-r22")
	require.NoError(t, mock.ExpectationsWereMet())
}
