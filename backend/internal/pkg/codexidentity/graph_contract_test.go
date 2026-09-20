package codexidentity

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptCacheRelationshipIsProfileScoped(t *testing.T) {
	headers := http.Header{
		"User-Agent": {"codex-tui/0.154.0 (Windows; x86_64) terminal"},
		"Session-Id": {"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	}
	body := []byte(`{"prompt_cache_key":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","client_metadata":{"session_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}}`)
	profile, ok := ProfileByID(Codex0154ProfileID)
	require.True(t, ok)

	graph := BuildGraph(Capture(headers, body, DefaultLimits()), profile)
	require.Equal(t, RoleSession, projectionRole(t, graph, "prompt_cache_key"))

	profile.PromptCachePolicy = PromptCacheIndependent
	graph = BuildGraph(Capture(headers, body, DefaultLimits()), profile)
	require.Equal(t, RolePromptCache, projectionRole(t, graph, "prompt_cache_key"))
}

func TestExplicitPromptCacheOverrideRemainsIndependent(t *testing.T) {
	headers := http.Header{
		"User-Agent": {"codex-tui/0.154.0 (Windows; x86_64) terminal"},
		"Session-Id": {"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	}
	body := []byte(`{"prompt_cache_key":"explicit-cache","client_metadata":{"session_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}}`)
	profile, ok := ProfileByID(Codex0154ProfileID)
	require.True(t, ok)
	graph := BuildGraph(Capture(headers, body, DefaultLimits()), profile)
	require.Equal(t, RolePromptCache, projectionRole(t, graph, "prompt_cache_key"))
}

func projectionRole(t *testing.T, graph SemanticGraph, fieldName string) SemanticRole {
	t.Helper()
	for _, projection := range graph.Projections {
		if projection.Field.Name == fieldName {
			return projection.Role
		}
	}
	t.Fatalf("projection %q not found", fieldName)
	return RoleUnknown
}
