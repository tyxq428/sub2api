package codexidentity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type clientFixture struct {
	Name           string `json:"name"`
	UserAgent      string `json:"user_agent"`
	InstallationID string `json:"installation_id"`
}

func testProfile(t *testing.T) ProtocolProfile {
	t.Helper()
	profile, ok := ProfileByID(Codex0154ProfileID)
	require.True(t, ok)
	return profile
}

func testMapper(t *testing.T, authScope, credentialScope string) *Mapper {
	t.Helper()
	mapper, err := NewMapper(MappingScope{
		Secret:          []byte("01234567890123456789012345678901"),
		AuthScope:       authScope,
		CredentialScope: credentialScope,
		Algorithm:       "hmac-sha256-v1",
		KeyEpoch:        "epoch-test",
	})
	require.NoError(t, err)
	return mapper
}

func planPatchValue(t *testing.T, plan OutboundPlan, carrier Carrier, path string) string {
	t.Helper()
	for _, patch := range plan.Patches {
		if patch.Carrier == carrier && patch.Path == path {
			return patch.Value
		}
	}
	t.Fatalf("missing patch %s:%s", carrier, path)
	return ""
}

func semanticFixture() (http.Header, []byte) {
	canonicalMetadata := `{"installation_id":"11111111-1111-4111-8111-111111111111","session_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","thread_id":"0199a1b2-c3d4-7e5f-8a9b-111111111111","turn_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222","parent_thread_id":"0199a1b2-c3d4-7e5f-8a9b-333333333333","root_turn_id":"0199a1b2-c3d4-7e5f-8a9b-444444444444","request_kind":"turn"}`
	headers := http.Header{
		"User-Agent":              {"codex-tui/0.154.0 (Windows 10.0.26200; x86_64) WindowsTerminal"},
		"Originator":              {"codex-tui"},
		"Version":                 {"0.154.0"},
		"X-Codex-Installation-Id": {"old-installation-projection"},
		"Session-Id":              {"old-session-projection"},
		"Thread-Id":               {"old-thread-projection"},
		"X-Client-Request-Id":     {"old-thread-projection"},
		"X-Codex-Turn-Metadata":   {canonicalMetadata},
	}
	embedded, _ := json.Marshal(canonicalMetadata)
	body := []byte(fmt.Sprintf(`{"prompt_cache_key":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","input":"literal thread_id and session_id must stay untouched","encrypted_content":"opaque-0199a1b2-c3d4-7e5f-8a9b-111111111111","client_metadata":{"installation_id":"old-installation-flat","session_id":"old-session-flat","thread_id":"old-thread-flat","x-client-request-id":"old-thread-flat","x-codex-turn-metadata":%s}}`, embedded))
	return headers, body
}

func TestBuildPlanCouplesCanonicalSemanticIdentityAcrossCarriers(t *testing.T) {
	headers, body := semanticFixture()
	snapshot := Capture(headers, body, DefaultLimits())
	plan, err := BuildPlan(snapshot, testProfile(t), testMapper(t, "tenant-a", "oauth-account-a"), BuildOptions{RequireValidatedClient: true})
	require.NoError(t, err)
	require.NoError(t, plan.Validate())
	require.True(t, plan.Client.Recognized)
	require.Equal(t, headers.Get("User-Agent"), plan.Client.UserAgent)

	session := planPatchValue(t, plan, CarrierTurnMetadata, "client_metadata.x-codex-turn-metadata.session_id")
	require.Equal(t, session, planPatchValue(t, plan, CarrierHeader, "session-id"))
	require.Equal(t, session, planPatchValue(t, plan, CarrierBody, "client_metadata.session_id"))
	require.Equal(t, session, planPatchValue(t, plan, CarrierBody, "prompt_cache_key"))

	thread := planPatchValue(t, plan, CarrierTurnMetadata, "client_metadata.x-codex-turn-metadata.thread_id")
	require.Equal(t, thread, planPatchValue(t, plan, CarrierHeader, "thread-id"))
	require.Equal(t, thread, planPatchValue(t, plan, CarrierHeader, "x-client-request-id"))
	require.Equal(t, thread, planPatchValue(t, plan, CarrierBody, "client_metadata.thread_id"))
	require.Equal(t, thread, planPatchValue(t, plan, CarrierBody, "client_metadata.x-client-request-id"))

	parentBody := planPatchValue(t, plan, CarrierTurnMetadata, "client_metadata.x-codex-turn-metadata.parent_thread_id")
	parentHeader := planPatchValue(t, plan, CarrierTurnMetadata, "header.x-codex-turn-metadata.parent_thread_id")
	require.Equal(t, parentBody, parentHeader)
	require.NotEqual(t, thread, parentBody)

	nextHeaders, nextBody, err := ProjectHTTP(headers, body, plan)
	require.NoError(t, err)
	require.Equal(t, headers.Get("User-Agent"), nextHeaders.Get("User-Agent"))
	require.Equal(t, thread, nextHeaders.Get("Thread-Id"))
	require.Equal(t, thread, nextHeaders.Get("X-Client-Request-Id"))
	require.Equal(t, session, gjson.GetBytes(nextBody, "prompt_cache_key").String())
	require.Equal(t, thread, gjson.GetBytes(nextBody, "client_metadata.thread_id").String())
	require.Equal(t, thread, gjson.Get(gjson.GetBytes(nextBody, "client_metadata.x-codex-turn-metadata").String(), "thread_id").String())
	require.Contains(t, string(nextBody), "literal thread_id and session_id must stay untouched")
	require.Contains(t, string(nextBody), "opaque-0199a1b2-c3d4-7e5f-8a9b-111111111111")
	require.Equal(t, "old-thread-projection", headers.Get("Thread-Id"), "projection must not mutate original headers")
	require.Contains(t, string(body), "old-thread-flat", "projection must not mutate original body")
}

func TestExplicitPromptCacheKeyStaysIndependent(t *testing.T) {
	headers := http.Header{
		"User-Agent": {"codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm"},
		"Session-Id": {"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	}
	body := []byte(`{"prompt_cache_key":"explicit-cache-key","client_metadata":{"session_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}}`)
	plan, err := BuildPlan(Capture(headers, body, DefaultLimits()), testProfile(t), testMapper(t, "tenant-a", "oauth-a"), BuildOptions{RequireValidatedClient: true})
	require.NoError(t, err)
	require.NoError(t, plan.Validate())
	require.NotEqual(t,
		planPatchValue(t, plan, CarrierHeader, "session-id"),
		planPatchValue(t, plan, CarrierBody, "prompt_cache_key"),
	)
}

func TestConflictingSamePrecedenceIdentityFailsBeforeProjection(t *testing.T) {
	headers := http.Header{
		"User-Agent": {"codex-tui/0.154.0 (Windows 10.0.26200; x86_64) WindowsTerminal"},
		"Thread-Id":  {"thread-a", "thread-b"},
	}
	plan, err := BuildPlan(Capture(headers, nil, DefaultLimits()), testProfile(t), testMapper(t, "tenant", "account"), BuildOptions{RequireValidatedClient: true})
	require.NoError(t, err)
	require.Error(t, plan.Validate())
	_, _, err = ProjectHTTP(headers, nil, plan)
	require.Error(t, err)
}

func TestMapperPreservesUUIDv7TimestampAndIsScopeStable(t *testing.T) {
	raw := "0199a1b2-c3d4-7e5f-8a9b-111111111111"
	mapperA := testMapper(t, "tenant-a", "oauth-a")
	first, err := mapperA.Map("thread", raw)
	require.NoError(t, err)
	second, err := mapperA.Map("thread", raw)
	require.NoError(t, err)
	require.Equal(t, first, second)
	parsedRaw := uuid.MustParse(raw)
	parsedMapped := uuid.MustParse(first)
	require.Equal(t, uuid.Version(7), parsedMapped.Version())
	require.Equal(t, parsedRaw[:6], parsedMapped[:6])

	otherAccount, err := testMapper(t, "tenant-a", "oauth-b").Map("thread", raw)
	require.NoError(t, err)
	require.NotEqual(t, first, otherAccount)
	otherTenant, err := testMapper(t, "tenant-b", "oauth-a").Map("thread", raw)
	require.NoError(t, err)
	require.NotEqual(t, first, otherTenant)
}

func TestSevenDeclaredClientProfilesArePreservedWithoutInstallationMerge(t *testing.T) {
	raw, err := os.ReadFile("testdata/seven_clients.json")
	require.NoError(t, err)
	var fixtures []clientFixture
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	require.Len(t, fixtures, 7)
	profile := testProfile(t)
	mapper := testMapper(t, "tenant", "oauth-account")
	seenUA := make(map[string]struct{})
	seenInstallations := make(map[string]struct{})
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			headers := http.Header{
				"User-Agent":              {fixture.UserAgent},
				"Originator":              {"codex-tui"},
				"Version":                 {"0.154.0"},
				"X-Codex-Installation-Id": {fixture.InstallationID},
			}
			plan, buildErr := BuildPlan(Capture(headers, nil, DefaultLimits()), profile, mapper, BuildOptions{RequireValidatedClient: true})
			require.NoError(t, buildErr)
			require.NoError(t, plan.Validate())
			require.Equal(t, fixture.UserAgent, plan.Client.UserAgent)
			mappedInstallation := planPatchValue(t, plan, CarrierHeader, "x-codex-installation-id")
			seenUA[plan.Client.UserAgent] = struct{}{}
			seenInstallations[mappedInstallation] = struct{}{}
		})
	}
	require.Len(t, seenUA, 7)
	require.Len(t, seenInstallations, 7)
}

func TestSameUserAgentStillKeepsDistinctInstallations(t *testing.T) {
	profile := testProfile(t)
	mapper := testMapper(t, "tenant", "oauth-account")
	ua := "codex-tui/0.154.0 (Windows 10.0.26200; x86_64) WindowsTerminal"
	installationIDs := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	}
	mapped := make(map[string]struct{})
	for _, installationID := range installationIDs {
		headers := http.Header{"User-Agent": {ua}, "X-Codex-Installation-Id": {installationID}}
		plan, err := BuildPlan(Capture(headers, nil, DefaultLimits()), profile, mapper, BuildOptions{RequireValidatedClient: true})
		require.NoError(t, err)
		require.NoError(t, plan.Validate())
		require.Equal(t, ua, plan.Client.UserAgent)
		mapped[planPatchValue(t, plan, CarrierHeader, "x-codex-installation-id")] = struct{}{}
	}
	require.Len(t, mapped, 2)
}

func TestRootAndChildThreadsShareSessionButKeepDistinctThreadIdentity(t *testing.T) {
	profile := testProfile(t)
	mapper := testMapper(t, "tenant", "oauth-account")
	session := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	rootThread := "0199a1b2-c3d4-7e5f-8a9b-111111111111"
	childThread := "0199a1b2-c3d4-7e5f-8a9b-222222222222"
	build := func(thread string) OutboundPlan {
		headers := http.Header{
			"User-Agent":          {"codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm"},
			"Session-Id":          {session},
			"Thread-Id":           {thread},
			"X-Client-Request-Id": {thread},
		}
		body := []byte(`{"prompt_cache_key":` + mustJSONString(session) + `,"client_metadata":{"session_id":` + mustJSONString(session) + `,"thread_id":` + mustJSONString(thread) + `}}`)
		plan, err := BuildPlan(Capture(headers, body, DefaultLimits()), profile, mapper, BuildOptions{RequireValidatedClient: true})
		require.NoError(t, err)
		require.NoError(t, plan.Validate())
		return plan
	}
	root := build(rootThread)
	child := build(childThread)
	require.Equal(t,
		planPatchValue(t, root, CarrierHeader, "session-id"),
		planPatchValue(t, child, CarrierHeader, "session-id"),
	)
	require.Equal(t,
		planPatchValue(t, root, CarrierBody, "prompt_cache_key"),
		planPatchValue(t, child, CarrierBody, "prompt_cache_key"),
	)
	require.NotEqual(t,
		planPatchValue(t, root, CarrierHeader, "thread-id"),
		planPatchValue(t, child, CarrierHeader, "thread-id"),
	)
	require.Equal(t,
		planPatchValue(t, child, CarrierHeader, "thread-id"),
		planPatchValue(t, child, CarrierHeader, "x-client-request-id"),
	)
}

func TestLineageReferenceMatchesMappedParentAcrossPlans(t *testing.T) {
	profile := testProfile(t)
	mapper := testMapper(t, "tenant", "oauth-account")
	parentThread := "0199a1b2-c3d4-7e5f-8a9b-111111111111"
	parentHeaders := http.Header{
		"User-Agent": {"codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm"},
		"Thread-Id":  {parentThread},
	}
	parentPlan, err := BuildPlan(Capture(parentHeaders, nil, DefaultLimits()), profile, mapper, BuildOptions{RequireValidatedClient: true})
	require.NoError(t, err)
	require.NoError(t, parentPlan.Validate())
	childMetadata := `{"thread_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222","parent_thread_id":"` + parentThread + `"}`
	childHeaders := http.Header{
		"User-Agent":            {"codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm"},
		"X-Codex-Turn-Metadata": {childMetadata},
	}
	childPlan, err := BuildPlan(Capture(childHeaders, nil, DefaultLimits()), profile, mapper, BuildOptions{RequireValidatedClient: true})
	require.NoError(t, err)
	require.NoError(t, childPlan.Validate())
	require.Equal(t,
		planPatchValue(t, parentPlan, CarrierHeader, "thread-id"),
		planPatchValue(t, childPlan, CarrierTurnMetadata, "header.x-codex-turn-metadata.parent_thread_id"),
	)
}

func TestUnsupportedVersionIsNotImpersonated(t *testing.T) {
	client, err := ResolveValidatedClientIdentity(testProfile(t), "codex-tui/0.153.0 (Mac OS 26.5; arm64) iTerm", "codex-tui", "0.153.0")
	require.NoError(t, err)
	require.False(t, client.Recognized)
	require.Equal(t, "unsupported_client_version", client.Reason)
}

func FuzzMapperDeterministic(f *testing.F) {
	f.Add("thread", "0199a1b2-c3d4-7e5f-8a9b-111111111111")
	f.Add("session", "session-value")
	f.Fuzz(func(t *testing.T, domain, raw string) {
		if len(domain) > 128 || len(raw) > 4096 || strings.TrimSpace(domain) == "" || strings.TrimSpace(raw) == "" {
			t.Skip()
		}
		mapper := testMapper(t, "tenant", "credential")
		first, err := mapper.Map(domain, raw)
		require.NoError(t, err)
		second, err := mapper.Map(domain, raw)
		require.NoError(t, err)
		require.Equal(t, first, second)
	})
}

func FuzzProjectHTTPDoesNotMutateInput(f *testing.F) {
	f.Add("session-a", "thread-a", "hello")
	f.Fuzz(func(t *testing.T, session, thread, input string) {
		if len(session) > 256 || len(thread) > 256 || len(input) > 4096 || strings.TrimSpace(session) == "" || strings.TrimSpace(thread) == "" {
			t.Skip()
		}
		headers := http.Header{
			"User-Agent": {"codex-tui/0.154.0 (Windows 10.0.26200; x86_64) WindowsTerminal"},
			"Session-Id": {session},
			"Thread-Id":  {thread},
		}
		encodedInput, _ := json.Marshal(input)
		body := []byte(`{"input":` + string(encodedInput) + `,"client_metadata":{"session_id":` + mustJSONString(session) + `,"thread_id":` + mustJSONString(thread) + `}}`)
		beforeHeaders := headers.Clone()
		beforeBody := append([]byte(nil), body...)
		plan, err := BuildPlan(Capture(headers, body, DefaultLimits()), testProfile(t), testMapper(t, "tenant", "credential"), BuildOptions{RequireValidatedClient: true})
		if err != nil || plan.Validate() != nil {
			return
		}
		_, _, _ = ProjectHTTP(headers, body, plan)
		require.Equal(t, beforeHeaders, headers)
		require.Equal(t, beforeBody, body)
	})
}

func mustJSONString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func BenchmarkBuildPlan(b *testing.B) {
	headers, body := semanticFixture()
	snapshot := Capture(headers, body, DefaultLimits())
	profile, _ := ProfileByID(Codex0154ProfileID)
	mapper, _ := NewMapper(MappingScope{
		Secret: []byte("01234567890123456789012345678901"), AuthScope: "tenant", CredentialScope: "credential",
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan, err := BuildPlan(snapshot, profile, mapper, BuildOptions{RequireValidatedClient: true})
		if err != nil || plan.Validate() != nil {
			b.Fatal("plan failed")
		}
	}
}

func TestProjectLeavesUnrelatedBytesUntouched(t *testing.T) {
	headers, body := semanticFixture()
	plan, err := BuildPlan(Capture(headers, body, DefaultLimits()), testProfile(t), testMapper(t, "tenant", "credential"), BuildOptions{RequireValidatedClient: true})
	require.NoError(t, err)
	_, nextBody, err := ProjectHTTP(headers, body, plan)
	require.NoError(t, err)
	for _, protected := range [][]byte{
		[]byte(`"input":"literal thread_id and session_id must stay untouched"`),
		[]byte(`"encrypted_content":"opaque-0199a1b2-c3d4-7e5f-8a9b-111111111111"`),
	} {
		require.True(t, bytes.Contains(body, protected))
		require.True(t, bytes.Contains(nextBody, protected))
	}
}
