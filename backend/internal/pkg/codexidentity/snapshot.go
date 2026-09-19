package codexidentity

import (
	"bytes"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// Mode controls whether R2 is inactive, computes a local proposal only, or is
// permitted to project a previously admitted policy. Enforce is deliberately a
// distinct value; callers must not infer it from Shadow or any R1 setting.
type Mode string

const (
	ModeOff     Mode = "off"
	ModeShadow  Mode = "shadow"
	ModeEnforce Mode = "enforce"
)

func ParseMode(raw string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case ModeShadow:
		return ModeShadow
	case ModeEnforce:
		return ModeEnforce
	default:
		return ModeOff
	}
}

type Stage string

const (
	StageInput    Stage = "input"
	StageActual   Stage = "actual"
	StageProposed Stage = "proposed_v2"
)

type Purpose string

const (
	PurposeInference  Purpose = "inference"
	PurposeCompact    Purpose = "compact"
	PurposeWebSocket  Purpose = "websocket"
	PurposeModels     Purpose = "models"
	PurposeAuth       Purpose = "auth"
	PurposeBackground Purpose = "background"
	PurposeUnknown    Purpose = "unknown"
)

type Carrier string

const (
	CarrierHeader       Carrier = "header"
	CarrierBody         Carrier = "body"
	CarrierTurnMetadata Carrier = "turn_metadata"
)

type IdentityClass string

const (
	ClassClient       IdentityClass = "client"
	ClassInstallation IdentityClass = "installation"
	ClassSession      IdentityClass = "session"
	ClassThread       IdentityClass = "thread"
	ClassTurn         IdentityClass = "turn"
	ClassWindow       IdentityClass = "window"
	ClassCache        IdentityClass = "cache"
	ClassRouting      IdentityClass = "routing"
	ClassOpaque       IdentityClass = "opaque"
)

type Limits struct {
	MaxMetadataBytes       int
	MaxIdentityHeaderBytes int
	MaxMetadataDepth       int
	MaxIdentityValueBytes  int
	MaxUserAgentBytes      int
}

func DefaultLimits() Limits {
	return Limits{
		MaxMetadataBytes:       256 * 1024,
		MaxIdentityHeaderBytes: 16 * 1024,
		MaxMetadataDepth:       32,
		MaxIdentityValueBytes:  1024,
		MaxUserAgentBytes:      1024,
	}
}

func normalizeLimits(in Limits) Limits {
	d := DefaultLimits()
	if in.MaxMetadataBytes > 0 {
		d.MaxMetadataBytes = in.MaxMetadataBytes
	}
	if in.MaxIdentityHeaderBytes > 0 {
		d.MaxIdentityHeaderBytes = in.MaxIdentityHeaderBytes
	}
	if in.MaxMetadataDepth > 0 {
		d.MaxMetadataDepth = in.MaxMetadataDepth
	}
	if in.MaxIdentityValueBytes > 0 {
		d.MaxIdentityValueBytes = in.MaxIdentityValueBytes
	}
	if in.MaxUserAgentBytes > 0 {
		d.MaxUserAgentBytes = in.MaxUserAgentBytes
	}
	return d
}

// Field is request-lifetime raw identity data. It must never be sent to a
// telemetry sink directly; Observer converts it to keyed digests first.
type Field struct {
	Carrier Carrier
	Name    string
	Class   IdentityClass
	Value   string
}

type Coverage struct {
	Complete         bool
	Reasons          []string
	HeaderFieldCount int
	BodyFieldCount   int
}

// RawSnapshot contains only the bounded identity/control subset required by
// R2. It owns its slices and returns copies so shadow evaluation cannot mutate
// the request or another observer view.
type RawSnapshot struct {
	fields   []Field
	coverage Coverage
}

func (s RawSnapshot) Fields() []Field {
	return append([]Field(nil), s.fields...)
}

func (s RawSnapshot) Coverage() Coverage {
	coverage := s.coverage
	coverage.Reasons = append([]string(nil), coverage.Reasons...)
	return coverage
}

func (s RawSnapshot) Clone() RawSnapshot {
	return RawSnapshot{fields: s.Fields(), coverage: s.Coverage()}
}

var identityHeaderNames = []string{
	"conversation-id",
	"conversation_id",
	"originator",
	"session-id",
	"session_id",
	"thread-id",
	"turn-id",
	"user-agent",
	"version",
	"x-client-request-id",
	"x-codex-beta-features",
	"x-codex-installation-id",
	"x-codex-parent-thread-id",
	"x-codex-turn-metadata",
	"x-codex-window-id",
	"x-openai-subagent",
}

type bodyFieldSpec struct {
	path  string
	name  string
	class IdentityClass
}

var bodyFieldSpecs = []bodyFieldSpec{
	{path: "prompt_cache_key", name: "prompt_cache_key", class: ClassCache},
	{path: "client_metadata.installation_id", name: "client_metadata.installation_id", class: ClassInstallation},
	{path: "client_metadata.x-codex-installation-id", name: "client_metadata.x-codex-installation-id", class: ClassInstallation},
	{path: "client_metadata.session_id", name: "client_metadata.session_id", class: ClassSession},
	{path: "client_metadata.session-id", name: "client_metadata.session-id", class: ClassSession},
	{path: "client_metadata.thread_id", name: "client_metadata.thread_id", class: ClassThread},
	{path: "client_metadata.thread-id", name: "client_metadata.thread-id", class: ClassThread},
	{path: "client_metadata.turn_id", name: "client_metadata.turn_id", class: ClassTurn},
	{path: "client_metadata.turn-id", name: "client_metadata.turn-id", class: ClassTurn},
	{path: "client_metadata.window_id", name: "client_metadata.window_id", class: ClassWindow},
	{path: "client_metadata.x-codex-window-id", name: "client_metadata.x-codex-window-id", class: ClassWindow},
	{path: "client_metadata.x-client-request-id", name: "client_metadata.x-client-request-id", class: ClassThread},
	{path: "client_metadata.x-codex-parent-thread-id", name: "client_metadata.x-codex-parent-thread-id", class: ClassThread},
	{path: "client_metadata.x-openai-subagent", name: "client_metadata.x-openai-subagent", class: ClassRouting},
}

var turnMetadataFields = []bodyFieldSpec{
	{path: "installation_id", name: "installation_id", class: ClassInstallation},
	{path: "session_id", name: "session_id", class: ClassSession},
	{path: "thread_id", name: "thread_id", class: ClassThread},
	{path: "turn_id", name: "turn_id", class: ClassTurn},
	{path: "window_id", name: "window_id", class: ClassWindow},
	{path: "context_window_id", name: "context_window_id", class: ClassWindow},
	{path: "forked_from_thread_id", name: "forked_from_thread_id", class: ClassThread},
	{path: "parent_thread_id", name: "parent_thread_id", class: ClassThread},
	{path: "parent_turn_id", name: "parent_turn_id", class: ClassTurn},
	{path: "root_turn_id", name: "root_turn_id", class: ClassTurn},
	{path: "request_kind", name: "request_kind", class: ClassRouting},
	{path: "subagent_kind", name: "subagent_kind", class: ClassRouting},
	{path: "thread_source", name: "thread_source", class: ClassRouting},
	{path: "turn_trigger", name: "turn_trigger", class: ClassRouting},
}

// Capture reads only bounded identity carriers. Oversized, malformed or
// unsupported carriers reduce coverage but never modify or reject the request.
func Capture(headers http.Header, body []byte, limits Limits) RawSnapshot {
	limits = normalizeLimits(limits)
	collector := snapshotCollector{limits: limits, coverage: Coverage{Complete: true}}
	collector.captureHeaders(headers)
	collector.captureBody(body)
	collector.finish()
	return RawSnapshot{fields: collector.fields, coverage: collector.coverage}
}

type snapshotCollector struct {
	limits      Limits
	fields      []Field
	coverage    Coverage
	reasons     map[string]struct{}
	headerBytes int
}

func (c *snapshotCollector) addReason(reason string) {
	if reason == "" {
		return
	}
	if c.reasons == nil {
		c.reasons = make(map[string]struct{})
	}
	c.reasons[reason] = struct{}{}
	c.coverage.Complete = false
}

func (c *snapshotCollector) addField(field Field, isHeader bool) {
	valueLimit := c.limits.MaxIdentityValueBytes
	if field.Name == "user-agent" {
		valueLimit = c.limits.MaxUserAgentBytes
	}
	if len(field.Value) > valueLimit {
		c.addReason("identity_value_too_large")
		return
	}
	if isHeader {
		next := c.headerBytes + len(field.Name) + len(field.Value)
		if next > c.limits.MaxIdentityHeaderBytes {
			c.addReason("identity_headers_too_large")
			return
		}
		c.headerBytes = next
		c.coverage.HeaderFieldCount++
	} else {
		c.coverage.BodyFieldCount++
	}
	c.fields = append(c.fields, field)
}

func (c *snapshotCollector) captureHeaders(headers http.Header) {
	if headers == nil {
		return
	}
	for _, name := range identityHeaderNames {
		for _, value := range headers.Values(name) {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			c.addField(Field{Carrier: CarrierHeader, Name: name, Class: classifyField(name), Value: value}, true)
			if name == "x-codex-turn-metadata" {
				c.captureTurnMetadata(value, "header.x-codex-turn-metadata")
			}
		}
	}
}

func (c *snapshotCollector) captureBody(body []byte) {
	if len(body) == 0 {
		return
	}
	if !gjson.ValidBytes(body) {
		c.addReason("body_invalid_json")
		return
	}
	c.captureBodyField(body, bodyFieldSpec{path: "prompt_cache_key", name: "prompt_cache_key", class: ClassCache})
	clientMetadata := gjson.GetBytes(body, "client_metadata")
	if !clientMetadata.Exists() {
		return
	}
	if !clientMetadata.IsObject() {
		c.addReason("client_metadata_non_object")
		return
	}
	if len(clientMetadata.Raw) > c.limits.MaxMetadataBytes {
		c.addReason("client_metadata_too_large")
		return
	}
	if !jsonDepthWithin([]byte(clientMetadata.Raw), c.limits.MaxMetadataDepth) {
		c.addReason("client_metadata_too_deep")
		return
	}
	for _, spec := range bodyFieldSpecs {
		if spec.path == "prompt_cache_key" {
			continue
		}
		c.captureBodyField(body, spec)
	}
	embedded := gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata")
	if embedded.Exists() {
		if embedded.Type != gjson.String {
			c.addReason("turn_metadata_non_string")
		} else {
			c.captureTurnMetadata(embedded.String(), "client_metadata.x-codex-turn-metadata")
		}
	}
}

func (c *snapshotCollector) captureBodyField(body []byte, spec bodyFieldSpec) {
	result := gjson.GetBytes(body, spec.path)
	if !result.Exists() {
		return
	}
	if result.Type != gjson.String {
		c.addReason("identity_field_non_string")
		return
	}
	value := strings.TrimSpace(result.String())
	if value != "" {
		c.addField(Field{Carrier: CarrierBody, Name: spec.name, Class: spec.class, Value: value}, false)
	}
}

func (c *snapshotCollector) captureTurnMetadata(raw, prefix string) {
	if len(raw) > c.limits.MaxMetadataBytes {
		c.addReason("turn_metadata_too_large")
		return
	}
	data := []byte(raw)
	if !gjson.ValidBytes(data) || !jsonDepthWithin(data, c.limits.MaxMetadataDepth) {
		c.addReason("turn_metadata_invalid")
		return
	}
	for _, spec := range turnMetadataFields {
		result := gjson.GetBytes(data, spec.path)
		if !result.Exists() {
			continue
		}
		value := ""
		switch result.Type {
		case gjson.String:
			value = strings.TrimSpace(result.String())
		case gjson.Number:
			value = strconv.FormatInt(result.Int(), 10)
		case gjson.True, gjson.False:
			value = strconv.FormatBool(result.Bool())
		default:
			c.addReason("turn_metadata_field_unsupported")
		}
		if value != "" {
			c.addField(Field{Carrier: CarrierTurnMetadata, Name: prefix + "." + spec.name, Class: spec.class, Value: value}, false)
		}
	}
}

func (c *snapshotCollector) finish() {
	sort.SliceStable(c.fields, func(i, j int) bool {
		if c.fields[i].Carrier != c.fields[j].Carrier {
			return c.fields[i].Carrier < c.fields[j].Carrier
		}
		if c.fields[i].Name != c.fields[j].Name {
			return c.fields[i].Name < c.fields[j].Name
		}
		return c.fields[i].Value < c.fields[j].Value
	})
	if len(c.reasons) != 0 {
		c.coverage.Reasons = make([]string, 0, len(c.reasons))
		for reason := range c.reasons {
			c.coverage.Reasons = append(c.coverage.Reasons, reason)
		}
		sort.Strings(c.coverage.Reasons)
	}
}

func classifyField(name string) IdentityClass {
	name = strings.ToLower(name)
	switch {
	case strings.Contains(name, "installation"):
		return ClassInstallation
	case name == "prompt_cache_key":
		return ClassCache
	case strings.Contains(name, "session") || strings.Contains(name, "conversation"):
		return ClassSession
	case strings.Contains(name, "thread") || name == "x-client-request-id":
		return ClassThread
	case strings.Contains(name, "turn"):
		return ClassTurn
	case strings.Contains(name, "window"):
		return ClassWindow
	case name == "user-agent" || name == "originator" || name == "version":
		return ClassClient
	case strings.Contains(name, "subagent") || strings.Contains(name, "request_kind") || strings.Contains(name, "source") || strings.Contains(name, "trigger"):
		return ClassRouting
	default:
		return ClassOpaque
	}
}

func jsonDepthWithin(data []byte, limit int) bool {
	if limit <= 0 {
		return false
	}
	depth := 0
	inString := false
	escaped := false
	for _, b := range bytes.TrimSpace(data) {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
				continue
			}
			if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > limit {
				return false
			}
		case '}', ']':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return !inString && depth == 0
}
