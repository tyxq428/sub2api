# R1 B8 — final Linux, image and workload acceptance

B8 status: PASSED

R1 B8 is closed for the fixed candidate image built from product source commit
`8905cbb82d4bad011eddae5e83228f52906c4b40`. The acceptance workload used only
synthetic credentials and a fake upstream. It made **0 real model requests**.
No main-branch merge or production deployment/change is part of this result.

## Identity and scope

- Repository: `E:\Apps\sub2api-worktrees\codex-gap-v1`
- Delivery branch: `feature/codex-gap-v1`
- Product image source: `8905cbb82d4bad011eddae5e83228f52906c4b40`
- Candidate image: `sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85`
- Baseline: unmodified official `v0.2.5` source rebuilt with the same pinned build inputs,
  `sha256:8af502b156cecdbb6f9469d604776c540ae501995094f9d8a50bb08f4bd42611`
- Frozen final harness source commit: `f0d90f5bb794d46cc9c4842ca8c366f8fe618ff0`
- Final accepted run: `sub2api-r1-b8-soak05`

The later delivery commit contains only B8 tooling/documentation. It is **not** the
source of the candidate image; the binary/image remains tied to `8905cbb82d4b...`.

## Earlier B8 gates retained without rerun

P0 and B3–B7 were not rerun during this closeout.

| Gate | Result |
| --- | --- |
| Full Linux unit-tag suite | 10,890 top-level tests + 9,062 subtests, 57 packages, exit 0 |
| Required original gate | 67/67 ran and passed; none missing or skipped |
| Critical Linux race selection | 211/211 required top-level + 235 subtests; 0 race warnings/failures/skipped-required |
| Candidate archive/build identity | 3,985 regular files checked; 0 mismatches |
| Fresh PG/Redis runtime | Health OK; 100 tables, 1 synthetic admin, 0 accounts; internal network |
| Candidate image/export | Digest, version, binary and Docker tar identities verified |
| Verifier unit suite after final-run binding | 20/20 passed |

## Infrastructure-invalid runs

`soak01`–`soak04` are retained as diagnostic evidence and are neither product
passes nor product failures. In particular:

- `soak02`: fake-upstream `Set<string>` grew without bound and caused a V8 heap OOM.
  The fixed bitset tracker subsequently passed the 400,020-ID self-test, 120,001
  HTTP/SSE stress test, and `smoke04`.
- `soak03`: `host_interruption_invalid_run`; Windows Kernel-Power 109 initiated a
  shutdown at 2026-09-16 07:38:43 -07:00 and the host restarted later. Evidence:
  `b8-soak03-host-interruption.json`.
- `soak04`: WSL lifecycle reclamation stopped the Linux VM even though systemd
  services/containers were present. Evidence: `b8-soak04-wsl-lifecycle-invalid.json`.

These runs never relax or substitute for the full-duration gate.

## Final full-image workload — soak05

Evidence root: `D:\Temp\sub2api-build\r1\fullimage-soak05`.

The controller completed 300 seconds of warmup plus 7,200 seconds of measured
traffic at 20 requests/second **per version**, using the real application HTTP
`/v1/responses` path against a synthetic fake upstream. JSON and SSE alternate.
Both images ran simultaneously with separate fresh PostgreSQL and Redis instances
on an internal-only Docker network.

Observed terminal values:

| Metric | Baseline | Candidate | Candidate degradation |
| --- | ---: | ---: | ---: |
| Total requests (warmup + measured) | 150,000 | 150,000 | — |
| Measured stream samples | 72,000 | 72,000 | — |
| Stream P95 | 56.060265 ms | 56.172200 ms | +0.199669% |
| Measured non-stream samples | 72,000 | 72,000 | — |
| Non-stream P95 | 39.382082 ms | 39.650673 ms | +0.682013% |
| Stable RSS median | 178,655,232 B | 177,852,416 B | -0.449366% |
| Stable RSS observations | 120 | 120 | — |

Additional terminal evidence:

- Actual controller elapsed time: `7499.988082468` seconds.
- Upstream requests: exactly `300020`, matching both clients plus the 20 setup probes.
- Request errors: baseline `0`, candidate `0`.
- Upstream duplicates: `0`; invalid inputs: `0`.
- Fake duplicate tracker: 10 keys, 65,664 bytes; fake V8 heap used 8,206,328 bytes.
- `real_model_requests`: `0`.
- Raw memory window: 249 samples, first at 0.68 s, last at 7529.78 s.
- Cleanup: all 9 owned resources removed; `cleanup_complete=true`.
- Driver exit: `0`.
- Internal-network evidence: true.

Every controller gate is true: zero errors, no duplicates, valid inputs, exact
upstream count, duration complete, sample count complete, both P95 gates, stable
RSS gate, bounded fake tracker, and complete cleanup.

## Frozen harness identity

The accepted run's environment records and the persisted frozen directory both
match these SHA-256 values:

- `run.py`: `d5b3baf318cea8596137797aaf5721ed49d0973fc70cf0dd0c2b17712b085f60`
- `load_agent.cjs`: `61b5a329378d52257e7782641afe30ea67896d2994379318d68e5b0a35fe9596`
- `fake_upstream.cjs`: `cff43063aeea46844f473c4960d94ef239feb4da7a7db932b2a2c3b233f67980`
- `duplicate_tracker.cjs`: `30511bdb5531a1be2e7176122df0b4c35e34c85f2525c14edef250d9178fc839`

`fullimage-soak05-driver.sh` SHA-256:
`1e284bc246f46dc88b1e2a4e7292f3cdc18474c48cd6e90b5dd59bd314e7f03b`.

## Independent verification

`tools/codex-r1-load/verify.py D:\Temp\sub2api-build\r1` independently rereads
the raw latency, memory, environment, Linux-suite, race-suite, image export and
source/archive evidence. The final-run selector was changed from the superseded
`soak03` identity to the actual accepted `soak05` identity; no duration,
performance, identity, cleanup, or safety threshold was weakened.

The verifier passed and wrote `b8-independent-acceptance.json`. Recomputed results:

- stream P95 degradation: `+0.1996690533%` (limit `<=10%`)
- non-stream P95 degradation: `+0.6820132059%` (limit `<=10%`)
- stable RSS degradation: `-0.4493660729%` (limit `<=20%`)
- stream/non-stream samples: 72,000 each per version
- stable RSS samples: 120 per version

## Candidate release export

- Tag: `local/sub2api-r1:sha-8905cbb82d4b`
- Version: `0.2.5-r1.8905cbb82d4b`
- Platform: `linux/amd64`
- Candidate digest: `sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85`
- Binary SHA-256: `2a85591bcf9ee7efeb84b957555abdc19ed6f78ecf4ae9e0253734d8f1617f37`
- Docker tar: `sub2api-r1-8905cbb82d4b.docker.tar`
- Docker tar bytes: `45,473,792`
- Docker tar SHA-256: `932b09eaf4359a1dc651b2367fabdecfbb116e29fed94407a48d6097ab2ff28f`
- Source tar SHA-256: `cd41416445a74e22185e525700ea4ee8f5bb7b518f4f7b121fbfdafe76c339d2`
- `production_deployed=false`

## Remote / CI / delivery boundary

Before the final delivery commit, local HEAD and `origin/feature/codex-gap-v1`
were both `f0d90f5bb794d46cc9c4842ca8c366f8fe618ff0`; `origin/main` was
`30ed40a56a5f4b5ab7b8dd3d685353db3a531c84`.

GitHub Actions returned **0 workflow runs** for the feature branch. This is
recorded as **CI not observed**, not as a green CI result. The post-push readback
is saved in `b8-git-final.json` and `b8-remote-ci-final.json` inside the local
acceptance evidence/package.

No force push is used. Main is not merged. No production Nginx, database,
account, Manager/Gateway, or runtime deployment change is authorized or performed.

## Limitations

This B8 workload is an API-key HTTP/SSE full-image test, not a full-image OAuth/zstd
or WebSocket load test, not a maximum-throughput test, and not a TTFT benchmark.
Earlier contract/race evidence retains its separate scope. No live A/C parity,
TLS/ALPN/JA3 equality, account-risk reduction, or account-safety guarantee is
claimed.

B4 had already reached its 7/12 baseline stop rule; B8 issued no new real-account
or real-model request.
