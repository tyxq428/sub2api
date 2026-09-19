package codexidentity

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Patch struct {
	Carrier Carrier `json:"carrier"`
	Path    string  `json:"path"`
	Value   string  `json:"value"`
}

type BuildOptions struct {
	RequireValidatedClient bool
}

type OutboundPlan struct {
	ProfileID   string
	Client      ClientIdentity
	Patches     []Patch
	Mapped      map[SemanticRole]string
	Diagnostics []Diagnostic
	Conflicts   []Diagnostic
}

func BuildPlan(snapshot RawSnapshot, profile ProtocolProfile, mapper *Mapper, options BuildOptions) (OutboundPlan, error) {
	if profile.ID == "" {
		return OutboundPlan{}, errors.New("codex protocol profile is required")
	}
	if mapper == nil {
		return OutboundPlan{}, errors.New("codex identity mapper is required")
	}
	graph := BuildGraph(snapshot, profile)
	plan := OutboundPlan{
		ProfileID:   profile.ID,
		Mapped:      make(map[SemanticRole]string),
		Diagnostics: append([]Diagnostic(nil), graph.Diagnostics...),
		Conflicts:   append([]Diagnostic(nil), graph.Conflicts...),
	}
	client, clientDiagnostics, clientConflicts := buildClientIdentity(snapshot, profile, options)
	plan.Client = client
	plan.Diagnostics = append(plan.Diagnostics, clientDiagnostics...)
	plan.Conflicts = append(plan.Conflicts, clientConflicts...)

	patches := make(map[string]Patch)
	for _, projection := range graph.Projections {
		domain := projection.Role.mappingDomain()
		if domain == "" || strings.TrimSpace(projection.SemanticValue) == "" {
			continue
		}
		mapped, err := mapper.Map(domain, projection.SemanticValue)
		if err != nil {
			return OutboundPlan{}, err
		}
		if _, exists := plan.Mapped[projection.Role]; !exists {
			plan.Mapped[projection.Role] = mapped
		}
		addPlanPatch(&plan, patches, Patch{Carrier: projection.Field.Carrier, Path: projection.Field.Name, Value: mapped})
	}
	if client.Recognized {
		for _, field := range snapshot.Fields() {
			switch profile.Role(field) {
			case RoleClientUserAgent:
				addPlanPatch(&plan, patches, Patch{Carrier: field.Carrier, Path: field.Name, Value: client.UserAgent})
			case RoleClientOriginator:
				addPlanPatch(&plan, patches, Patch{Carrier: field.Carrier, Path: field.Name, Value: client.Originator})
			case RoleClientVersion:
				addPlanPatch(&plan, patches, Patch{Carrier: field.Carrier, Path: field.Name, Value: client.Version})
			}
		}
	}
	plan.Patches = make([]Patch, 0, len(patches))
	for _, patch := range patches {
		plan.Patches = append(plan.Patches, patch)
	}
	sort.SliceStable(plan.Patches, func(i, j int) bool {
		if plan.Patches[i].Carrier != plan.Patches[j].Carrier {
			return plan.Patches[i].Carrier < plan.Patches[j].Carrier
		}
		return plan.Patches[i].Path < plan.Patches[j].Path
	})
	sortDiagnostics(plan.Diagnostics)
	sortDiagnostics(plan.Conflicts)
	return plan, nil
}

func (p OutboundPlan) MappedValue(role SemanticRole) string {
	return p.Mapped[role]
}

func (p OutboundPlan) Validate() error {
	if len(p.Conflicts) == 0 {
		return nil
	}
	parts := make([]string, 0, len(p.Conflicts))
	for _, conflict := range p.Conflicts {
		parts = append(parts, conflict.Code+":"+string(conflict.Role))
	}
	return fmt.Errorf("invalid codex outbound identity plan: %s", strings.Join(parts, ","))
}

func addPlanPatch(plan *OutboundPlan, patches map[string]Patch, patch Patch) {
	key := string(patch.Carrier) + "\x00" + patch.Path
	if existing, ok := patches[key]; ok {
		if existing.Value != patch.Value {
			plan.Conflicts = append(plan.Conflicts, Diagnostic{
				Code: "conflicting_patch_values", Fields: []string{patch.Path},
				Message: "one carrier path resolved to different semantic values",
			})
		}
		return
	}
	patches[key] = patch
}

func buildClientIdentity(snapshot RawSnapshot, profile ProtocolProfile, options BuildOptions) (ClientIdentity, []Diagnostic, []Diagnostic) {
	values := map[SemanticRole]map[string][]string{}
	for _, field := range snapshot.Fields() {
		role := profile.Role(field)
		if role != RoleClientUserAgent && role != RoleClientOriginator && role != RoleClientVersion {
			continue
		}
		if values[role] == nil {
			values[role] = make(map[string][]string)
		}
		value := strings.TrimSpace(field.Value)
		if value != "" {
			values[role][value] = append(values[role][value], field.Name)
		}
	}
	var conflicts []Diagnostic
	one := func(role SemanticRole) string {
		if len(values[role]) > 1 {
			fields := make([]string, 0)
			for _, names := range values[role] {
				fields = append(fields, names...)
			}
			sort.Strings(fields)
			conflicts = append(conflicts, Diagnostic{Code: "conflicting_client_identity", Role: role, Fields: fields})
			return ""
		}
		for value := range values[role] {
			return value
		}
		return ""
	}
	ua, originator, version := one(RoleClientUserAgent), one(RoleClientOriginator), one(RoleClientVersion)
	if len(conflicts) != 0 {
		return ClientIdentity{}, nil, conflicts
	}
	client, err := ResolveValidatedClientIdentity(profile, ua, originator, version)
	if err != nil {
		return ClientIdentity{}, nil, []Diagnostic{{Code: "client_identity_conflict", Role: RoleClientUserAgent, Message: err.Error()}}
	}
	if !client.Recognized {
		diagnostic := Diagnostic{Code: "client_identity_unvalidated", Role: RoleClientUserAgent, Message: client.Reason}
		if options.RequireValidatedClient {
			return client, nil, []Diagnostic{diagnostic}
		}
		return client, []Diagnostic{diagnostic}, nil
	}
	return client, nil, nil
}
