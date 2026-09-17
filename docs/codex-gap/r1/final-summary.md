# R1 final summary

R1 acceptance result: **PASSED through B8** for the fixed candidate image sourced
from `8905cbb82d4bad011eddae5e83228f52906c4b40`.

The delivered candidate is
`sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85`.
The comparison baseline is the unmodified official `v0.2.5` source rebuilt with
the same pinned inputs,
`sha256:8af502b156cecdbb6f9469d604776c540ae501995094f9d8a50bb08f4bd42611`.

## What passed

P0 and B3–B7 were already complete and were not rerun during final closeout.
B8 retained successful Linux evidence: 10,890 top-level unit-tag tests, 67/67
required current gates, and the critical race selection 211/211 with no race
warning, required failure, or required skip. The candidate image/export and fresh
PG/Redis runtime evidence also passed.

The final accepted full-image run was `sub2api-r1-b8-soak05`: 300 seconds warmup
plus 7,200 seconds measured at 20 RPS per version. It completed 150,000 requests
per version and 300,020 exact fake-upstream requests, with zero request errors,
zero duplicate upstream requests, zero invalid inputs, zero real model requests,
complete measured samples, and complete cleanup of all 9 owned resources.

Candidate versus baseline:
- stream P95: 56.172200 ms vs 56.060265 ms, `+0.199669%`;
- non-stream P95: 39.650673 ms vs 39.382082 ms, `+0.682013%`;
- stable RSS median: 177,852,416 B vs 178,655,232 B, `-0.449366%`.

All required limits therefore pass: each P95 degradation is `<=10%` and stable
RSS degradation is `<=20%`. The bounded fake tracker used 65,664 bytes and the
fake V8 heap used 8,206,328 bytes at terminal collection.

The independent verifier re-read raw latencies, memory samples, identities,
cleanup, release archives, Linux logs and export hashes and passed. Its final-run
binding is `soak05`; the earlier `soak03` and `soak04` runs remain explicitly
infrastructure-invalid evidence rather than being reclassified.

## Delivery identities

Product image source and delivery-document/tool commits are intentionally separate.
The candidate image is and remains from `8905cbb82d4b...`. The frozen v3 harness
used for the accepted run came from `f0d90f5bb794d46cc9c4842ca8c366f8fe618ff0`.
A later closeout commit contains only `docs/codex-gap/r1/` and
`tools/codex-r1-load/` changes and does not rebuild or relabel the image.

Candidate Docker archive:
`D:\Temp\sub2api-build\r1\sub2api-r1-8905cbb82d4b.docker.tar`
SHA-256:
`932b09eaf4359a1dc651b2367fabdecfbb116e29fed94407a48d6097ab2ff28f`.

Source archive SHA-256:
`cd41416445a74e22185e525700ea4ee8f5bb7b518f4f7b121fbfdafe76c339d2`.

The local acceptance ZIP and its hash are recorded in
`D:\Temp\sub2api-build\r1\b8-delivery-package.json` after the final feature
push/readback.

## Boundaries

`origin/main` was `30ed40a56a5f4b5ab7b8dd3d685353db3a531c84` before final delivery and is
required to remain unchanged. No main merge, production deployment, production
Nginx/database/account modification, or Manager/Gateway modification is part of
this closeout.

At B8 closeout time GitHub Actions had reported zero workflow runs, so that
historical B8 statement was correctly recorded as **not observed**. During the
subsequent B9 continuation, CI option B was explicitly selected: a manual
`workflow_dispatch` trigger was added while retaining the existing push/PR
triggers. CI then ran on the feature branch. After fixing test-only cleanup
`errcheck` findings, run `35196231446` on support/test HEAD
`7e1d7ed03437ef40cccc80da6d14ce7f8dd5d1c8` completed successfully with all
four jobs (`frontend`, `test`, `golangci-lint`, `shell`) green; unit and
integration tests both passed. This later CI evidence does not change the
frozen product-image source `8905cbb82d4b...`.

B9 also added an authorized fresh production logical-backup recovery check.
The protected custom-format dump SHA-256 is
`0444079f558b26f288a4cfd93d1ff1485747587c873cf481c56b34df0aefbdc5`.
It restored with `pg_restore --exit-on-error` returning 0 in a disposable
`network=none` PostgreSQL instance with no published ports. Archive schema-only
SQL normalized exactly to the contemporaneous production schema, including all
80 foreign keys. See `docs/codex-gap/r1/b9-staging-verification.md` for the
recovery caveats.

The B9 authenticated-protocol residual was subsequently closed by a synthetic
in-process integration harness using the real API-key middleware, gateway
handler/scheduler, concrete zstd HTTP path, and real local WebSocket transport.
CI run `35202176347` on `028fac5eac30...` passed all four jobs and covers
upstream-zstd JSON/SSE, parsed compact opaque preservation, successful WS
multi-turn, and one-time continuation recovery after
`previous_response_not_found`. No real model request or account 8 was used.

The sole remaining application release gate at this checkpoint is the
disposable lifecycle rehearsal (candidate cold recreation and
candidate -> previous -> candidate). Assistant-side execution was safety-blocked
before mutation, so the reviewed manual runner must be executed once on the VPS
and its non-sensitive `passed: true` JSON independently read back before B9 can
be declared fully closed.

No real model request was made in B8. The earlier B4 7/12 live-baseline stop rule
remains respected.

The full verification record is `docs/codex-gap/r1/b8-verification.md`; local raw
evidence remains under `D:\Temp\sub2api-build\r1`.
