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

// CompatibilityFallbackEligible reports whether a compatibility selector
// failed only because an otherwise well-formed official Codex client version
// is not pinned by that selector. Missing/ambiguous user agents and conflicting
// Originator/Version headers remain fail-closed.
func CompatibilityFallbackEligible(referenceProfile string, snapshot RawSnapshot) bool {
	if strings.TrimSpace(referenceProfile) != Codex0154To0155CompatibilityID {
		return false
	}

	userAgents := make(map[string]struct{})
	originators := make(map[string]struct{})
	versions := make(map[string]struct{})
	for _, field := range snapshot.Fields() {
		if field.Carrier != CarrierHeader {
			continue
		}
		value := strings.TrimSpace(field.Value)
		if value == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(field.Name)) {
		case "user-agent":
			userAgents[value] = struct{}{}
		case "originator":
			originators[value] = struct{}{}
		case "version":
			versions[value] = struct{}{}
		}
	}
	if len(userAgents) != 1 || len(originators) > 1 || len(versions) > 1 {
		return false
	}

	var userAgent string
	for value := range userAgents {
		userAgent = value
	}
	pairedOriginator, pairedUA, ok := openai.PairCodexClientIdentity(userAgent)
	if !ok {
		return false
	}
	uaVersion := strings.TrimSpace(openai.CodexUserAgentVersion(pairedUA))
	if !clientVersionPattern.MatchString(uaVersion) {
		return false
	}

	// A pinned version that failed resolution implies some conflicting identity
	// signal; it must not be converted into a compatibility fallback.
	for _, profileID := range []string{Codex0154ProfileID, Codex0155ProfileID} {
		profile, ok := ProfileByID(profileID)
		if ok && uaVersion == profile.ClientVersion {
			return false
		}
	}

	for originator := range originators {
		if !strings.EqualFold(originator, pairedOriginator) {
			return false
		}
	}
	for version := range versions {
		if version != uaVersion {
			return false
		}
	}
	return true
}
