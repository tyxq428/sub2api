package codexwire

import (
	"fmt"
	"sort"
	"strings"
)

type WireField struct {
	Path   string
	Domain string
	Value  string
	Opaque bool
}

type Mismatch struct {
	Path     string `json:"path"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// NormalizeRelations replaces identity values with deterministic per-domain
// symbols. Equality and inequality relationships are retained, unlike a
// blanket "<ID>" redaction that can hide accidental merges or splits.
func NormalizeRelations(fields []WireField) []WireField {
	next := make([]WireField, len(fields))
	copy(next, fields)
	seen := map[string]map[string]string{}
	counts := map[string]int{}
	for i := range next {
		if next[i].Opaque || strings.TrimSpace(next[i].Domain) == "" || strings.TrimSpace(next[i].Value) == "" {
			continue
		}
		domain := next[i].Domain
		if seen[domain] == nil {
			seen[domain] = map[string]string{}
		}
		symbol, ok := seen[domain][next[i].Value]
		if !ok {
			counts[domain]++
			symbol = fmt.Sprintf("<%s:%d>", domain, counts[domain])
			seen[domain][next[i].Value] = symbol
		}
		next[i].Value = symbol
	}
	sort.SliceStable(next, func(i, j int) bool { return next[i].Path < next[j].Path })
	return next
}

func Compare(expected, actual []WireField) []Mismatch {
	expected = NormalizeRelations(expected)
	actual = NormalizeRelations(actual)
	em := map[string]string{}
	am := map[string]string{}
	paths := map[string]struct{}{}
	for _, field := range expected {
		em[field.Path] = field.Value
		paths[field.Path] = struct{}{}
	}
	for _, field := range actual {
		am[field.Path] = field.Value
		paths[field.Path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var out []Mismatch
	for _, path := range ordered {
		if em[path] != am[path] {
			out = append(out, Mismatch{Path: path, Expected: em[path], Actual: am[path]})
		}
	}
	return out
}
