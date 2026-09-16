# R1 B3d — Required route policy, token redirects, and resumed verification

Parent: `c45f1c43a8d014e4ae1248616cc80137681cc724`. This is an un-deployed local change on `feature/codex-gap-v1`; no production account is altered.

## Opt-in account contract

Existing account Extra can carry the boolean `openai_proxy_required`. Missing/false preserves legacy default-route policy. True requires an explicit valid binding; a present non-boolean is an error, not an opt-out. The flag is administrator account state, not an incoming request header. No database schema migration is added.

With this policy enabled, an absent binding, inactive/expired route or automatic-fallback provenance stops the call. A replacement environment/default proxy cannot satisfy an absent binding. Fresh administrative proxy lookup preserves the account policy instead of reconstructing an ID-only Account. Selected third-party plugins are rejected for such accounts because their internal egress has not been verified; the system does not silently bypass the selected plugin.

This checks the account snapshot supplied to the request; it is NOT a claim of immediate database revocation or a full immutable credential/metadata snapshot. Atomic policy updates, cache invalidation and already-in-flight requests remain separately scoped. Existing non-OpenAI behavior is unchanged. No production flag has been enabled.

## OAuth and Agent task network boundary

The OpenAI token client now opts into the existing no-redirect client policy, covering authorization-code exchange and refresh. Local 302/307/308 redirects must not result in a second host request, including credentialed form-data replay. Normal token success remains covered.

Agent task registration validates the account binding before signing or creating a network client, and uses DisableRedirects. Tests generate local synthetic keys and only access httptest servers; no actual registration or attestation request is made.

## Red/green evidence

- Initial new contracts: 7 primary tests actually ran; 5 defect/policy tests failed and 2 normal-behavior controls passed.
- Additional propagation and Agent tests: both primary tests failed before their corresponding implementation.
- Candidate targeted existing/new tests: **56 primary + 103 subtests passed**, 9/9 new required tests executed, zero failures/skips.
- Prior gate rerun: 31/31 required tests, 46 primary +130 subtests passed.
- Expanded checked-in gate: **40/40 required tests**, **55 primary +145 subtests passed**. This overlaps the other groups and must not be added as a unique total.
- Gate negative self-test passed. git diff --check passed.

## Triage of the interrupted Linux full suite

The interrupted run on c45f1c43 had 3 failed primary tests and is NOT a passing full-suite result. Diagnosis:

1. ent schema loading writes `.entc`; the read-only extracted source mount caused failure. A writable disposable baseline archive makes this test pass. The developer worktree and production mounts remain untouched.
2. Req client cache-key expectation omitted the new DisableRedirects component. Official baseline passes; the candidate test was updated to assert the full five-part key. Runtime protection was not relaxed.
3. Channel-monitor validation fixtures resolve api.openai.com/api.kimi.com before checking other fields. The same failure reproduces on unmodified official v0.2.5 with networking disabled. Container-only /etc/hosts mapping to documentation-range addresses (203.0.113.10 and 203.0.113.11), still with --network none, makes all 9 subcases pass. No upstream contact is enabled or required.

A diagnostic initially suffered Windows stdin CRLF conversion, so one package name ended with a CR byte; that run is not valid three-test evidence. The LF script was rerun and confirmed the above baseline outcomes.

The corrected full Linux suite must still be rerun on the new committed snapshot. Existing Linux -race evidence (167 primary +172 subtests) belongs to c45f1c43, not this new candidate.

## Logs

Stored under `D:\Temp\sub2api-build\r1`. Hashes:

- `b3d-red.jsonl`: `aa6716e5b51034b60db82bef81782f2cad6d0ba4f0f6391368eb3f0b2f63d78d`
- `b3d-propagation-red.jsonl`: `f60efdc2d99e69bacb695d1d5e438af7dfab693873add73a754c6a527e9055fd`
- `b3d-green.jsonl`: `5b8f8ad6263311250608815e42924f3f15be8e94c90fec228fc9f77ff252a532`
- `b3d-previous-gate.jsonl`: `1a323a86aaa200203d5c3836e77af14db56e62b9a28d10f93143351c4baf520b`
- `b3d-expanded-gate.jsonl`: `bc24f75449fb0e8de030b97cec974d4998a6fe54a8c075952089aae05dd16e5b`
- `baseline-triage-final.jsonl`: `0660090d61698f97fd14fc9c99e42c9207e92b71ed617531cb6404e1305ace86`
- `baseline-dns-fixture.jsonl`: `126a6ae8f4ad9453c128fc9aa3f475109afb32751906790f2d66721b33480932`

Live-budget tasks remain 0/24. Actual A/B/C requests, source-level final identity differences, complete-image/runtime/load checks and authenticated feature push are not represented by these offline results.
