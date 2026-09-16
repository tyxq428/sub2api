# B8 load-harness failure diagnosis

This document explains why `fullimage-soak02` is classified as a **test-harness failure**, not a candidate-product regression. It does not waive the required final two-hour `soak03` gate.

## Observed failure chain

- `soak02` used the full baseline and candidate images on one internal Docker network. Both variants were healthy and had processed about 31k requests each when the common upstream failed.
- At `2026-09-16T12:44:13.868Z` the WSL kernel recorded `potentially unexpected fatal signal 11` and `CaptureCrash` for PID 1, executable `/usr/local/bin/node`.
- The ELF core identifies the process as `node /harness/fake_upstream.cjs`, hostname/container prefix `93f4c5247f6a`.
- Core memory contains the fatal reason: `FATAL ERROR: Ineffective mark-compacts near heap limit Allocation failed - JavaScript heap out of memory`.
- The fault RIP bytes exactly match the pinned Node Alpine image's musl `abort()` path (`abort` at `0x426d0`, fallback HLT at `0x4274b`). No kernel OOM-killer or Docker network-reset event was observed.
- Docker/containerd records show the fake container exited first, then the load-agent; the six baseline/candidate app/Redis/PostgreSQL containers remained until controller cleanup. Both applications reported upstream SSE `unexpected EOF` at the same moment and returned 502 as a consequence.

## Root cause

The fake upstream retained every synthetic request identifier forever in `Set<string>`. Under sustained traffic this unbounded diagnostic structure exhausted the fake Node process V8 heap. The failure was shared test infrastructure and therefore simultaneously affected official-v0.2.5 baseline and candidate.

## Fix without weakening duplicate detection

The fake now parses its controlled request-id format and tracks exact sequence occupancy using per-worker bitmaps. It also rejects a mixed run prefix. Duplicate detection remains exact for the synthetic workload while memory grows with the highest sequence bit rather than the length of every request-id string.

Additional controller diagnostics now persist fake/load/app container states and logs before cleanup and sample fake/load RSS alongside the application processes.

## Verification

- Tracker self-test under a 128 MiB V8 heap: 400,020 unique IDs, exact intentional duplicate detection, 65,664 tracker bytes, ~6.8 MiB heap used.
- Accelerated fake-only HTTP/SSE test: 120,001 requests (past the old ~62k crash point), zero request errors/invalids, one injected duplicate detected exactly, 25,600 tracker bytes, ~12.4 MiB heap used; fake remained running and was not OOM-killed.
- Frozen-harness full-image `smoke04`: 1,420 upstream requests, zero errors/duplicates/invalids, tracker 640 bytes, ~6.3 MiB fake heap, all gates including `fake_tracker_bounded` true, and cleanup complete.
- The independent verifier now rejects fake tracker >=2 MiB or final fake heap >=128 MiB, in addition to the existing duration/error/duplicate/P95/RSS gates.

The final B8 acceptance still requires a fresh `sub2api-r1-b8-soak03` with 300 seconds warmup plus 7,200 seconds measured traffic. `soak01` and `soak02` remain retained as failed infrastructure evidence and are not counted as acceptance.
