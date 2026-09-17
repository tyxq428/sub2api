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

GitHub Actions reported zero workflow runs for the feature branch, so CI is
reported as **not observed**, never as green.

No real model request was made in B8. The earlier B4 7/12 live-baseline stop rule
remains respected.

The full verification record is `docs/codex-gap/r1/b8-verification.md`; local raw
evidence remains under `D:\Temp\sub2api-build\r1`.
