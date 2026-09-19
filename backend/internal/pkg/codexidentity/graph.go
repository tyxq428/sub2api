package codexidentity

import (
	"fmt"
	"sort"
	"strings"
)

type Diagnostic struct {
	Code    string       `json:"code"`
	Role    SemanticRole `json:"role,omitempty"`
	Fields  []string     `json:"fields,omitempty"`
	Message string       `json:"message,omitempty"`
}

type Projection struct {
	Field         Field
	Role          SemanticRole
	SemanticValue string
}

type SemanticGraph struct {
	ProfileID   string
	Projections []Projection
	Diagnostics []Diagnostic
	Conflicts   []Diagnostic
	current     map[SemanticRole]string
}

func BuildGraph(snapshot RawSnapshot, profile ProtocolProfile) SemanticGraph {
	graph := SemanticGraph{ProfileID: profile.ID, current: make(map[SemanticRole]string)}
	fields := snapshot.Fields()
	byRole := make(map[SemanticRole][]Field)
	for _, field := range fields {
		role := profile.Role(field)
		if role != RoleUnknown {
			byRole[role] = append(byRole[role], field)
		}
	}
	for role, roleFields := range byRole {
		if !role.currentEntityRole() {
			continue
		}
		value, diagnostics, conflicts := selectAuthoritativeValue(role, roleFields)
		graph.Diagnostics = append(graph.Diagnostics, diagnostics...)
		graph.Conflicts = append(graph.Conflicts, conflicts...)
		if value != "" {
			graph.current[role] = value
		}
	}
	for _, field := range fields {
		role := profile.Role(field)
		if role == RoleUnknown || role == RoleClientUserAgent || role == RoleClientOriginator || role == RoleClientVersion {
			continue
		}
		semanticValue := strings.TrimSpace(field.Value)
		if role.currentEntityRole() && graph.current[role] != "" {
			semanticValue = graph.current[role]
		}
		if role == RolePromptCache && graph.current[RoleSession] != "" && semanticValue == graph.current[RoleSession] {
			role = RoleSession
		}
		graph.Projections = append(graph.Projections, Projection{Field: field, Role: role, SemanticValue: semanticValue})
	}
	sort.SliceStable(graph.Projections, func(i, j int) bool {
		if graph.Projections[i].Field.Carrier != graph.Projections[j].Field.Carrier {
			return graph.Projections[i].Field.Carrier < graph.Projections[j].Field.Carrier
		}
		return graph.Projections[i].Field.Name < graph.Projections[j].Field.Name
	})
	sortDiagnostics(graph.Diagnostics)
	sortDiagnostics(graph.Conflicts)
	return graph
}

func (g SemanticGraph) Current(role SemanticRole) string {
	return g.current[role]
}

func (g SemanticGraph) Validate() error {
	if len(g.Conflicts) == 0 {
		return nil
	}
	parts := make([]string, 0, len(g.Conflicts))
	for _, conflict := range g.Conflicts {
		parts = append(parts, string(conflict.Role)+":"+conflict.Code)
	}
	return fmt.Errorf("codex semantic identity conflicts: %s", strings.Join(parts, ","))
}

func selectAuthoritativeValue(role SemanticRole, fields []Field) (string, []Diagnostic, []Diagnostic) {
	type rankedValue struct {
		rank   int
		value  string
		fields []string
	}
	byRank := make(map[int]map[string][]string)
	for _, field := range fields {
		value := strings.TrimSpace(field.Value)
		if value == "" {
			continue
		}
		rank := fieldAuthorityRank(field)
		if byRank[rank] == nil {
			byRank[rank] = make(map[string][]string)
		}
		byRank[rank][value] = append(byRank[rank][value], field.Name)
	}
	ranks := make([]int, 0, len(byRank))
	for rank := range byRank {
		ranks = append(ranks, rank)
	}
	sort.Ints(ranks)
	var selected rankedValue
	var diagnostics []Diagnostic
	var conflicts []Diagnostic
	for _, rank := range ranks {
		values := byRank[rank]
		if len(values) > 1 {
			fieldNames := make([]string, 0)
			for _, names := range values {
				fieldNames = append(fieldNames, names...)
			}
			sort.Strings(fieldNames)
			conflicts = append(conflicts, Diagnostic{
				Code: "conflicting_authoritative_values", Role: role, Fields: fieldNames,
				Message: "same-precedence identity projections disagree",
			})
			continue
		}
		for value, names := range values {
			if selected.value == "" {
				selected = rankedValue{rank: rank, value: value, fields: append([]string(nil), names...)}
				continue
			}
			if value != selected.value {
				fieldNames := append([]string(nil), names...)
				sort.Strings(fieldNames)
				diagnostics = append(diagnostics, Diagnostic{
					Code: "stale_projection", Role: role, Fields: fieldNames,
					Message: "lower-precedence projection will follow the canonical semantic value",
				})
			}
		}
	}
	return selected.value, diagnostics, conflicts
}

func fieldAuthorityRank(field Field) int {
	switch {
	case isCanonicalSnapshotField(field):
		return 0
	case isFlatBodyField(field):
		return 1
	case field.Carrier == CarrierTurnMetadata:
		return 2
	case field.Carrier == CarrierHeader:
		return 3
	default:
		return 4
	}
}

func sortDiagnostics(items []Diagnostic) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Code != items[j].Code {
			return items[i].Code < items[j].Code
		}
		if items[i].Role != items[j].Role {
			return items[i].Role < items[j].Role
		}
		return strings.Join(items[i].Fields, "\x00") < strings.Join(items[j].Fields, "\x00")
	})
}
