package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestR1B6TransportFailoverRequiresProvablyPreSendFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name     string
		err      error
		wantNext bool
	}{
		{"dial_connection_refused", errors.New(`dial tcp 1.2.3.4:443: connect: connection refused`), true},
		{"dial_no_route", errors.New(`dial tcp 1.2.3.4:443: connect: no route to host`), true},
		{"read_connection_reset", errors.New(`read tcp 10.0.0.1:5->2.2.2.2:443: read: connection reset by peer`), false},
		{"awaiting_headers_timeout", errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`), false},
		{"unexpected_eof", io.ErrUnexpectedEOF, false},
		{"broken_pipe", errors.New(`write tcp 10.0.0.1:5->2.2.2.2:443: write: broken pipe`), false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			svc := &OpenAIGatewayService{}
			err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, &Account{ID: 8008, Platform: PlatformOpenAI}, tt.err, false)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Equal(t, tt.wantNext, failoverErr.ShouldRetryNextAccount(), "cross-account replay must require a provably pre-send failure")
		})
	}
}

func TestR1B6HTTPResponseBodyReadFailureDoesNotReplayAcrossAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"r1-b6-read"}},
		Body:       &openAICompatBufferedReadErrorCloser{err: io.ErrUnexpectedEOF},
	}
	result, err := (&OpenAIGatewayService{}).handleChatBufferedStreamingResponse(resp, c, &Account{ID: 8008, Platform: PlatformOpenAI}, "gpt-5.6-sol", "gpt-5.6-sol", "gpt-5.6-sol", time.Now())
	require.Nil(t, result)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, failoverErr.ShouldRetryNextAccount(), "an HTTP response proves the upstream received the request; a later read error is replay-unsafe")
}

func TestR1B6FirstOutputTimeoutDoesNotReplayAcrossAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Gateway: config.GatewayConfig{OpenAIFirstOutputTimeoutSeconds: 1, MaxLineSize: defaultMaxLineSize}}
	svc := &OpenAIGatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	pr, pw := io.Pipe()
	body := &firstOutputCloseTrackingBody{ReadCloser: pr, closed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer pw.Close()
		_, _ = io.WriteString(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_r1_b6\"}}\n\n")
		select {
		case <-body.closed:
		case <-time.After(2 * time.Second):
		}
	}()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Request-Id": []string{"r1-b6-timeout"}}, Body: body}
	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 8008, Platform: PlatformOpenAI}, time.Now(), "gpt-5.6-sol", "gpt-5.6-sol")
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, failoverErr.ShouldRetryNextAccount(), "first-output timeout happens after upstream response dispatch and is not proof of non-execution")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("upstream writer did not exit")
	}
}

func TestR1B6ExplicitBusinessFailoverKeepsExistingNextAccountPolicy(t *testing.T) {
	failoverErr := &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, RequestScopedTransient: true}
	require.True(t, failoverErr.ShouldRetryNextAccount())
}
