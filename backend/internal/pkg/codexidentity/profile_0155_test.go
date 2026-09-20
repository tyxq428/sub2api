package codexidentity

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodex0155ProfileIsExplicitAndVersionQualified(t *testing.T) {
	headers := http.Header{
		"User-Agent":          {"codex-tui/0.155.1 (Windows 10.0.26200; x86_64) WindowsTerminal"},
		"Originator":          {"codex-tui"},
		"Version":             {"0.155.1"},
		"Session-Id":          {"0199a1b2-c3d4-7e5f-8a9b-111111111111"},
		"Thread-Id":           {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
		"X-Client-Request-Id": {"0199a1b2-c3d4-7e5f-8a9b-222222222222"},
	}
	body := []byte(`{"prompt_cache_key":"0199a1b2-c3d4-7e5f-8a9b-111111111111","client_metadata":{"session_id":"0199a1b2-c3d4-7e5f-8a9b-111111111111","thread_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222"}}`)
	snapshot := Capture(headers, body, DefaultLimits())

	profile0155, ok := ProfileByID(Codex0155ProfileID)
	require.True(t, ok)
	require.Equal(t, "rust-v0.155.1", profile0155.ReferenceTag)
	require.Equal(t, "be2951ea34f0d295ed0becf97079f92fa5f6950e", profile0155.ReferenceCommit)
	plan0155, err := BuildPlan(snapshot, profile0155, testMapper(t, "tenant", "oauth"), BuildOptions{RequireValidatedClient: true})
	require.NoError(t, err)
	require.NoError(t, plan0155.Validate())
	require.True(t, plan0155.Client.Recognized)
	require.Equal(t, "0.155.1", plan0155.Client.Version)

	profile0154, ok := ProfileByID(Codex0154ProfileID)
	require.True(t, ok)
	plan0154, err := BuildPlan(snapshot, profile0154, testMapper(t, "tenant", "oauth"), BuildOptions{RequireValidatedClient: true})
	require.NoError(t, err)
	require.Error(t, plan0154.Validate(), "0.155.1 must not be silently accepted by the frozen 0.154 profile")

	futureHeaders := headers.Clone()
	futureHeaders.Set("User-Agent", "codex-tui/0.156.0 (Windows 10.0.26200; x86_64) WindowsTerminal")
	futureHeaders.Set("Version", "0.156.0")
	futurePlan, err := BuildPlan(
		Capture(futureHeaders, body, DefaultLimits()),
		profile0155,
		testMapper(t, "tenant", "oauth"),
		BuildOptions{RequireValidatedClient: true},
	)
	require.NoError(t, err)
	require.Error(t, futurePlan.Validate(), "unknown future client versions must remain fail-closed")
}
