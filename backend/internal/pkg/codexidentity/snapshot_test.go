package codexidentity

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaptureIsImmutableAndExtractsKnownCarriers(t *testing.T) {
	headers := http.Header{
		"User-Agent":            {"codex-tui/0.154.0 (Windows 11; x86_64) terminal"},
		"Session-Id":            {"session-a"},
		"Thread-Id":             {"thread-a"},
		"X-Client-Request-Id":   {"thread-a"},
		"X-Codex-Turn-Metadata": {`{"session_id":"session-a","thread_id":"thread-a","turn_id":"turn-a","parent_thread_id":"thread-parent","request_kind":"turn"}`},
	}
	body := []byte(`{"prompt_cache_key":"session-a","input":"private prompt must not be captured","client_metadata":{"session_id":"session-a","thread_id":"thread-a","x-codex-turn-metadata":"{\"session_id\":\"session-a\",\"thread_id\":\"thread-a\",\"turn_id\":\"turn-a\"}"}}`)
	originalBody := append([]byte(nil), body...)
	originalUA := headers.Get("User-Agent")

	snapshot := Capture(headers, body, DefaultLimits())
	require.True(t, snapshot.Coverage().Complete)
	require.NotEmpty(t, snapshot.Fields())
	require.Equal(t, originalBody, body)
	require.Equal(t, originalUA, headers.Get("User-Agent"))

	fields := snapshot.Fields()
	fields[0].Value = "mutated"
	require.NotEqual(t, "mutated", snapshot.Fields()[0].Value)
	serialized := make([]string, 0, len(snapshot.Fields()))
	for _, field := range snapshot.Fields() {
		serialized = append(serialized, field.Name+"="+field.Value)
	}
	require.NotContains(t, strings.Join(serialized, "\n"), "private prompt")
}

func TestCaptureReportsBoundsWithoutRejectingRequest(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxIdentityValueBytes = 8
	limits.MaxMetadataBytes = 32
	headers := http.Header{"Session-Id": {"session-value-too-long"}}
	snapshot := Capture(headers, []byte(`{"client_metadata":{"session_id":"session-a","padding":"012345678901234567890123456789"}}`), limits)
	coverage := snapshot.Coverage()
	require.False(t, coverage.Complete)
	require.Contains(t, coverage.Reasons, "identity_value_too_large")
	require.Contains(t, coverage.Reasons, "client_metadata_too_large")
	require.Empty(t, snapshot.Fields())
}

func TestCaptureDoesNotPartiallyExtractOversizedClientMetadata(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxMetadataBytes = 64
	body := []byte(`{"prompt_cache_key":"cache-a","client_metadata":{"session_id":"session-a","thread_id":"thread-a","padding":"0123456789012345678901234567890123456789"}}`)
	snapshot := Capture(nil, body, limits)
	coverage := snapshot.Coverage()
	require.False(t, coverage.Complete)
	require.Contains(t, coverage.Reasons, "client_metadata_too_large")
	require.Len(t, snapshot.Fields(), 1)
	require.Equal(t, "prompt_cache_key", snapshot.Fields()[0].Name)
}

func TestCaptureLargePromptStillObservesBoundedClientMetadata(t *testing.T) {
	prompt := strings.Repeat("x", 300*1024)
	body := []byte(`{"input":"` + prompt + `","client_metadata":{"session_id":"session-a","thread_id":"thread-a"}}`)
	snapshot := Capture(nil, body, DefaultLimits())
	require.True(t, snapshot.Coverage().Complete)
	values := make(map[string]string)
	for _, field := range snapshot.Fields() {
		values[field.Name] = field.Value
	}
	require.Equal(t, "session-a", values["client_metadata.session_id"])
	require.Equal(t, "thread-a", values["client_metadata.thread_id"])
}

func TestCaptureUnknownOrMalformedMetadataIsExplicitlyIncomplete(t *testing.T) {
	snapshot := Capture(http.Header{"X-Codex-Turn-Metadata": {`{"thread_id":{"unexpected":true}}`}}, []byte(`{"client_metadata":{"thread_id":123}}`), DefaultLimits())
	coverage := snapshot.Coverage()
	require.False(t, coverage.Complete)
	require.Contains(t, coverage.Reasons, "identity_field_non_string")
	require.Contains(t, coverage.Reasons, "turn_metadata_field_unsupported")
}

func TestParseModeFailsClosed(t *testing.T) {
	require.Equal(t, ModeOff, ParseMode(""))
	require.Equal(t, ModeOff, ParseMode("unknown"))
	require.Equal(t, ModeShadow, ParseMode(" SHADOW "))
	require.Equal(t, ModeEnforce, ParseMode("enforce"))
}

func FuzzCaptureNeverMutatesInput(f *testing.F) {
	f.Add("session-a", `{"client_metadata":{"thread_id":"thread-a"}}`)
	f.Fuzz(func(t *testing.T, session, rawBody string) {
		if len(session) > 4096 || len(rawBody) > 64*1024 {
			t.Skip()
		}
		headers := http.Header{"Session-Id": {session}}
		beforeHeader := headers.Get("Session-Id")
		body := []byte(rawBody)
		beforeBody := append([]byte(nil), body...)
		_ = Capture(headers, body, DefaultLimits())
		require.Equal(t, beforeHeader, headers.Get("Session-Id"))
		require.Equal(t, beforeBody, body)
	})
}
