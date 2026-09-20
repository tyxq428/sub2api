package codexidentity

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// This synthetic cardinality check guards the R2/R2.2 invariant that mapper
// scope is part of the identity domain. It intentionally uses no global cache
// or production state: each mapped value is derived from the frozen HMAC
// inputs and is checked for accidental merge/reuse.
func TestMapperHighCardinalityNoMergeAcrossScopes(t *testing.T) {
	const count = 50_000
	secret := []byte("01234567890123456789012345678901")
	mapperA, err := NewMapper(MappingScope{
		Secret: secret, AuthScope: "api-key:synthetic-a", CredentialScope: "oauth:synthetic-a",
		Algorithm: "hmac-sha256-v1", KeyEpoch: "epoch-1",
	})
	require.NoError(t, err)
	mapperB, err := NewMapper(MappingScope{
		Secret: secret, AuthScope: "api-key:synthetic-a", CredentialScope: "oauth:synthetic-b",
		Algorithm: "hmac-sha256-v1", KeyEpoch: "epoch-1",
	})
	require.NoError(t, err)

	seenA := make(map[string]struct{}, count)
	seenB := make(map[string]struct{}, count)
	for i := 0; i < count; i++ {
		raw := fmt.Sprintf("session-%08d", i)
		mappedA, mapErr := mapperA.Map("session", raw)
		require.NoError(t, mapErr)
		mappedB, mapErr := mapperB.Map("session", raw)
		require.NoError(t, mapErr)
		require.NotEqual(t, mappedA, mappedB, "credential scopes must not share mapped session identities")
		if _, exists := seenA[mappedA]; exists {
			t.Fatalf("scope A collision at %d: %s", i, mappedA)
		}
		if _, exists := seenB[mappedB]; exists {
			t.Fatalf("scope B collision at %d: %s", i, mappedB)
		}
		seenA[mappedA] = struct{}{}
		seenB[mappedB] = struct{}{}
	}
	require.Len(t, seenA, count)
	require.Len(t, seenB, count)
}
