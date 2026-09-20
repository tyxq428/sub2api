package codexwire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type ValueKind string

const (
	KindNull   ValueKind = "null"
	KindString ValueKind = "string"
	KindNumber ValueKind = "number"
	KindBool   ValueKind = "bool"
	KindObject ValueKind = "object"
	KindArray  ValueKind = "array"
)

type TypedValue struct {
	Kind ValueKind
	Raw  json.RawMessage
}

type TurnMetadata struct {
	Fields  map[string]TypedValue
	Unknown map[string]json.RawMessage
}

var knownTurnMetadataFields = map[string]struct{}{
	"installation_id": {}, "session_id": {}, "thread_id": {}, "turn_id": {},
	"window_id": {}, "context_window_id": {}, "forked_from_thread_id": {},
	"parent_thread_id": {}, "parent_turn_id": {}, "root_turn_id": {},
	"agent_name": {}, "window_number": {}, "request_kind": {}, "compaction": {},
	"subagent_kind": {}, "thread_source": {}, "turn_trigger": {}, "sandbox": {},
	"sandbox_mode": {}, "auto_review_enabled": {}, "node_repl_auto_review_required": {},
	"node_repl_disabled": {}, "workspaces": {}, "tool_namespaces_info": {},
	"turn_started_at_unix_ms": {}, "history_ingest_requested": {}, "analytics_enabled": {},
}

// ParseTurnMetadata parses exactly one JSON object while preserving unknown
// values byte-for-byte and rejecting duplicate top-level keys.  It does not
// infer or fabricate missing client environment fields.
func ParseTurnMetadata(raw string) (TurnMetadata, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TurnMetadata{}, errors.New("turn metadata is empty")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	token, err := dec.Token()
	if err != nil {
		return TurnMetadata{}, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return TurnMetadata{}, errors.New("turn metadata must be a JSON object")
	}
	result := TurnMetadata{Fields: map[string]TypedValue{}, Unknown: map[string]json.RawMessage{}}
	seen := map[string]struct{}{}
	for dec.More() {
		nameToken, err := dec.Token()
		if err != nil {
			return TurnMetadata{}, err
		}
		name, ok := nameToken.(string)
		if !ok {
			return TurnMetadata{}, errors.New("turn metadata object key must be a string")
		}
		if _, exists := seen[name]; exists {
			return TurnMetadata{}, fmt.Errorf("duplicate turn metadata key %q", name)
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return TurnMetadata{}, err
		}
		value = append(json.RawMessage(nil), value...)
		if _, known := knownTurnMetadataFields[name]; !known {
			result.Unknown[name] = value
			continue
		}
		kind, err := classifyJSONValue(value)
		if err != nil {
			return TurnMetadata{}, fmt.Errorf("turn metadata %s: %w", name, err)
		}
		result.Fields[name] = TypedValue{Kind: kind, Raw: value}
	}
	if _, err := dec.Token(); err != nil {
		return TurnMetadata{}, err
	}
	if token, err := dec.Token(); err != io.EOF {
		if err == nil {
			return TurnMetadata{}, fmt.Errorf("unexpected trailing JSON token %v", token)
		}
		return TurnMetadata{}, err
	}
	return result, nil
}

// ParseEmbeddedTurnMetadata extracts the canonical metadata string from
// client_metadata without rebuilding the surrounding request. Unknown sibling
// fields are therefore never dropped merely because R2.2 inspects metadata.
func ParseEmbeddedTurnMetadata(body []byte) (TurnMetadata, bool, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return TurnMetadata{}, false, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return TurnMetadata{}, false, err
	}
	rawClientMetadata, ok := top["client_metadata"]
	if !ok {
		return TurnMetadata{}, false, nil
	}
	var clientMetadata map[string]json.RawMessage
	if err := json.Unmarshal(rawClientMetadata, &clientMetadata); err != nil {
		return TurnMetadata{}, false, errors.New("client_metadata must be a JSON object")
	}
	rawEmbedded, ok := clientMetadata["x-codex-turn-metadata"]
	if !ok {
		return TurnMetadata{}, false, nil
	}
	var embedded string
	if err := json.Unmarshal(rawEmbedded, &embedded); err != nil {
		return TurnMetadata{}, true, errors.New("client_metadata.x-codex-turn-metadata must be a string")
	}
	parsed, err := ParseTurnMetadata(embedded)
	return parsed, true, err
}

func classifyJSONValue(raw json.RawMessage) (ValueKind, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", errors.New("empty JSON value")
	}
	switch trimmed[0] {
	case '"':
		return KindString, nil
	case '{':
		return KindObject, nil
	case '[':
		return KindArray, nil
	case 't', 'f':
		return KindBool, nil
	case 'n':
		return KindNull, nil
	default:
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&n); err != nil {
			return "", err
		}
		return KindNumber, nil
	}
}

func (m TurnMetadata) String(name string) (string, bool) {
	value, ok := m.Fields[name]
	if !ok || value.Kind != KindString {
		return "", false
	}
	var out string
	if err := json.Unmarshal(value.Raw, &out); err != nil {
		return "", false
	}
	return out, true
}
