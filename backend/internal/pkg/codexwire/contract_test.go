package codexwire

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractRegistryPinnedAndStable(t *testing.T) {
	contract, ok := ContractForProfile("codex-0.155.1-profile-r1")
	require.True(t, ok)
	require.Equal(t, "be2951ea34f0d295ed0becf97079f92fa5f6950e", contract.ReferenceCommit)
	require.True(t, contract.EvidenceComplete)
	require.True(t, contract.SupportsPurpose("websocket"))
	require.Len(t, contract.Digest(), 64)
	copy := contract
	copy.GraphRevision = "changed"
	require.NotEqual(t, contract.Digest(), copy.Digest())
	_, ok = ContractForProfile("codex-9.9.9-profile")
	require.False(t, ok)
}

func TestParseTurnMetadataPreservesTypesAndUnknown(t *testing.T) {
	parsed, err := ParseTurnMetadata(`{
		"session_id":"s",
		"window_number":9007199254740993,
		"analytics_enabled":false,
		"workspaces":[{"root":"/synthetic"}],
		"sandbox":{"mode":"workspace-write"},
		"future_field":{"n":9007199254740993}
	}`)
	require.NoError(t, err)
	require.Equal(t, KindString, parsed.Fields["session_id"].Kind)
	require.Equal(t, KindNumber, parsed.Fields["window_number"].Kind)
	require.Equal(t, KindBool, parsed.Fields["analytics_enabled"].Kind)
	require.Equal(t, KindArray, parsed.Fields["workspaces"].Kind)
	require.Equal(t, KindObject, parsed.Fields["sandbox"].Kind)
	require.JSONEq(t, `{"n":9007199254740993}`, string(parsed.Unknown["future_field"]))
	require.Equal(t, "s", mustMetadataString(t, parsed, "session_id"))
}

func TestParseTurnMetadataRejectsDuplicateKeys(t *testing.T) {
	_, err := ParseTurnMetadata(`{"session_id":"a","session_id":"b"}`)
	require.ErrorContains(t, err, "duplicate")
}

func TestParseEmbeddedTurnMetadataDoesNotRequireOrRebuildUnknownSiblings(t *testing.T) {
	body := []byte(`{"client_metadata":{"x-codex-turn-metadata":"{\"session_id\":\"s\",\"future\":{\"n\":9007199254740993}}","large_unknown":{"keep":true}},"input":"literal"}`)
	parsed, present, err := ParseEmbeddedTurnMetadata(body)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, "s", mustMetadataString(t, parsed, "session_id"))
	require.JSONEq(t, `{"n":9007199254740993}`, string(parsed.Unknown["future"]))
}

func mustMetadataString(t *testing.T, metadata TurnMetadata, name string) string {
	t.Helper()
	value, ok := metadata.String(name)
	require.True(t, ok)
	return value
}

func TestRelationalNormalizationKeepsMergeSplitVisible(t *testing.T) {
	expected := []WireField{
		{Path: "session", Domain: "session", Value: "raw-s"},
		{Path: "cache", Domain: "session", Value: "raw-s"},
		{Path: "thread", Domain: "thread", Value: "raw-t"},
	}
	actual := []WireField{
		{Path: "session", Domain: "session", Value: "mapped-s"},
		{Path: "cache", Domain: "session", Value: "mapped-s"},
		{Path: "thread", Domain: "thread", Value: "mapped-t"},
	}
	require.Empty(t, Compare(expected, actual))
	actual[1].Value = "different"
	mismatches := Compare(expected, actual)
	require.Len(t, mismatches, 1)
	require.Equal(t, "cache", mismatches[0].Path)
}
