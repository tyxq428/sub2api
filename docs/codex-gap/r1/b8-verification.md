# R1 B8 — final Linux, image and workload acceptance

B8 status: RUNNING

The final two-hour workload is still running. This is a progress record, **not** a
completed B8 verdict. Replace this status only after the actual terminal result,
independent raw-data verification, delivery commit/push and package checks.

## Source and scope

Candidate source: `8905cbb82d4bad011eddae5e83228f52906c4b40`.
Production-code parent: `da35837051c0a3a87eb49b12cf3882850b7539e3`.
The two later commits `3279bdbab...` and `8905cbb82...` fix old test fixtures that
read zstd wire bytes as JSON; they do not change the production forwarding logic.
The previous conversation handoff missed those commits and is superseded here.

Repository: `E:\Apps\sub2api-worktrees\codex-gap-v1`.
Branch: `feature/codex-gap-v1`.
Task: `task_Wp1FXYEsm1bgOYAX9YEI` (`mcp-manager` is only a command entry).
Only B8 is continued. P0/B3–B7 are not rerun. Main is not merged and production,
Nginx, databases, accounts and Manager/Gateway code are not changed.

Documentation/external harness commits after the source commit are not relabeled
as image source. The final delivery manifest records product and documentation
commits separately and rejects product-tree differences after image construction.

## Recovered jobs and retained evidence

The three requested old handles are now `lost`, not successful active jobs:
`job_Xl65ho3lTeSE_bvHGCrZ`, `job_dHB_s5dKSnL_W6Z7N4K1`, and
`job_hIezPNyCMhni6QS5inpB`. Their bounded logs were inspected; later successful
release3 outputs already supersede the unfinished older snapshot/fixture work.
No duplicate B8 Linux suite or image build was started for the candidate.

The retained results are under `D:\Temp\sub2api-build\r1`.
`job_7_Vy9LLx_5NEtGt6KFJG` rechecked full/race raw-log hashes.
`job_0WDhayXGnNfqxvbXWiRj` independently rechecked source/build files, actual test
execution and candidate export identity. Both completed with exit code 0.

| Gate | Observed result | Evidence |
| --- | --- | --- |
| Full Linux unit-tag suite | 10,890 top-level tests + 9,062 subtests; 57 packages; exit 0 | `linux-full-unit-release3-result.json` and raw JSONL |
| Required current gate | 67/67 actually ran and passed; none missing or skipped | Full-suite result |
| Additional R1-named-file audit | 38/38 test functions independently found in raw run/pass events | `b8-release-evidence-reverified.json` |
| Critical Linux race selection | 211/211 required top-level + 235 subtests; 0 race warnings, failures or skipped-required | `linux-race-release3-result.json` and raw JSONL |
| Archive/build identity | 3,985 regular files checked; 0 mismatches | Reverified evidence |
| Full frontend image | Built and version/commit/digest checked | `candidate-release-metadata.json`, `candidate-release-inspect.json` |
| Fresh PG/Redis runtime | Health OK; 100 tables, 1 synthetic admin, 0 accounts; HTML and JS served; internal network | `candidate-runtime-release-result.json` |
| Full-image HTTP/SSE short smoke | Passed, exact 1,420 fake upstream requests; 0 errors/duplicates/invalid inputs; all 8 test resources removed | `fullimage-smoke01/result.json` |
| Actual 2-hour measurement | RUNNING; not yet accepted | `fullimage-soak01/status.json` |
| Independent verifier | 18 synthetic unit tests passed; actual running soak correctly rejected as incomplete | `b8-independent-verifier-unit-result.json` |

The full unit-tag run has 67 skip events including 16 named skipped tests.
The skipped names are preserved in `b8-release-evidence-reverified.json`, including
live TLS/API and optional DB/Redis/plugin tests. This is not a claim that every
integration or live test ran; **no required current gate was skipped**.

## Candidate image and export

- Tag: `local/sub2api-r1:sha-8905cbb82d4b`
- Version: `0.2.5-r1.8905cbb82d4b`; platform: `linux/amd64`
- Image digest: `sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85`
- Binary SHA-256: `2a85591bcf9ee7efeb84b957555abdc19ed6f78ecf4ae9e0253734d8f1617f37`
- Docker archive: `sub2api-r1-8905cbb82d4b.docker.tar`, 45,473,792 bytes
- Archive SHA-256: `932b09eaf4359a1dc651b2367fabdecfbb116e29fed94407a48d6097ab2ff28f`
- Source archive SHA-256: `cd41416445a74e22185e525700ea4ee8f5bb7b518f4f7b121fbfdafe76c339d2`

The image uses pinned Node, Go, Alpine and PostgreSQL base-image digests from
`image-inputs-lock.json`. The actual binary reports the expected version/commit.
The old `2e940cdb...` image is historical and is not the delivery candidate.
No candidate image has been deployed to production or published as a production release.

## Real full-image workload (pending terminal result)

The old request-builder-only harness was rejected as insufficient: it never made
an actual application-to-upstream request. The replacement uses the complete
candidate and baseline application images, actual HTTP ingress, authentication,
scheduling, forwarding, billing and response handling with separate fresh databases.

Baseline: official `v0.2.5` source `86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea`,
unmodified, rebuilt with the same pinned base images. Baseline image digest:
`sha256:8af502b156cecdbb6f9469d604776c540ae501995094f9d8a50bb08f4bd42611`.
This is a source-controlled rebuild, not a claim of using an official registry binary.
Baseline build `job_May3XLYbCAXQ9p_BBNpE` completed with exit code 0.

Run `sub2api-r1-b8-soak01`, durable job `job_n0okoG6TnqkKTNrOLjax`:
300 seconds warmup + **7,200 seconds measured**, 20 requests/second per variant,
four clients per variant, 4 KiB synthetic inputs, alternating JSON and SSE.
Current estimated measurement finish: **2026-09-16T11:59:38.531890+00:00**, subject to actual termination
and cleanup. A one-time scheduled continuation collects the result afterward;
a conversation interruption must not restart this workload.

All seven containers use a labeled internal-only network and no published ports.
The baseline/candidate have separate PG/Redis data. Only synthetic credentials and
API-key accounts exist. No real model calls are made. Test resources have strict
CPU/memory limits, bounded logs and run-label-checked cleanup.

Acceptance requires actual complete duration and samples, zero errors and duplicate
upstream requests, exact client/upstream count reconciliation, both JSON and SSE
P95 degradation <=10%, stable app RSS degradation <=20%, and complete cleanup.
`tools/codex-r1-load/verify.py` independently recomputes the metrics from raw data,
checks identities/hashes, and refuses short/partial runs. No threshold is relaxed.
The smoke numbers are explicitly not the two-hour performance verdict.

## Remote/CI status and limits

At the pre-close readback (`b8-remote-ci-preclose.json`), origin feature was exactly
`8905cbb82d4bad011eddae5e83228f52906c4b40`; main remained
`30ed40a56a5f4b5ab7b8dd3d685353db3a531c84`. GitHub Actions query returned HTTP 200
with **0 runs**, which is recorded as no CI observation, not a green CI result.
Fresh remote readback is required after the final delivery commit.

B4 ended with 7/12 baseline slots used and no successful fresh official live A;
its stop rule remains in force and candidate real requests remain 0/12. There are
no new real-account requests in B8. No live A/C parity, exact TLS/ALPN/JA3 equality,
account-risk reduction or account safety guarantee is claimed.

The workload is **API-key HTTP/SSE**, not a full-image OAuth/zstd or WS load test,
not a maximum-throughput test, and not a time-to-first-token benchmark. Earlier
contract/race evidence retains its separate, expressly limited scope.

## Remaining B8 closure

Read the running job's existing result; do not rerun B3–B7 or restart soak01.
After all actual workload gates pass, independently verify, finish this report and
`final-summary.md`, commit/push only feature documentation/harness updates, verify
remote feature/main and CI availability, create the allowlisted ZIP with hashes,
and then clear B8 pending work and mark the durable task completed.
B9/main merge and any production changes remain outside this authorization.
