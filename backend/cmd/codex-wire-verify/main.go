package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codexwire"
)

type stringListFlag []string

func (s *stringListFlag) String() string { return fmt.Sprint([]string(*s)) }
func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	mode := flag.String("mode", "contracts", "contracts|diff-fields|diff-manifest|build-manifest")
	beforePath := flag.String("before", "", "path to expected/before JSON")
	afterPath := flag.String("after", "", "path to actual/after JSON")
	root := flag.String("root", "", "pinned local checkout root for build-manifest")
	repository := flag.String("repository", "openai/codex", "reference repository")
	tag := flag.String("tag", "", "pinned reference tag")
	commit := flag.String("commit", "", "pinned reference commit")
	baseline := flag.String("baseline", "", "implementation baseline commit")
	var files stringListFlag
	flag.Var(&files, "file", "relative reference file path; repeat for multiple files")
	flag.Parse()
	if err := run(*mode, *beforePath, *afterPath, *root, *repository, *tag, *commit, *baseline, files); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(mode, beforePath, afterPath, root, repository, tag, commit, baseline string, files []string) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	switch mode {
	case "contracts":
		return encoder.Encode(codexwire.KnownContracts())
	case "diff-fields":
		if beforePath == "" || afterPath == "" {
			return errors.New("diff-fields requires -before and -after")
		}
		var expected, actual []codexwire.WireField
		if err := readJSON(beforePath, &expected); err != nil {
			return err
		}
		if err := readJSON(afterPath, &actual); err != nil {
			return err
		}
		return encoder.Encode(codexwire.Compare(expected, actual))
	case "diff-manifest":
		if beforePath == "" || afterPath == "" {
			return errors.New("diff-manifest requires -before and -after")
		}
		var before, after codexwire.ReferenceManifest
		if err := readJSON(beforePath, &before); err != nil {
			return err
		}
		if err := readJSON(afterPath, &after); err != nil {
			return err
		}
		changes, err := codexwire.DiffReferenceManifests(before, after)
		if err != nil {
			return err
		}
		return encoder.Encode(struct {
			BeforeDigest string                     `json:"before_digest"`
			AfterDigest  string                     `json:"after_digest"`
			Changes      []codexwire.ManifestChange `json:"changes"`
		}{
			BeforeDigest: before.Digest(),
			AfterDigest:  after.Digest(),
			Changes:      changes,
		})
	case "build-manifest":
		if root == "" || commit == "" || len(files) == 0 {
			return errors.New("build-manifest requires -root, -commit and at least one -file")
		}
		manifest, err := codexwire.BuildReferenceManifestFromFiles(
			root, repository, tag, commit, baseline, files,
		)
		if err != nil {
			return err
		}
		return encoder.Encode(manifest)
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
