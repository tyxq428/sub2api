//go:build unit

package routes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type b9ProtocolAPIKeyRepo struct {
	service.APIKeyRepository
	apiKey *service.APIKey
}

func (r *b9ProtocolAPIKeyRepo) GetByKeyForAuth(_ context.Context, key string) (*service.APIKey, error) {
	if r.apiKey == nil || key != r.apiKey.Key {
		return nil, service.ErrAPIKeyNotFound
	}
	clone := *r.apiKey
	return &clone, nil
}

func (r *b9ProtocolAPIKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

type b9ProtocolAccountRepo struct {
	service.AccountRepository
	account service.Account
}

func (r *b9ProtocolAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if id != r.account.ID {
		return nil, service.ErrNoAvailableAccounts
	}
	clone := r.account
	return &clone, nil
}

func (r *b9ProtocolAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	if platform != r.account.Platform {
		return nil, nil
	}
	return []service.Account{r.account}, nil
}

type b9ProtocolConcurrencyCache struct {
	service.ConcurrencyCache
}

func (*b9ProtocolConcurrencyCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (*b9ProtocolConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}
func (*b9ProtocolConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (*b9ProtocolConcurrencyCache) ReleaseUserSlot(context.Context, int64, string) error { return nil }
func (*b9ProtocolConcurrencyCache) TrackAPIKeySlot(context.Context, int64, string) error { return nil }
func (*b9ProtocolConcurrencyCache) ReleaseAPIKeySlot(context.Context, int64, string) error {
	return nil
}
func (*b9ProtocolConcurrencyCache) AcquireOpenAIWSIngressLease(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (*b9ProtocolConcurrencyCache) RefreshOpenAIWSIngressLease(context.Context, int64, string) (bool, error) {
	return true, nil
}
func (*b9ProtocolConcurrencyCache) ReleaseOpenAIWSIngressLease(context.Context, int64, string) error {
	return nil
}

func b9ProtocolSecret(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 24)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return "b9_" + hex.EncodeToString(buf)
}

func b9ProtocolConfig() *config.Config {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxAccountSwitches = 2
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.IngressPreviousResponseRecoveryEnabled = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	return cfg
}

func b9ProtocolRouter(t *testing.T, cfg *config.Config, account service.Account, clientKey string) *gin.Engine {
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
	upstream := repository.NewHTTPUpstream(cfg)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	concurrency := service.NewConcurrencyService(&b9ProtocolConcurrencyCache{})
	h := handler.NewOpenAIGatewayHandler(gateway, concurrency, billingCache, apiKeyService, nil, nil, nil, nil, cfg)
	auth := servermiddleware.NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)
	r := gin.New()
	r.Use(handler.InboundEndpointMiddleware())
	r.POST("/v1/responses", gin.HandlerFunc(auth), h.Responses)
	r.POST("/v1/responses/*subpath", gin.HandlerFunc(auth), h.Responses)
	r.GET("/v1/responses", gin.HandlerFunc(auth), h.ResponsesWebSocket)
	return r
}

func b9ReadPossiblyZstdRequest(t *testing.T, r *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "zstd") {
		return body
	}
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(body, nil)
	require.NoError(t, err)
	return decoded
}

func b9WriteZstd(t *testing.T, w http.ResponseWriter, contentType string, body []byte) {
	t.Helper()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	require.NoError(t, err)
	defer func() { _ = encoder.Close() }()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Encoding", "zstd")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(encoder.EncodeAll(body, nil))
	require.NoError(t, err)
}

func TestR1B9AuthenticatedHTTPProtocolClosure(t *testing.T) {
	clientKey := b9ProtocolSecret(t)
	upstreamKey := b9ProtocolSecret(t)
	var upstreamRequests atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+upstreamKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		upstreamRequests.Add(1)
		body := b9ReadPossiblyZstdRequest(t, r)
		if strings.HasSuffix(r.URL.Path, "/compact") {
			b9WriteZstd(t, w, "application/json", []byte(`{"id":"resp_b9_compact","status":"completed","model":"gpt-5.6-sol","output":[{"id":"cmp_b9","type":"compaction","status":"completed","encrypted_content":"compact-cipher","summary":[{"type":"summary_text","text":"compact summary"}],"opaque":{"kept":true,"nested":{"n":9007199254740993}}}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
			return
		}
		if gjson.GetBytes(body, "stream").Bool() {
			b9WriteZstd(t, w, "text/event-stream", []byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_b9_sse\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
			return
		}
		b9WriteZstd(t, w, "application/json", []byte(`{"id":"resp_b9_json","object":"response","created_at":1,"status":"completed","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	account := service.Account{
		ID: 9001, Name: "b9-protocol-http", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{
			"api_key":               upstreamKey,
			"base_url":              upstream.URL,
			"compact_model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.6-sol"},
		},
		Extra: map[string]any{"openai_ws_force_http": true, "openai_compact_mode": service.OpenAICompactModeForceOn},
	}
	router := b9ProtocolRouter(t, b9ProtocolConfig(), account, clientKey)

	do := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+clientKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("upstream zstd json", func(t *testing.T) {
		w := do("/v1/responses", `{"model":"gpt-5.6-sol","input":"synthetic","stream":false}`)
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "resp_b9_json", gjson.GetBytes(w.Body.Bytes(), "id").String())
		require.Equal(t, "completed", gjson.GetBytes(w.Body.Bytes(), "status").String())
		require.NotEqual(t, "zstd", strings.ToLower(w.Header().Get("Content-Encoding")))
	})

	t.Run("upstream zstd sse", func(t *testing.T) {
		w := do("/v1/responses", `{"model":"gpt-5.6-sol","input":"synthetic","stream":true}`)
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), `"id":"resp_b9_sse"`)
		require.Equal(t, 1, strings.Count(w.Body.String(), `"type":"response.completed"`))
		require.NotEqual(t, "zstd", strings.ToLower(w.Header().Get("Content-Encoding")))
	})

	t.Run("compact opaque structure", func(t *testing.T) {
		w := do("/v1/responses/compact", `{"model":"gpt-5.6-sol","input":"synthetic"}`)
		require.Equal(t, http.StatusOK, w.Code)
		body := w.Body.Bytes()
		require.Equal(t, "compact-cipher", gjson.GetBytes(body, "output.0.encrypted_content").String())
		require.Equal(t, "compact summary", gjson.GetBytes(body, "output.0.summary.0.text").String())
		require.True(t, gjson.GetBytes(body, "output.0.opaque.kept").Bool())
		require.Equal(t, "9007199254740993", gjson.GetBytes(body, "output.0.opaque.nested.n").Raw)
	})

	require.Equal(t, int32(3), upstreamRequests.Load())
}

func TestR1B9AuthenticatedWebSocketMultiTurnContinuationRecovery(t *testing.T) {
	clientKey := b9ProtocolSecret(t)
	upstreamKey := b9ProtocolSecret(t)
	var connCount atomic.Int32
	var mu sync.Mutex
	var captured [][]byte
	upstreamErr := make(chan error, 2)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+upstreamKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			upstreamErr <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		idx := connCount.Add(1)
		read := func() ([]byte, error) {
			ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			defer cancel()
			_, message, readErr := conn.Read(ctx)
			if readErr == nil {
				mu.Lock()
				captured = append(captured, append([]byte(nil), message...))
				mu.Unlock()
			}
			return message, readErr
		}
		write := func(payload string) error {
			ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			defer cancel()
			return conn.Write(ctx, coderws.MessageText, []byte(payload))
		}
		if idx == 1 {
			if _, err = read(); err == nil {
				err = write(`{"type":"response.completed","response":{"id":"resp_b9_ws_1","model":"gpt-5.6-sol","usage":{"input_tokens":1,"output_tokens":1}}}`)
			}
			if err == nil {
				_, err = read()
			}
			if err == nil {
				err = write(`{"type":"error","error":{"type":"invalid_request_error","code":"previous_response_not_found","message":"synthetic missing anchor"}}`)
			}
			if err != nil {
				upstreamErr <- err
			}
			return
		}
		if idx == 2 {
			if _, err = read(); err == nil {
				err = write(`{"type":"response.completed","response":{"id":"resp_b9_ws_2","model":"gpt-5.6-sol","usage":{"input_tokens":1,"output_tokens":1}}}`)
			}
			if err != nil {
				upstreamErr <- err
			}
			return
		}
		upstreamErr <- context.Canceled
	}))
	defer upstream.Close()

	cfg := b9ProtocolConfig()
	account := service.Account{
		ID: 9002, Name: "b9-protocol-ws", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{"api_key": upstreamKey, "base_url": upstream.URL},
		Extra:       map[string]any{"openai_apikey_responses_websockets_v2_enabled": true},
	}
	router := b9ProtocolRouter(t, cfg, account, clientKey)
	app := httptest.NewServer(router)
	defer app.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+clientKey)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	client, response, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(app.URL, "http")+"/v1/responses", &coderws.DialOptions{HTTPHeader: header})
	cancel()
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()

	write := func(payload string) {
		writeCtx, writeCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer writeCancel()
		require.NoError(t, client.Write(writeCtx, coderws.MessageText, []byte(payload)))
	}
	read := func() []byte {
		readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer readCancel()
		_, message, readErr := client.Read(readCtx)
		require.NoError(t, readErr)
		return message
	}

	write(`{"type":"response.create","model":"gpt-5.6-sol","input":"first"}`)
	first := read()
	require.Equal(t, "response.completed", gjson.GetBytes(first, "type").String())
	require.Equal(t, "resp_b9_ws_1", gjson.GetBytes(first, "response.id").String())

	write(`{"type":"response.create","model":"gpt-5.6-sol","input":"second","previous_response_id":"resp_b9_ws_1"}`)
	second := read()
	require.Equal(t, "response.completed", gjson.GetBytes(second, "type").String())
	require.Equal(t, "resp_b9_ws_2", gjson.GetBytes(second, "response.id").String())

	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	require.Eventually(t, func() bool { return connCount.Load() == 2 }, 3*time.Second, 20*time.Millisecond)
	select {
	case upstreamReadErr := <-upstreamErr:
		require.NoError(t, upstreamReadErr)
	default:
	}

	mu.Lock()
	messages := make([][]byte, len(captured))
	copy(messages, captured)
	mu.Unlock()
	require.Len(t, messages, 3)
	require.False(t, gjson.GetBytes(messages[0], "previous_response_id").Exists())
	require.Equal(t, "resp_b9_ws_1", gjson.GetBytes(messages[1], "previous_response_id").String())
	require.False(t, gjson.GetBytes(messages[2], "previous_response_id").Exists(), "recovery retry must drop the missing upstream continuation anchor")
}
