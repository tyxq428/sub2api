//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func codexR2WSTestConfig(accountID int64, mappingKey string) *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.CodexR2 = config.GatewayCodexR2Config{
		Mode:                   config.CodexR2ModeEnforce,
		ClientUAMode:           config.CodexR2ClientUAModePreserveValidated,
		ReferenceProfile:       "codex-0.154-profile-r1",
		EligibleAccountIDs:     []int64{accountID},
		MappingHMACKey:         mappingKey,
		MappingKeyEpoch:        "epoch-1",
		MaxMetadataBytes:       256 * 1024,
		MaxIdentityHeaderBytes: 16 * 1024,
		MaxMetadataDepth:       32,
		MaxIdentityValueBytes:  1024,
		MaxUserAgentBytes:      1024,
	}
	return cfg
}

func expectCodexR2WSBinding(
	t *testing.T,
	mock sqlmock.Sqlmock,
	accountID int64,
	mappingKey string,
	sessionID string,
	chatgptAccountID string,
) {
	t.Helper()
	authDigest := codexR2Digest([]byte(mappingKey), "auth-scope", "api-key:4444")
	sessionDigest := codexR2Digest([]byte(mappingKey), "session", sessionID)
	namespaceDigest := codexR2Digest([]byte(mappingKey), "credential-scope", "chatgpt:"+chatgptAccountID)
	now := time.Unix(1_700_000_000, 0).UTC()
	mock.ExpectQuery(`(?s)SELECT .*FROM codex_r2_policy_bindings.*WHERE account_id=\$1`).
		WithArgs(accountID, authDigest, sessionDigest).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "account_id", "auth_scope_digest", "session_digest", "profile_revision", "policy_revision",
			"mapping_algorithm", "mapping_key_epoch", "namespace_digest", "ua_policy", "status", "version", "created_at", "updated_at",
		}).AddRow(
			1, accountID, authDigest, sessionDigest, "codex-0.154-profile-r1", "r2-v1",
			"hmac-sha256-v1", "epoch-1", namespaceDigest, config.CodexR2ClientUAModePreserveValidated,
			"active", 1, now, now,
		))
}

func r2WSTurnPayload(t *testing.T, sessionID, threadID, turnID, installID, windowID, input string) string {
	t.Helper()
	metadata, err := json.Marshal(map[string]any{
		"installation_id": installID,
		"session_id":      sessionID,
		"thread_id":       threadID,
		"turn_id":         turnID,
		"window_id":       windowID,
		"request_kind":    "turn",
	})
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{
		"type":             "response.create",
		"model":            "gpt-5.6-sol",
		"input":            input,
		"stream":           false,
		"prompt_cache_key": sessionID,
		"client_metadata": map[string]any{
			"session_id":              sessionID,
			"thread_id":               threadID,
			"x-codex-installation-id": installID,
			"x-codex-window-id":       windowID,
			"x-codex-turn-metadata":   string(metadata),
		},
	})
	require.NoError(t, err)
	return string(payload)
}

func TestCodexR2WebSocketMultiTurnKeepsSemanticIdentityAndClientProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		accountID      = int64(9018)
		mappingKey     = "01234567890123456789012345678901"
		chatgptAccount = "synthetic-r2-ws-account"
		sessionID      = "0199a1b2-c3d4-7e5f-8a9b-111111111111"
		threadID       = "0199a1b2-c3d4-7e5f-8a9b-222222222222"
		turn1ID        = "0199a1b2-c3d4-7e5f-8a9b-333333333331"
		turn2ID        = "0199a1b2-c3d4-7e5f-8a9b-333333333332"
		installID      = "44444444-4444-4444-8444-444444444444"
		windowID       = "0199a1b2-c3d4-7e5f-8a9b-555555555555"
		clientUA       = "codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm"
	)
	cfg := codexR2WSTestConfig(accountID, mappingKey)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// One binding lookup occurs for each client turn.
	expectCodexR2WSBinding(t, mock, accountID, mappingKey, sessionID, chatgptAccount)
	expectCodexR2WSBinding(t, mock, accountID, mappingKey, sessionID, chatgptAccount)

	captureConn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"id":"resp_r2_ws_1","model":"gpt-5.6-sol","usage":{"input_tokens":1,"output_tokens":1}}}`),
		[]byte(`{"type":"response.completed","response":{"id":"resp_r2_ws_2","model":"gpt-5.6-sol","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	dialer := &openAIWSCaptureDialer{conn: captureConn}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(dialer)
	t.Cleanup(pool.Close)
	state := NewCodexR2StateService(db, cfg)
	svc := &OpenAIGatewayService{
		cfg:              cfg,
		codexR2State:     state,
		httpUpstream:     &httpUpstreamRecorder{},
		cache:            &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:    NewCodexToolCorrector(),
		openaiWSPool:     pool,
	}
	account := &Account{
		ID: accountID, Name: "r2-ws", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "synthetic-r2-ws-token",
			"chatgpt_account_id": chatgptAccount,
		},
		Extra: map[string]any{"openai_oauth_responses_websockets_v2_enabled": true},
	}

	serverErrCh := make(chan error, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, acceptErr := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if acceptErr != nil {
			serverErrCh <- acceptErr
			return
		}
		defer func() { _ = conn.CloseNow() }()
		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, firstMessage, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		rec := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(rec)
		ginCtx.Request = r.Clone(r.Context())
		ginCtx.Request.Header = ginCtx.Request.Header.Clone()
		ginCtx.Request.Header.Set("User-Agent", clientUA)
		ginCtx.Request.Header.Set("originator", "codex-tui")
		ginCtx.Request.Header.Set("version", "0.154.0")
		ginCtx.Request.Header.Set("session-id", sessionID)
		ginCtx.Request.Header.Set("thread-id", threadID)
		ginCtx.Request.Header.Set("x-client-request-id", threadID)
		ginCtx.Request.Header.Set("x-codex-installation-id", installID)
		ginCtx.Request.Header.Set("x-codex-window-id", windowID)
		ginCtx.Set("api_key", &APIKey{ID: 4444})
		serverErrCh <- svc.ProxyResponsesWebSocketFromClient(
			r.Context(), ginCtx, conn, account, "synthetic-r2-ws-token", firstMessage, nil,
		)
	}))
	defer wsServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
	cancel()
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()
	write := func(payload string) {
		writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelWrite()
		require.NoError(t, client.Write(writeCtx, coderws.MessageText, []byte(payload)))
	}
	read := func() []byte {
		readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelRead()
		_, msg, readErr := client.Read(readCtx)
		require.NoError(t, readErr)
		return msg
	}

	write(r2WSTurnPayload(t, sessionID, threadID, turn1ID, installID, windowID, "first"))
	require.Equal(t, "resp_r2_ws_1", gjson.GetBytes(read(), "response.id").String())
	write(r2WSTurnPayload(t, sessionID, threadID, turn2ID, installID, windowID, "second"))
	require.Equal(t, "resp_r2_ws_2", gjson.GetBytes(read(), "response.id").String())
	_ = client.Close(coderws.StatusNormalClosure, "done")

	select {
	case proxyErr := <-serverErrCh:
		require.NoError(t, proxyErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for r2 websocket ingress to close")
	}
	require.Equal(t, 1, dialer.DialCount(), "same R2 client profile/session should keep one upstream lease")
	dialer.mu.Lock()
	handshakeHeaders := cloneHeader(dialer.lastHeaders)
	dialer.mu.Unlock()
	require.Equal(t, clientUA, handshakeHeaders.Get("User-Agent"))
	require.Equal(t, "codex-tui", handshakeHeaders.Get("originator"))
	require.Equal(t, "0.154.0", handshakeHeaders.Get("version"))
	require.NotEqual(t, sessionID, handshakeHeaders.Get("session-id"))
	require.NotEqual(t, threadID, handshakeHeaders.Get("thread-id"))
	require.Equal(t, handshakeHeaders.Get("thread-id"), handshakeHeaders.Get("x-client-request-id"))
	require.Equal(t, "Bearer synthetic-r2-ws-token", handshakeHeaders.Get("Authorization"))

	captureConn.mu.Lock()
	writes := append([]map[string]any(nil), captureConn.writes...)
	captureConn.mu.Unlock()
	require.Len(t, writes, 2)
	encoded1, err := json.Marshal(writes[0])
	require.NoError(t, err)
	encoded2, err := json.Marshal(writes[1])
	require.NoError(t, err)
	m1 := gjson.GetBytes(encoded1, "client_metadata.x-codex-turn-metadata").String()
	m2 := gjson.GetBytes(encoded2, "client_metadata.x-codex-turn-metadata").String()
	require.NotEmpty(t, m1)
	require.NotEmpty(t, m2)
	require.Equal(t, gjson.Get(m1, "session_id").String(), gjson.Get(m2, "session_id").String())
	require.Equal(t, gjson.Get(m1, "thread_id").String(), gjson.Get(m2, "thread_id").String())
	require.NotEqual(t, gjson.Get(m1, "turn_id").String(), gjson.Get(m2, "turn_id").String())
	require.NotEqual(t, turn1ID, gjson.Get(m1, "turn_id").String())
	require.NotEqual(t, turn2ID, gjson.Get(m2, "turn_id").String())
	require.NoError(t, mock.ExpectationsWereMet())
	ap, ok := pool.getAccountPool(accountID)
	require.True(t, ok)
	ap.mu.Lock()
	require.LessOrEqual(t, len(ap.conns), 1, "R2 compatibility buckets must still share the account connection cap")
	ap.mu.Unlock()
}

func TestCodexR2WSCompatibilitySeparatesClientProfilesWithinAccountCap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	dialer := &openAIWSCountingDialer{}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(dialer)
	t.Cleanup(pool.Close)
	account := &Account{ID: 9099, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1}

	winReq := openAIWSAcquireRequest{
		Account: account, WSURL: "wss://synthetic.invalid/backend-api/codex/responses",
		Headers:         http.Header{"User-Agent": []string{"codex-tui/0.154.0 (Windows 10.0.26200; x86_64) WindowsTerminal"}},
		R2Compatibility: "r2-win-profile",
	}
	macReq := openAIWSAcquireRequest{
		Account: account, WSURL: winReq.WSURL,
		Headers:         http.Header{"User-Agent": []string{"codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm"}},
		R2Compatibility: "r2-mac-profile",
	}
	require.False(t, sameOpenAIWSPrewarmTarget(winReq, macReq))
	lease1, err := pool.Acquire(context.Background(), winReq)
	require.NoError(t, err)
	lease1.Release()
	lease2, err := pool.Acquire(context.Background(), macReq)
	require.NoError(t, err)
	lease2.Release()
	require.Equal(t, 2, dialer.DialCount(), "incompatible R2 client profiles must not reuse a handshake")
	ap, ok := pool.getAccountPool(account.ID)
	require.True(t, ok)
	ap.mu.Lock()
	require.LessOrEqual(t, len(ap.conns), 1, "compatibility buckets must not multiply the account connection limit")
	ap.mu.Unlock()
}
