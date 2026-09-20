# R2.2 verification record

This record covers the isolated R2.2 development branch only. It does not
authorize or describe a production rollout.

## Source boundary

- Implementation baseline:
  6afffd9a27864fd6b81a05efe832f97d2e8e2362.
- All model-facing protocol tests use synthetic credentials and fake/local
  upstreams. No real OpenAI model request is part of this verification.
- gateway.codex_r2.wire_contract_mode remains default-off.

## Passing gates

- git diff --check.
- Full backend compile-only using go test -mod=readonly -run '^$' ./....
- Targeted R2/R2.2 HTTP, SSE, compact, WebSocket, turn-state, compression,
  durable-binding and authenticated route tests.
- go vet on the changed protocol/config packages.
- golangci-lint v2.13: zero issues after the one new-test Close result finding
  was fixed.
- govulncheck ./...: zero reachable vulnerabilities. The scanner reported
  vulnerabilities in required modules that are not reached by this code.
- Existing BenchmarkBuildPlan remained below the frozen 1 ms planner budget
  (observed approximately 73-92 microseconds/op in three runs).
- R2.2 metadata parser benchmark observed approximately 5.8-8.3
  microseconds/op, 3534 B/op and 62 allocs/op in three runs.
- High-cardinality synthetic identity verification mapped 50,000 session
  identities in each of two credential scopes with no within-scope collisions
  and no cross-scope reuse.
- Authenticated R2.2 admission integration verified one client request produces
  exactly one fake-upstream request while policy binding plus wire contract are
  persisted transactionally.
- Fresh review regression coverage verifies that enabling R2.2 enforce does
  not reinterpret or reject an already-bound r2-v1 0.155.1 session merely
  because R2.2's 0.155.1 reference evidence is incomplete. The evidence gate
  applies when admitting/using R2.2 state, not retroactively to legacy R2
  correctness state.
- Existing r2.2-wire-v1 bindings continue to validate their persisted wire
  contract even if the mutable global wire_contract_mode is later set to off;
  the runtime toggle cannot silently downgrade an already-admitted R2.2
  session.

## Repository-wide baseline findings

The complete unit suite is not a clean candidate-only gate on this Windows
checkout because TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort
fails. The same test was replayed three times in a temporary detached worktree
at the exact R2 baseline commit above and failed the same way. It is therefore
recorded as pre-existing baseline behavior rather than R2.2 evidence.

The broad integration run also observed a single
TestFetchCodexModelsManifestAPIKeyServesStaleWhileRefreshing timing failure.
An isolated ten-run replay of that exact candidate test passed 10/10, so the
broad run is not recorded as a clean pass and the isolated result is retained
as evidence of a pre-existing/timing-sensitive suite condition rather than an
R2.2 protocol regression.

## Explicitly unavailable evidence

- A repository-wide -race pass is not claimed. The active Windows Go
  environment reports that -race requires CGO.
- Fresh per-file hash refresh for the pinned Codex 0.155.1 commit is not
  claimed. Multiple GitHub/raw fixed-commit fetch attempts failed at the
  transport/DNS layer. The exact commit remains pinned, and moving main was
  not substituted as evidence. R2.2 enforce therefore rejects the 0.155.1
  wire contract until that fixed-reference evidence is complete; shadow may
  still compute it for differential observation.
- A production receiver observation is not claimed. receiver_observed is an
  evidence-plane label reserved for an isolated receiver/fake upstream or a
  separately authorized production observation; local request construction is
  only semantic_ready / transport_ready.

These unavailable or baseline-limited checks remain explicit rather than being
converted into passing evidence.
