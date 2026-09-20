# R2 E6 Release Review and E7/E8 Handoff

Status: **E6 delivery candidate approved for an unmerged review PR.**

## Delivery shape

- Validated product source commit:
  `a1d287191d7a3c0a83f27bac3691a109d59d17bb`
- Delivery branch: `feature/codex-r2-identity`
- Review PR base: `feature/codex-gap-v1`
- The delivery documentation commit is intentionally a docs-only descendant
  of the validated product source commit. No product file is changed after the
  E5 candidate image and exact-head acceptance.
- Main is not merged or modified by E0-E6.
- `feature/codex-gap-v1` is not modified by E0-E6.
- No production deployment is part of E0-E6.

## Fresh review results

Fresh-context spec-compliance and code-quality reviews found no blocking
findings.

The spec review confirmed:

- R2 is independently default-off: mode off, shadow telemetry off,
  new-session admission off, and no default eligible accounts.
- Active R2 requires explicit OpenAI OAuth account allowlisting, a mapping key
  and key epoch, and rejects conflicting fingerprint convergence, custom UA,
  or force-Codex-CLI settings.
- Enforce mode requires a compatible durable session binding; new bindings are
  admitted only under the explicit `new_session_admission` gate.
- Admin diagnostics and drain operations remain under the authenticated,
  rate-limited and audited admin route group.
- Shadow telemetry persists keyed digests/aggregates rather than raw identity
  values and is bounded/nonblocking.
- WS connection reuse includes the local R2 compatibility digest so
  incompatible R2 identity/policy contexts do not share a pooled connection.

The code-quality review confirmed deterministic semantic selection/projection,
explicit conflict failure, bounded observer queues/events, durable CAS state,
compatible WS pool isolation, clean migration ownership and resource cleanup.
Exact-head lint reported 0 issues and the vulnerability scan found 0 reachable
vulnerabilities.

Nonblocking limitations retained for the next stage:

1. The two-hour full-image load test is API-key HTTP/SSE; OAuth-only R2
   semantics are covered by E4 protocol tests rather than that load generator.
2. A repository-wide race run is not a clean global gate because pre-existing
   parallel tests race on global `gin.SetMode`. R2-specific race/protocol
   coverage and the other frozen gates were not weakened.

## E7/E8 stop point

E6 ends at the unmerged PR. Do not perform any of the following without a new,
explicit authorization:

- enable R2 shadow mode in production;
- add production account IDs to the R2 eligible list;
- set production R2 HMAC secrets or key epochs;
- enable new-session admission in production;
- enable R2 enforce mode in production;
- migrate live policy by inference from R1 flags or cache misses;
- send real model traffic for R2 validation.

The production rollout sequence remains a separate E7/E8 decision.
