//go:build unit

package routes

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type r2ProtocolHTTPUpstream struct {
	mu      sync.Mutex
	headers http.Header
	body    []byte
	calls   int
}

func (u *r2ProtocolHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := b9ReadPossiblyZstdRequestRaw(req)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.calls++
	u.headers = req.Header.Clone()
	u.body = append([]byte(nil), body...)
	u.mu.Unlock()
	if strings.HasSuffix(req.URL.Path, "/compact") {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"resp_r2_compact","status":"completed","model":"gpt-5.6-sol","output":[{"id":"cmp_r2","type":"compaction","status":"completed","encrypted_content":"r2-opaque-cipher","summary":[{"type":"summary_text","text":"r2 compact summary"}],"opaque":{"kept":true,"nested":{"n":9007199254740993}}}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			)),
		}, nil
	}
	if gjson.GetBytes(body, "stream").Bool() {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_r2_sse\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n",
			)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_r2_http","object":"response","created_at":1,"status":"completed","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}, nil
}

func (u *r2ProtocolHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *r2ProtocolHTTPUpstream) captured() (http.Header, []byte, int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.headers.Clone(), append([]byte(nil), u.body...), u.calls
}

func b9ReadPossiblyZstdRequestRaw(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "zstd") {
		return body, nil
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	return decoder.DecodeAll(body, nil)
}

func r2StateDigest(secret, domain, value string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("sub2api-codex-r2-state-v1\x00"))
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

func r2ProtocolConfig(accountID int64, mappingKey string) *config.Config {
	cfg := b9ProtocolConfig()
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
		ObserverQueueCapacity:  64,
		ObserverEventMaxBytes:  4096,
		ObserverBatchSize:      16,
		ObserverFlushSeconds:   5,
	}
	return cfg
}

func r2ProtocolRouter(t *testing.T, cfg *config.Config, account service.Account, clientKey string, upstream service.HTTPUpstream, state *service.CodexR2StateService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(4242)
	group := &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformOpenAI}
	user := &service.User{ID: 4343, Role: service.RoleUser, Status: service.StatusActive, Balance: 10, Concurrency: 4}
	apiKey := &service.APIKey{ID: 4444, UserID: user.ID, Key: clientKey, Status: service.StatusActive, User: user, GroupID: &groupID, Group: group}
	apiKeyRepo := &b9ProtocolAPIKeyRepo{apiKey: apiKey}
	apiKeyService := service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, cfg)
	accountRepo := &b9ProtocolAccountRepo{account: account}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	gateway.SetCodexR2StateService(state)
	concurrency := service.NewConcurrencyService(&b9ProtocolConcurrencyCache{})
	h := handler.NewOpenAIGatewayHandler(gateway, concurrency, billingCache, apiKeyService, nil, nil, nil, nil, cfg)
	auth := servermiddleware.NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)
	r := gin.New()
	r.Use(handler.InboundEndpointMiddleware())
	r.POST("/v1/responses", gin.HandlerFunc(auth), h.Responses)
	r.POST("/v1/responses/*subpath", gin.HandlerFunc(auth), h.Responses)
	return r
}

func expectR2ProtocolBinding(t *testing.T, mock sqlmock.Sqlmock, accountID int64, mappingKey, sessionID, chatgptAccountID string) {
	t.Helper()
	authDigest := r2StateDigest(mappingKey, "auth-scope", "api-key:4444")
	sessionDigest := r2StateDigest(mappingKey, "session", sessionID)
	namespaceDigest := r2StateDigest(mappingKey, "credential-scope", "chatgpt:"+chatgptAccountID)
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

func TestR2AuthenticatedNewSessionAdmissionCreatesBindingServerSide(t *testing.T) {
	const (
		accountID        = int64(9011)
		mappingKey       = "01234567890123456789012345678901"
		sessionID        = "0199a1b2-c3d4-7e5f-8a9b-141414141414"
		threadID         = "0199a1b2-c3d4-7e5f-8a9b-151515151515"
		installID        = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		windowID         = "0199a1b2-c3d4-7e5f-8a9b-161616161616"
		clientUA         = "codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm"
		chatgptAccountID = "synthetic-r2-admission-account"
	)
	clientKey := b9ProtocolSecret(t)
	cfg := r2ProtocolConfig(accountID, mappingKey)
	cfg.Gateway.CodexR2.NewSessionAdmission = true
	account := service.Account{
		ID: accountID, Name: "r2-admission", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{
			"access_token":       "synthetic-admission-token",
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
	now := time.Unix(1_700_000_100, 0).UTC()
	mock.ExpectQuery("INSERT INTO codex_r2_policy_bindings").
		WithArgs(
			accountID, authDigest, sessionDigest, "codex-0.154-profile-r1", "r2-v1",
			"hmac-sha256-v1", "epoch-1", namespaceDigest, config.CodexR2ClientUAModePreserveValidated,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "account_id", "auth_scope_digest", "session_digest", "profile_revision", "policy_revision",
			"mapping_algorithm", "mapping_key_epoch", "namespace_digest", "ua_policy", "status", "version", "created_at", "updated_at",
		}).AddRow(
			2, accountID, authDigest, sessionDigest, "codex-0.154-profile-r1", "r2-v1",
			"hmac-sha256-v1", "epoch-1", namespaceDigest, config.CodexR2ClientUAModePreserveValidated,
			"active", 1, now, now,
		))
	state := service.NewCodexR2StateService(db, cfg)
	upstream := &r2ProtocolHTTPUpstream{}
	router := r2ProtocolRouter(t, cfg, account, clientKey, upstream, state)

	metadataBytes, err := json.Marshal(map[string]any{
		"installation_id": installID, "session_id": sessionID, "thread_id": threadID,
		"turn_id": "0199a1b2-c3d4-7e5f-8a9b-171717171717", "window_id": windowID, "request_kind": "turn",
	})
	require.NoError(t, err)
	metadata := string(metadataBytes)
	bodyBytes, err := json.Marshal(map[string]any{
		"model": "gpt-5.6-sol", "input": "synthetic-r2-admission", "stream": false,
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
	_, _, calls := upstream.captured()
	require.Equal(t, 1, calls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestR2AuthenticatedHTTPPreservesClientProfileAndSemanticIdentity(t *testing.T) {
	const (
		accountID  = int64(9008)
		apiKeyID   = int64(4444)
		mappingKey = "01234567890123456789012345678901"
		sessionID  = "0199a1b2-c3d4-7e5f-8a9b-111111111111"
		threadID   = "0199a1b2-c3d4-7e5f-8a9b-222222222222"
		turnID     = "0199a1b2-c3d4-7e5f-8a9b-333333333333"
		installID  = "44444444-4444-4444-8444-444444444444"
		windowID   = "0199a1b2-c3d4-7e5f-8a9b-555555555555"
		clientUA   = "codex-tui/0.154.0 (Windows 10.0.26200; x86_64) WindowsTerminal"
	)
	clientKey := b9ProtocolSecret(t)
	cfg := r2ProtocolConfig(accountID, mappingKey)
	account := service.Account{
		ID: accountID, Name: "r2-protocol-http", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{
			"access_token":       "synthetic-oauth-token",
			"chatgpt_account_id": "synthetic-chatgpt-account",
		},
	}

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	authDigest := r2StateDigest(mappingKey, "auth-scope", "api-key:4444")
	sessionDigest := r2StateDigest(mappingKey, "session", sessionID)
	namespaceDigest := r2StateDigest(mappingKey, "credential-scope", "chatgpt:synthetic-chatgpt-account")
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
	state := service.NewCodexR2StateService(db, cfg)
	upstream := &r2ProtocolHTTPUpstream{}
	router := r2ProtocolRouter(t, cfg, account, clientKey, upstream, state)

	turnMetadataBytes, err := json.Marshal(map[string]any{
		"installation_id": installID,
		"session_id":      sessionID,
		"thread_id":       threadID,
		"turn_id":         turnID,
		"window_id":       windowID,
		"request_kind":    "turn",
	})
	require.NoError(t, err)
	turnMetadata := string(turnMetadataBytes)
	bodyBytes, err := json.Marshal(map[string]any{
		"model":            "gpt-5.6-sol",
		"input":            "synthetic-r2",
		"stream":           false,
		"prompt_cache_key": sessionID,
		"client_metadata": map[string]any{
			"session_id":              sessionID,
			"thread_id":               threadID,
			"x-codex-installation-id": installID,
			"x-codex-window-id":       windowID,
			"x-codex-turn-metadata":   turnMetadata,
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
	req.Header.Set("x-codex-turn-metadata", turnMetadata)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// OAuth Responses may use upstream SSE internally even for a non-streaming
	// client and fold the terminal event back into JSON.
	require.Equal(t, "resp_r2_sse", gjson.GetBytes(w.Body.Bytes(), "id").String())

	headers, upstreamBody, calls := upstream.captured()
	require.Equal(t, 1, calls)
	require.Equal(t, "Bearer synthetic-oauth-token", headers.Get("Authorization"))
	require.Equal(t, "synthetic-chatgpt-account", headers.Get("chatgpt-account-id"))
	require.Equal(t, clientUA, headers.Get("User-Agent"))
	require.Equal(t, "codex-tui", headers.Get("originator"))
	require.Equal(t, "0.154.0", headers.Get("version"))
	require.NotEqual(t, sessionID, headers.Get("session-id"))
	require.NotEqual(t, threadID, headers.Get("thread-id"))
	require.Equal(t, headers.Get("thread-id"), headers.Get("x-client-request-id"))
	require.Equal(t, headers.Get("session-id"), gjson.GetBytes(upstreamBody, "prompt_cache_key").String())
	require.Equal(t, headers.Get("session-id"), gjson.GetBytes(upstreamBody, "client_metadata.session_id").String())
	require.Equal(t, headers.Get("thread-id"), gjson.GetBytes(upstreamBody, "client_metadata.thread_id").String())
	require.NotEqual(t, installID, headers.Get("x-codex-installation-id"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestR2AuthenticatedSSEAndCompactProtocolSurface(t *testing.T) {
	const (
		accountID        = int64(9009)
		mappingKey       = "01234567890123456789012345678901"
		sessionID        = "0199a1b2-c3d4-7e5f-8a9b-aaaaaaaaaaaa"
		threadID         = "0199a1b2-c3d4-7e5f-8a9b-bbbbbbbbbbbb"
		installID        = "88888888-8888-4888-8888-888888888888"
		windowID         = "0199a1b2-c3d4-7e5f-8a9b-cccccccccccc"
		clientUA         = "codex-tui/0.154.0 (Mac OS 26.4; arm64) Terminal"
		chatgptAccountID = "synthetic-r2-surface-account"
	)
	clientKey := b9ProtocolSecret(t)
	cfg := r2ProtocolConfig(accountID, mappingKey)
	account := service.Account{
		ID: accountID, Name: "r2-protocol-surface", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{
			"access_token":       "synthetic-surface-token",
			"chatgpt_account_id": chatgptAccountID,
		},
		Extra: map[string]any{"openai_compact_mode": service.OpenAICompactModeForceOn, "openai_ws_force_http": true},
	}
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	expectR2ProtocolBinding(t, mock, accountID, mappingKey, sessionID, chatgptAccountID)
	expectR2ProtocolBinding(t, mock, accountID, mappingKey, sessionID, chatgptAccountID)
	state := service.NewCodexR2StateService(db, cfg)
	upstream := &r2ProtocolHTTPUpstream{}
	router := r2ProtocolRouter(t, cfg, account, clientKey, upstream, state)

	makeRequest := func(path string, stream bool, turnID string) *httptest.ResponseRecorder {
		metadataBytes, marshalErr := json.Marshal(map[string]any{
			"installation_id": installID, "session_id": sessionID, "thread_id": threadID,
			"turn_id": turnID, "window_id": windowID, "request_kind": "turn",
		})
		require.NoError(t, marshalErr)
		metadata := string(metadataBytes)
		bodyBytes, marshalErr := json.Marshal(map[string]any{
			"model": "gpt-5.6-sol", "input": "synthetic-r2-surface", "stream": stream,
			"prompt_cache_key": sessionID,
			"client_metadata": map[string]any{
				"session_id": sessionID, "thread_id": threadID,
				"x-codex-installation-id": installID, "x-codex-window-id": windowID,
				"x-codex-turn-metadata": metadata,
			},
		})
		require.NoError(t, marshalErr)
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(bodyBytes)))
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
		return w
	}

	sse := makeRequest("/v1/responses", true, "0199a1b2-c3d4-7e5f-8a9b-dddddddddd01")
	require.Equal(t, http.StatusOK, sse.Code, sse.Body.String())
	require.Contains(t, sse.Body.String(), `"id":"resp_r2_sse"`)
	require.Equal(t, 1, strings.Count(sse.Body.String(), `"type":"response.completed"`))

	compact := makeRequest("/v1/responses/compact", false, "0199a1b2-c3d4-7e5f-8a9b-dddddddddd02")
	require.Equal(t, http.StatusOK, compact.Code, compact.Body.String())
	require.Equal(t, "r2-opaque-cipher", gjson.GetBytes(compact.Body.Bytes(), "output.0.encrypted_content").String())
	require.Equal(t, "r2 compact summary", gjson.GetBytes(compact.Body.Bytes(), "output.0.summary.0.text").String())
	require.True(t, gjson.GetBytes(compact.Body.Bytes(), "output.0.opaque.kept").Bool())
	require.Equal(t, "9007199254740993", gjson.GetBytes(compact.Body.Bytes(), "output.0.opaque.nested.n").Raw)

	headers, body, calls := upstream.captured()
	require.Equal(t, 2, calls)
	require.Equal(t, clientUA, headers.Get("User-Agent"))
	mappedSession := gjson.GetBytes(body, "prompt_cache_key").String()
	mappedBodySession := gjson.GetBytes(body, "client_metadata.session_id").String()
	mappedBodyThread := gjson.GetBytes(body, "client_metadata.thread_id").String()
	if mappedSession != "" {
		require.NotEqual(t, sessionID, mappedSession)
	}
	if mappedBodySession != "" {
		require.NotEqual(t, sessionID, mappedBodySession)
		if mappedSession != "" {
			require.Equal(t, mappedSession, mappedBodySession)
		}
	}
	if mappedBodyThread != "" {
		require.NotEqual(t, threadID, mappedBodyThread)
	}
	serialized := string(body)
	require.NotContains(t, serialized, sessionID)
	require.NotContains(t, serialized, threadID)
	require.NotContains(t, serialized, installID)
	// Compact does not promise the ordinary turn header carrier set. If the
	// thread projection is present in headers, its alias relation must hold.
	if mappedHeaderThread := headers.Get("thread-id"); mappedHeaderThread != "" {
		require.Equal(t, mappedHeaderThread, headers.Get("x-client-request-id"))
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestR2AuthenticatedPassthroughUsesSingleSemanticWriter(t *testing.T) {
	const (
		accountID        = int64(9010)
		mappingKey       = "01234567890123456789012345678901"
		sessionID        = "0199a1b2-c3d4-7e5f-8a9b-eeeeeeeeeeee"
		threadID         = "0199a1b2-c3d4-7e5f-8a9b-ffffffffffff"
		installID        = "99999999-9999-4999-8999-999999999999"
		windowID         = "0199a1b2-c3d4-7e5f-8a9b-121212121212"
		clientUA         = "codex-tui/0.154.0 (Windows 10.0.26100; x86_64) PowerShell"
		chatgptAccountID = "synthetic-r2-passthrough-account"
	)
	clientKey := b9ProtocolSecret(t)
	cfg := r2ProtocolConfig(accountID, mappingKey)
	account := service.Account{
		ID: accountID, Name: "r2-passthrough", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{
			"access_token":       "synthetic-passthrough-token",
			"chatgpt_account_id": chatgptAccountID,
		},
		Extra: map[string]any{"openai_passthrough": true, "openai_ws_force_http": true},
	}
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	expectR2ProtocolBinding(t, mock, accountID, mappingKey, sessionID, chatgptAccountID)
	state := service.NewCodexR2StateService(db, cfg)
	upstream := &r2ProtocolHTTPUpstream{}
	router := r2ProtocolRouter(t, cfg, account, clientKey, upstream, state)
	metadataBytes, err := json.Marshal(map[string]any{
		"installation_id": installID, "session_id": sessionID, "thread_id": threadID,
		"turn_id": "0199a1b2-c3d4-7e5f-8a9b-131313131313", "window_id": windowID, "request_kind": "turn",
	})
	require.NoError(t, err)
	metadata := string(metadataBytes)
	bodyBytes, err := json.Marshal(map[string]any{
		"model": "gpt-5.6-sol", "input": "r2 passthrough", "stream": false,
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
	headers, upstreamBody, calls := upstream.captured()
	require.Equal(t, 1, calls)
	require.Equal(t, clientUA, headers.Get("User-Agent"))
	require.NotEqual(t, sessionID, headers.Get("session-id"))
	require.NotEqual(t, threadID, headers.Get("thread-id"))
	require.Equal(t, headers.Get("thread-id"), headers.Get("x-client-request-id"))
	require.Equal(t, headers.Get("session-id"), gjson.GetBytes(upstreamBody, "prompt_cache_key").String())
	require.Equal(t, headers.Get("thread-id"), gjson.GetBytes(upstreamBody, "client_metadata.thread_id").String())
	require.NotContains(t, string(upstreamBody), sessionID)
	require.NotContains(t, string(upstreamBody), threadID)
	require.NoError(t, mock.ExpectationsWereMet())
}
