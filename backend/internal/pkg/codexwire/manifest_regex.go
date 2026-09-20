package codexwire

import "regexp"

var (
	codexCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	codexSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)
