package codexwire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReferenceManifestDigestIsOrderStable(t *testing.T) {
	a := ReferenceManifest{
		SchemaVersion:   1,
		Repository:      "openai/codex",
		ReferenceCommit: strings.Repeat("a", 40),
		Files: []ReferenceFile{
			{Path: "b.rs", SHA256: strings.Repeat("b", 64)},
			{Path: "a.rs", SHA256: strings.Repeat("a", 64)},
		},
		EvidenceGaps: []string{"z", "a"},
	}
	b := a
	b.Files = []ReferenceFile{a.Files[1], a.Files[0]}
	b.EvidenceGaps = []string{"a", "z"}
	require.NoError(t, a.Validate())
	require.Equal(t, a.Digest(), b.Digest())
}

func TestDiffReferenceManifestsReportsOnlyChangedFiles(t *testing.T) {
	before := ReferenceManifest{
		SchemaVersion: 1, Repository: "openai/codex", ReferenceCommit: strings.Repeat("a", 40),
		Files: []ReferenceFile{
			{Path: "same.rs", SHA256: strings.Repeat("1", 64)},
			{Path: "changed.rs", SHA256: strings.Repeat("2", 64)},
			{Path: "removed.rs", SHA256: strings.Repeat("3", 64)},
		},
	}
	after := ReferenceManifest{
		SchemaVersion: 1, Repository: "openai/codex", ReferenceCommit: strings.Repeat("b", 40),
		Files: []ReferenceFile{
			{Path: "same.rs", SHA256: strings.Repeat("1", 64)},
			{Path: "changed.rs", SHA256: strings.Repeat("4", 64)},
			{Path: "added.rs", SHA256: strings.Repeat("5", 64)},
		},
	}
	changes, err := DiffReferenceManifests(before, after)
	require.NoError(t, err)
	require.Len(t, changes, 3)
	require.Equal(t, "added.rs", changes[0].Path)
	require.Equal(t, "changed", changes[1].Kind)
	require.Equal(t, "removed", changes[2].Kind)
}

func TestBuildReferenceManifestFromFilesIsDeterministicAndOffline(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "codex-rs", "core"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "codex-rs", "core", "a.rs"), []byte("pinned-a\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.rs"), []byte("pinned-b\n"), 0o644))
	commit := strings.Repeat("a", 40)
	manifest, err := BuildReferenceManifestFromFiles(
		root, "openai/codex", "rust-v-test", commit, "baseline",
		[]string{"b.rs", "codex-rs/core/a.rs"},
	)
	require.NoError(t, err)
	require.True(t, manifest.EvidenceComplete)
	require.Equal(t, "b.rs", manifest.Files[0].Path)
	require.Equal(t, "codex-rs/core/a.rs", manifest.Files[1].Path)
	require.Len(t, manifest.Files[0].SHA256, 64)

	_, err = BuildReferenceManifestFromFiles(
		root, "openai/codex", "rust-v-test", commit, "baseline", []string{"../escape"},
	)
	require.ErrorContains(t, err, "escapes")
}
