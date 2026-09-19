# Codex R2: approved implementation boundary

Status: E0-E6 approved for isolated implementation on 2026-09-19. E7/E8 are NOT approved.

User approval: "批准 R2 确认版 v1.0，执行 E0—E6；不合并 main、不部署生产、不发送真实模型请求；E7/E8 另行确认。"

## Immutable inputs

- Source baseline: `434112e816951080f3e9619f30d5f5bc1165c8a0`.
- Official Codex reference: `6b9826e3aa83b1a5947db50f4332cb9c65f1b340` (tag `rust-v0.154.0`).
- Approved document: `R2_Implementation_Design_For_Approval_ZH.md`, 41008 bytes; SHA-256 `643128d604c5a07067006c62a8f3ff2b67be99bbfbea43be62f92280a089b0e4`.
- Archived approval package: `Sub2API_R2_Approval_Package_v1.zip`, 20327 bytes; SHA-256 `16942b2aefe9efb716f4b9aa5faf59d73a405639d1128c351868beda7d21c363`.
- The immutable originals remain attached to the approving conversation. This file is the execution index, not a replacement for the full plan.
- Durable execution task: `task_plaFTlstJ7DAsXWpS8iV`.
- Delivery branch: `feature/codex-r2-identity`; do not update `feature/codex-gap-v1` or `main`.

## Required delivery

E0: freeze baseline/reference/purpose matrix and budgets.
E1: immutable identity snapshots, default-off/shadow admission, private bounded observer; actual-output equivalence.
E2: pure versioned profile/graph/mapping/codec/UA/projection, seven heterogeneous synthetic client fixtures and property/fuzz tests.
E3: durable policy/scope/lineage anchors with CAS, explicit admission/drain, bounded telemetry store and protected admin UI/API.
E4: exclusive legacy/v2 writers on native HTTP/SSE/compact/HTTP-to-WS/direct WS; compatible bounded pools, prewarm/lane behavior, auxiliary-purpose inventory.
E5: exact-head CI/security; fresh image; synthetic high-cardinality and 300+7200-second comparison; isolated lifecycle/rollback; verify cleanup.
E6: review, feature push/unmerged PR, evidence-bound handoff.

## Hard invariants

- No production or existing staging modification, live data copy, real account credentials or model requests.
- No automatic R2 admission from R1 flags, cache misses, UA or caller headers.
- No device/session/full convergence or device identity derived from UA.
- Preserve scope isolation and all global proxy, redirect, replay and gRPC security protections.
- No fabricated client analytics, cookie, attestation, geographic or timezone data.
- Raw, actual and proposed observations are distinct; shadow never sends a second upstream request.
- Correctness bindings are durable and cannot be evicted by telemetry retention; existing v2 state cannot silently fall back to v1.
- Unknown versions, missing identity, insufficient evidence and incomplete telemetry remain explicitly unknown.
- Product comments about upstream policy and prior percentage estimates are not acceptance evidence.
- A blocked tool operation is a stopping condition, not permission to find a bypass.

## Implementation status

This approval index does not claim implementation, protocol or performance gates passed. Evidence is recorded per stage in the durable task and verification reports.
