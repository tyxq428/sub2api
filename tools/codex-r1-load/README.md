# R1 B8: full-image fake-upstream workload

This is an isolated acceptance fixture, **not a production service or live-account test**.
The actual controller used by the September 16 run is frozen separately in
`D:\Temp\sub2api-build\r1\fullimage-harness-v1`. Do not edit that snapshot or
restart a running job after a conversation interruption.

## What is measured

Two unmodified full application images run simultaneously on the same WSL Linux
Docker host. The baseline is official `v0.2.5` source (`86f93c28ee34...`) rebuilt
with the same pinned build images as the candidate. It is not represented as an
unchanged official registry binary. The candidate source is
`8905cbb82d4bad011eddae5e83228f52906c4b40`.

Each variant has a new PostgreSQL database, Redis instance and standard-mode app.
A fake API-key account is seeded only after verifying the database has no accounts.
Requests enter the real HTTP `/v1/responses` route, exercising authentication,
scheduling, application forwarding and response handling. Synthetic upstream
responses include unique request IDs and output text, which are validated for both
JSON and SSE. The controller independently reconciles upstream request counts and
detects duplicated requests. There is no connection to a real model service.

The workload is 20 requests per second per variant with four client threads,
4 KiB input, alternating JSON and SSE responses, 300 seconds of warmup followed by
7,200 seconds of measurement. Both variants use the same fake upstream timing.
Latency is end-to-end response completion latency, **not time to first token**.
RSS is read from the actual app process every approximately 30 seconds. The stable
RSS comparison uses the median over the latter half of the measured period.

This workload does **not** measure full-image OAuth/zstd, WebSocket, TLS fingerprint
parity, maximum throughput or real-account behavior. Separate earlier contract and
race tests cover their stated paths; this benchmark must not be used to invent
unobserved coverage or an account-risk estimate.

## Isolation and cancellation

All containers are resource limited, have `restart=no`, and use a new internal
Docker network with no published ports. Existing resource names cause a refusal.
All credentials, users, groups and accounts are synthetic. The only writable
SQL target is an explicitly created and tracked test database. No host or
production proxy, URL validation, certificate, firewall or database configuration
is changed. Private HTTP fixture access is confined to the fresh test containers.

A `STOP` file in the run output directory requests cancellation; the controller
checks it within about 30 seconds. Use that instead of terminating unrelated
Docker/WSL processes. Its `finally` block verifies run ownership labels and removes
only the seven owned containers and their network. A host crash or abrupt kill
can prevent cleanup; in that case inspect the exact run label/names before manual
cleanup. Never use global Docker prune or remove the pre-existing local stack.

## Invocation and evidence

Run on the authorized WSL Linux host as a user authorized to access its Docker
socket. Supply immutable image digests, a unique `sub2api-r1-b8-...` run ID, and a
new output directory. **This launches a new test; do not use it to resume soak01.**

```sh
python3 tools/codex-r1-load/run.py \
  --run-id sub2api-r1-b8-NEW-RUN \
  --baseline local/sub2api-r1-baseline@sha256:8af502b156cecdbb6f9469d604776c540ae501995094f9d8a50bb08f4bd42611 \
  --candidate local/sub2api-r1@sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85 \
  --output /path/to/a/new/output-directory --seconds 7200 --warmup 300 --rps 20
```

`status.json` and `memory.json` are updated during the run. Only `result.json`
contains a terminal verdict. The launch wrapper also writes a driver exit record.
A stale MCP job handle is not evidence of success or a reason to repeat the test.

After the **actual** soak01 run exits and cleanup finishes, independently verify:

```powershell
python tools/codex-r1-load/verify.py D:\Temp\sub2api-build\r1
```

The independent verifier does not start containers or alter git/production. It
rejects missing or partial results, a short smoke, wrong image/source/harness
identities, raw-sample discrepancies, insufficient duration/observations, errors,
duplicates, incomplete cleanup, P95 degradation above 10% for either response mode,
and stable RSS degradation above 20%. It also verifies the final source/archive,
Go logs and image export hashes. Only success writes
`b8-independent-acceptance.json`. This gate is fixed to this R1 candidate/run and
is not a generic performance waiver.

Unit fixtures in `test_verify.py` are explicitly synthetic tests of the verifier,
not substitutes for the real two-hour run. Run `python -B -m unittest discover
-s tools/codex-r1-load -p test_verify.py` to check the verifier without Docker.

## Final delivery (only after the run)

Finish `b8-verification.md` with `B8 status: PASSED` only after independent
verification succeeds. Create `final-summary.md`, commit and push the reports,
then record the fresh git readback in `b8-git-final.json`:
`head`, `remote_feature`, `remote_main`, and `main_unchanged`. Save a fresh CI query
as `b8-remote-ci-final.json`; no CI runs must be reported as unobserved, not green.

```powershell
python tools/codex-r1-load/package.py D:\Temp\sub2api-build\r1 E:\Apps\sub2api-worktrees\codex-gap-v1 D:\Temp\sub2api-build\r1\sub2api-r1-8905cbb82d4b-acceptance.zip
```

The allowlisted package contains the source archive, exact candidate Docker tar,
raw tests and load observations, final reports and a per-file SHA-256 manifest.
It refuses an unfinished result, a dirty branch, product-file changes after the
image source commit, unverified push, or an existing output ZIP. It has no git
write, deployment or network operations. The delivery documentation commit and
the image source commit are recorded separately rather than relabeling a binary.
