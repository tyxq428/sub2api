package codexidentity

import (
	"errors"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

var clientVersionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){2,3}(?:-[0-9A-Za-z.]+)?$`)

type ClientIdentity struct {
	UserAgent  string
	Originator string
	Version    string
	Recognized bool
	Reason     string
}

func ResolveValidatedClientIdentity(profile ProtocolProfile, userAgent, originator, version string) (ClientIdentity, error) {
	userAgent = strings.TrimSpace(userAgent)
	originator = strings.TrimSpace(originator)
	version = strings.TrimSpace(version)
	if userAgent == "" {
		return ClientIdentity{Reason: "user_agent_missing"}, nil
	}
	pairedOriginator, pairedUA, ok := openai.PairCodexClientIdentity(userAgent)
	if !ok {
		return ClientIdentity{Reason: "user_agent_unrecognized"}, nil
	}
	uaVersion := strings.TrimSpace(openai.CodexUserAgentVersion(pairedUA))
	if !clientVersionPattern.MatchString(uaVersion) {
		return ClientIdentity{Reason: "user_agent_version_invalid"}, nil
	}
	if profile.ClientVersion != "" && uaVersion != profile.ClientVersion {
		return ClientIdentity{UserAgent: pairedUA, Originator: pairedOriginator, Version: uaVersion, Reason: "unsupported_client_version"}, nil
	}
	if originator != "" && !strings.EqualFold(originator, pairedOriginator) {
		return ClientIdentity{}, errors.New("codex client originator conflicts with user-agent")
	}
	if version != "" && version != uaVersion {
		return ClientIdentity{}, errors.New("codex client version header conflicts with user-agent")
	}
	return ClientIdentity{
		UserAgent:  pairedUA,
		Originator: pairedOriginator,
		Version:    uaVersion,
		Recognized: true,
		Reason:     "validated",
	}, nil
}
