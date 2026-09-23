package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Reproduces a chained-sub2api setup without needing a second database:
// layer 1 applies a group mapping before OpenAIGatewayService.Forward, then
// layer 2 observes layer 1's client-facing response exactly as another
// sub2api upstream-response observer would.
func TestManualGroupMappingChain_RestoresPublicModelForDownstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	group := &Group{
		Platform: PlatformOpenAI,
		ModelAllowlist: GroupModelAllowlist{ModelMapping: map[string]string{
			"gpt-6-sol": "gpt-5.6-sol",
		}},
	}
	mapping := ResolveGroupMappingWithoutChannel(group, "gpt-6-sol")
	require.True(t, mapping.Mapped)
	require.Equal(t, "gpt-5.6-sol", mapping.MappedModel)

	clientBody := []byte(`{"model":"gpt-6-sol","stream":false,"input":"hello"}`)
	layer1ForwardBody := ReplaceModelInBody(clientBody, mapping.MappedModel)
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(layer1ForwardBody, "model").String())

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"resp_chain","object":"response","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			)),
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID:          99,
		Name:        "layer1-upstream",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://example.com",
		},
		Extra: map[string]any{"use_responses_api": true},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = req.WithContext(WithOpenAIManualResponseModelAlias(req.Context(), "gpt-6-sol", "gpt-5.6-sol"))
	c.Request = req
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

	result, err := svc.Forward(context.Background(), c, account, layer1ForwardBody)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gpt-5.6-sol", result.UpstreamModel)
	require.Equal(t, "gpt-5.6-sol", observedUpstreamResponseModel(c))

	// The manually configured implementation model is hidden at the public
	// response boundary, while internal observation above stays real.
	require.Equal(t, "gpt-6-sol", gjson.Get(rec.Body.String(), "model").String())

	// A second sub2api therefore observes the public alias, not the manual route.
	layer2Observer := &upstreamResponseModelObserver{}
	layer2Observer.ObserveOpenAI(rec.Body.Bytes(), "")
	require.Equal(t, "gpt-6-sol", layer2Observer.Model())
}

func TestManualGroupMappingChain_GenuineUpstreamDowngradeRemainsVisible(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"resp_chain_downgrade","object":"response","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			)),
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID:          100,
		Name:        "layer1-upstream-downgrade",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://example.com",
		},
		Extra: map[string]any{"use_responses_api": true},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = req.WithContext(WithOpenAIManualResponseModelAlias(req.Context(), "gpt-6-sol", "gpt-5.6-sol"))
	c.Request = req
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.6-sol","stream":false,"input":"hello"}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gpt-5.6-sol", result.UpstreamModel)
	require.Equal(t, "gpt-5.5", observedUpstreamResponseModel(c))

	// The upstream returned something other than the frozen manually routed
	// model, so this is a genuine upstream/official change and must stay visible.
	require.Equal(t, "gpt-5.5", gjson.Get(rec.Body.String(), "model").String())
	layer2Observer := &upstreamResponseModelObserver{}
	layer2Observer.ObserveOpenAI(rec.Body.Bytes(), "")
	require.Equal(t, "gpt-5.5", layer2Observer.Model())
}

func TestManualGroupMappingChain_ChatCompletionsPreservesSameBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name          string
		upstreamModel string
		wantClient    string
	}{
		{name: "manual route echo is restored", upstreamModel: "gpt-5.6-sol", wantClient: "gpt-6-sol"},
		{name: "genuine upstream downgrade stays visible", upstreamModel: "gpt-5.5", wantClient: "gpt-5.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{
				resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader(
						`data: {"type":"response.completed","response":{"id":"resp_chat_chain","object":"response","model":"` + tt.upstreamModel + `","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n",
					)),
				},
			}
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = false
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{
				ID:          101,
				Name:        "chat-layer1-upstream",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{
					"api_key":  "sk-test",
					"base_url": "https://example.com",
				},
				Extra: map[string]any{"use_responses_api": true},
			}

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			req = req.WithContext(WithOpenAIManualResponseModelAlias(req.Context(), "gpt-6-sol", "gpt-5.6-sol"))
			c.Request = req
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

			result, err := svc.ForwardAsChatCompletions(
				context.Background(),
				c,
				account,
				[]byte(`{"model":"gpt-5.6-sol","stream":false,"messages":[{"role":"user","content":"hello"}]}`),
				"",
				"",
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.upstreamModel, observedUpstreamResponseModel(c))
			require.Equal(t, tt.wantClient, gjson.Get(rec.Body.String(), "model").String())
		})
	}
}
