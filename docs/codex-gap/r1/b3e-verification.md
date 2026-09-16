# R1 B3e — outbound egress closure, attempt boundary, and endpoint inventory

Baseline for this batch: `2e940cdbdd0c3d27ba3bd46c86b1a28daf404427`. This batch is local until the commit at the end of this document is pushed. Production remains official Sub2API v0.2.5 and account 8 is unchanged.

## Newly reproduced defects

Three independent red contracts were reproduced on the unmodified B3d product code:

1. `ProbeOpenAIAPIKeyResponsesSupport`: an OpenAI API-key account with `ProxyID != nil` and an unresolved `Proxy` dispatched `DoWithTLS` with an empty route. The accepted red log is the assertion-based rerun, not the earlier invalid `io.NopCloser(nil)` fixture panic.
2. Codex PAT `/whoami`: the credentialed client followed local 302/307/308 redirects to a second listener. The test token is synthetic.
3. Two OpenAI admin API-key account tests, `/v1/chat/completions` and `/v1/images/generations`, called `httpUpstream.DoWithTLS` directly and therefore could dispatch with an empty candidate route when an explicit binding was unresolved.

## Minimal fixes

- API-key Responses capability probing resolves `resolveOpenAIAccountProxyURL` before its network call. A route error is logged and the best-effort probe returns without updating capability state; account creation/update remains non-blocking.
- PAT `/whoami` uses the existing `DisableRedirects` HTTP-client option. The normal 2xx validation test still passes.
- API-key Chat Completions and Images admin tests now use `doOpenAIAccountTestUpstream(..., true)`, retaining TLS-profile behavior while sharing the final OpenAI dispatch resolver. Non-OpenAI accounts remain governed by the existing candidate-route behavior of that wrapper.
- No UA, session/thread lifecycle, TLS profile, WS framing, database schema or production configuration changed.

## Final OpenAI network-boundary inventory

| Surface | Final route boundary / policy | B3 evidence status |
|---|---|---|
| Public HTTP Responses / passthrough / messages / compatible bridges | `doOpenAIUpstream` → `resolveOpenAIDispatchProxyURL` | covered by GapV2 dispatch and no-direct tests |
| Plugin handoff | same final resolver before plugin; `openai_proxy_required=true` rejects selected plugin whose internal egress is unverified | covered by required-proxy plugin test |
| HTTP account tests (OAuth/APIKey, Chat Completions, compact, images) | `doOpenAIAccountTestUpstream` → final resolver | all 5 `testOpenAI*` functions statically audited with zero direct `httpUpstream.Do/DoWithTLS` calls; new API-key red/green coverage |
| API-key Responses capability probe | `resolveOpenAIAccountProxyURL` immediately before `DoWithTLS` | new red/green coverage |
| Pooled WS ingress / v2 | pool `Acquire` validates final dispatch route; physical compatibility includes route hash | broken-binding, queued snapshot and route-change reuse tests |
| Dedicated WS passthrough / HTTP bridge | `resolveOpenAIRequestProxyURL` / dispatch resolver before dial/bridge | B3b dedicated WS tests |
| OAuth authorization session / callback | session captures requested ProxyID; current binding must still match before token exchange | B3a session-change/revocation tests |
| OAuth refresh and admin refresh | fresh proxy record lookup while preserving account policy | B3a/B3d tests |
| OAuth token exchange / refresh client | validated caller route + no automatic redirects | B3d redirect tests |
| Models / manifest | credential-owner route and fail-closed binding; credentialed redirect refused | GapV2 + B3c tests |
| Quota | binding validated before token lookup/client creation | Batch 1/2 quota contract tests |
| Privacy | binding validated before client; arbitrary redirect refused | GapV2/B3c tests |
| Image binary/download/backfill helpers | credential-owner route; unresolved binding stops fetch | B3b image tests |
| Alpha search / live helper routing | credential-owner route; PAT metadata validation receives the resolved route | B3b helper tests |
| Codex PAT `/whoami` | caller-resolved route + `DisableRedirects` | new red/green redirect test |
| Agent Identity task registration | explicit binding validation before signing/network; redirects disabled | B3d tests |

This inventory is a code/network-boundary claim for the supported OpenAI paths reviewed in R1. It is not a claim that every feature above is exercised by account 8 or enabled by current traffic.

## Revocation, cache, queued and in-flight contract

B3 deliberately uses an **attempt boundary**, not mid-flight route mutation:

1. Before a new network attempt crosses the final HTTP/WS dispatch boundary, its account snapshot and explicit binding are validated.
2. For `openai_proxy_required=true`, a missing binding, inactive/expired route, malformed policy or automatic fallback provenance rejects the new attempt instead of substituting direct/default egress.
3. OAuth authorization sessions bind the selected ProxyID and reject a changed/revoked binding before exchange.
4. Administrative refresh paths may reload the current proxy record, but preserve the account policy/fallback provenance.
5. WS queued/prewarm data snapshots route fields; idle physical reuse includes a route hash, so changing A→B does not reuse an A connection for B.
6. Once an HTTP attempt has already crossed the final dispatch boundary with a validated route, a concurrent account-object change does **not** rewrite that in-flight connection. `TestR1RequiredProxyAttemptBoundary` proves the first attempt remains on its captured route while the next attempt fails before a second network call after binding removal.
7. B3 does **not** promise instantaneous database-driven cancellation of an already-sent HTTP request or already-active WS turn. That would introduce replay/cancellation ambiguity and belongs to B6 attempt/commit semantics rather than egress fail-closed policy.

## Production-scope observation

Read-only production inspection after this batch showed `RUN_MODE=standard` and `TZ=Asia/Shanghai`; no explicit `GATEWAY_OPENAI_*` or `GATEWAY_FORCE_CODEX_CLI` environment overrides were present in the inspected application container. Production continues to run official `weishaw/sub2api:0.2.5`; the R1 `openai_proxy_required` flag has not been enabled on account 8.

Read-only Nginx inspection shows the Sub2API location proxying to `127.0.0.1:8081` with HTTP/1.1, Upgrade/Connection forwarding and 600s read timeout. No explicit `underscores_in_headers` directive was observed. Whether underscore-named headers survive the exact external Nginx→Gin chain remains a B4 synthetic-chain test; B3 does not infer the result from defaults.

## Verification

- Valid red: API-key capability probe and PAT redirect each executed and failed before product fixes.
- Valid red: both API-key admin test subpaths executed and failed before switching to the unified dispatch boundary.
- Targeted green: original normal capability/PAT behavior plus both new fixes passed.
- Attempt-boundary characterization passed.
- Final required execution gate: **44/44 required tests executed and passed; 59 primary + 150 subtests; zero failures and zero skipped-required tests**. Counts overlap targeted groups and must not be summed.
- Machine static audit: 5 `testOpenAI*` admin functions, `unsafe_direct_count=0` for direct `httpUpstream.Do/DoWithTLS` calls.
- `git diff --check` passes before commit.

### Evidence hashes

- `b3e-red-TestR1APIKeyResponsesProbeBrokenBindingDoesNotDispatch.jsonl`: `77520dd73b0d6c7c98c0637fd49271e534a305898e618438aa00c5cf96aaa16a`
- `b3e-red-TestR1CodexPATWhoamiDoesNotFollowRedirect.jsonl`: `3b69a95efb8ad1f9b8f733e5535fe91f96bd28c3553701a2329975d239005bf8`
- `b3e-green.jsonl`: `3b431c4c46dd28d8120a30902ea8150a73f6250512b8a5fa25639472dd92a254`
- `b3e-attempt-green.jsonl`: `a23b16c272d026bb1a38dd43a0b8c6002b0a040e1b959a2031bee6c00ea86e89`
- `b3e-accounttest-red.jsonl`: `8aa3a3b6e001578267e0e1d227d423080444dc76db9bf68a845883d60c1bec4b`
- `b3e-accounttest-green.jsonl`: `6731a9d2c9a851de5711441d2023dac52d27dbe52d6c8bea6eecbb3e91bd64d5`
- `b3e-complete-gate.jsonl`: `302f8524c7575eaf7e4c6ba17f1001fdd981f23481816ace0fa7a1bcc8f29445`

The feature branch's prior commit has already been pushed through worker-local GCM. This B3e commit is pushed only after local commit verification; main and production remain untouched.
