package codexidentity

import "strings"

const Codex0154ProfileID = "codex-0.154-profile-r1"

type SemanticRole string

const (
	RoleUnknown          SemanticRole = "unknown"
	RoleClientUserAgent  SemanticRole = "client_user_agent"
	RoleClientOriginator SemanticRole = "client_originator"
	RoleClientVersion    SemanticRole = "client_version"
	RoleInstallation     SemanticRole = "installation"
	RoleSession          SemanticRole = "session"
	RoleRoutingSession   SemanticRole = "routing_session"
	RoleThread           SemanticRole = "thread"
	RoleTurn             SemanticRole = "turn"
	RoleWindow           SemanticRole = "window"
	RoleContextWindow    SemanticRole = "context_window"
	RolePromptCache      SemanticRole = "prompt_cache"
	RoleParentThread     SemanticRole = "parent_thread"
	RoleForkedFromThread SemanticRole = "forked_from_thread"
	RoleParentTurn       SemanticRole = "parent_turn"
	RoleRootTurn         SemanticRole = "root_turn"
)

type ProtocolProfile struct {
	ID                         string
	ReferenceTag               string
	ReferenceCommit            string
	ClientVersion              string
	ClientRequestFollowsThread bool
}

func ProfileByID(id string) (ProtocolProfile, bool) {
	switch strings.TrimSpace(id) {
	case Codex0154ProfileID:
		return ProtocolProfile{
			ID:                         Codex0154ProfileID,
			ReferenceTag:               "rust-v0.154.0",
			ReferenceCommit:            "6b9826e3aa83b1a5947db50f4332cb9c65f1b340",
			ClientVersion:              "0.154.0",
			ClientRequestFollowsThread: true,
		}, true
	default:
		return ProtocolProfile{}, false
	}
}

func (p ProtocolProfile) Role(field Field) SemanticRole {
	name := strings.ToLower(strings.TrimSpace(field.Name))
	leaf := name
	if idx := strings.LastIndexByte(leaf, '.'); idx >= 0 {
		leaf = leaf[idx+1:]
	}
	switch leaf {
	case "user-agent":
		return RoleClientUserAgent
	case "originator":
		return RoleClientOriginator
	case "version":
		return RoleClientVersion
	case "installation_id", "x-codex-installation-id":
		return RoleInstallation
	case "session_id", "session-id":
		return RoleSession
	case "conversation_id", "conversation-id":
		return RoleRoutingSession
	case "thread_id", "thread-id":
		return RoleThread
	case "x-client-request-id":
		if p.ClientRequestFollowsThread {
			return RoleThread
		}
		return RoleUnknown
	case "turn_id", "turn-id":
		return RoleTurn
	case "window_id", "x-codex-window-id":
		return RoleWindow
	case "context_window_id":
		return RoleContextWindow
	case "prompt_cache_key":
		return RolePromptCache
	case "parent_thread_id", "x-codex-parent-thread-id":
		return RoleParentThread
	case "forked_from_thread_id":
		return RoleForkedFromThread
	case "parent_turn_id":
		return RoleParentTurn
	case "root_turn_id":
		return RoleRootTurn
	default:
		return RoleUnknown
	}
}

func (r SemanticRole) mappingDomain() string {
	switch r {
	case RoleInstallation:
		return "installation"
	case RoleSession:
		return "session"
	case RoleRoutingSession:
		return "routing-session"
	case RoleThread, RoleParentThread, RoleForkedFromThread:
		return "thread"
	case RoleTurn, RoleParentTurn, RoleRootTurn:
		return "turn"
	case RoleWindow:
		return "window"
	case RoleContextWindow:
		return "context-window"
	case RolePromptCache:
		return "prompt-cache"
	default:
		return ""
	}
}

func (r SemanticRole) currentEntityRole() bool {
	switch r {
	case RoleInstallation, RoleSession, RoleThread, RoleTurn, RoleWindow, RoleContextWindow:
		return true
	default:
		return false
	}
}

func isCanonicalSnapshotField(field Field) bool {
	return field.Carrier == CarrierTurnMetadata && strings.HasPrefix(field.Name, "client_metadata.x-codex-turn-metadata.")
}

func isFlatBodyField(field Field) bool {
	return field.Carrier == CarrierBody && strings.HasPrefix(field.Name, "client_metadata.") &&
		!strings.HasPrefix(field.Name, "client_metadata.x-codex-turn-metadata.")
}
