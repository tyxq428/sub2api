package codexwire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ReferenceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes,omitempty"`
}

type ReferenceManifest struct {
	SchemaVersion    int             `json:"schema_version"`
	Repository       string          `json:"repository"`
	ReferenceTag     string          `json:"reference_tag"`
	ReferenceCommit  string          `json:"reference_commit"`
	BaselineCommit   string          `json:"baseline_commit,omitempty"`
	Files            []ReferenceFile `json:"files"`
	EvidenceComplete bool            `json:"evidence_complete"`
	EvidenceGaps     []string        `json:"evidence_gaps,omitempty"`
}

type ManifestChange struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

func (m ReferenceManifest) Validate() error {
	if m.SchemaVersion != 1 {
		return fmt.Errorf("unsupported reference manifest schema %d", m.SchemaVersion)
	}
	if strings.TrimSpace(m.Repository) == "" || strings.TrimSpace(m.ReferenceCommit) == "" {
		return fmt.Errorf("reference repository and commit are required")
	}
	if !codexCommitPattern.MatchString(strings.TrimSpace(m.ReferenceCommit)) {
		return fmt.Errorf("reference commit must be a 40-character lowercase hex commit")
	}
	seen := map[string]struct{}{}
	for _, file := range m.Files {
		path := strings.TrimSpace(file.Path)
		if path == "" {
			return fmt.Errorf("reference file path is required")
		}
		if _, ok := seen[path]; ok {
			return fmt.Errorf("duplicate reference file %q", path)
		}
		seen[path] = struct{}{}
		if !codexSHA256Pattern.MatchString(strings.TrimSpace(file.SHA256)) {
			return fmt.Errorf("reference file %q sha256 must be lowercase hex", path)
		}
	}
	return nil
}

func (m ReferenceManifest) Digest() string {
	copyManifest := m
	copyManifest.Files = append([]ReferenceFile(nil), m.Files...)
	copyManifest.EvidenceGaps = append([]string(nil), m.EvidenceGaps...)
	sort.Slice(copyManifest.Files, func(i, j int) bool { return copyManifest.Files[i].Path < copyManifest.Files[j].Path })
	sort.Strings(copyManifest.EvidenceGaps)
	encoded, _ := json.Marshal(copyManifest)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func DiffReferenceManifests(before, after ReferenceManifest) ([]ManifestChange, error) {
	if err := before.Validate(); err != nil {
		return nil, fmt.Errorf("before manifest: %w", err)
	}
	if err := after.Validate(); err != nil {
		return nil, fmt.Errorf("after manifest: %w", err)
	}
	bm := map[string]string{}
	am := map[string]string{}
	paths := map[string]struct{}{}
	for _, file := range before.Files {
		bm[file.Path] = file.SHA256
		paths[file.Path] = struct{}{}
	}
	for _, file := range after.Files {
		am[file.Path] = file.SHA256
		paths[file.Path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var changes []ManifestChange
	for _, path := range ordered {
		beforeSHA, beforeOK := bm[path]
		afterSHA, afterOK := am[path]
		switch {
		case !beforeOK:
			changes = append(changes, ManifestChange{Path: path, Kind: "added", After: afterSHA})
		case !afterOK:
			changes = append(changes, ManifestChange{Path: path, Kind: "removed", Before: beforeSHA})
		case beforeSHA != afterSHA:
			changes = append(changes, ManifestChange{Path: path, Kind: "changed", Before: beforeSHA, After: afterSHA})
		}
	}
	return changes, nil
}

// BuildReferenceManifestFromFiles hashes an already-pinned local checkout.
// It intentionally performs no network I/O and therefore cannot silently
// replace an unavailable pinned revision with moving main.
func BuildReferenceManifestFromFiles(
	root, repository, tag, commit, baseline string,
	paths []string,
) (ReferenceManifest, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return ReferenceManifest{}, fmt.Errorf("reference checkout root is required")
	}
	manifest := ReferenceManifest{
		SchemaVersion:    1,
		Repository:       strings.TrimSpace(repository),
		ReferenceTag:     strings.TrimSpace(tag),
		ReferenceCommit:  strings.TrimSpace(commit),
		BaselineCommit:   strings.TrimSpace(baseline),
		EvidenceComplete: true,
	}
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			return ReferenceManifest{}, fmt.Errorf("duplicate reference file %q", path)
		}
		seen[path] = struct{}{}
		clean := filepath.Clean(filepath.FromSlash(path))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return ReferenceManifest{}, fmt.Errorf("reference file escapes checkout root: %q", path)
		}
		data, err := os.ReadFile(filepath.Join(root, clean))
		if err != nil {
			return ReferenceManifest{}, fmt.Errorf("read reference file %q: %w", path, err)
		}
		digest := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, ReferenceFile{
			Path: path, SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data)),
		})
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	if err := manifest.Validate(); err != nil {
		return ReferenceManifest{}, err
	}
	return manifest, nil
}
