# R1 B9 VPS staging / pre-production verification

Status at this checkpoint: **NO-GO for production cutover**.

This document extends the completed B8 acceptance with evidence from the VPS
staging environment. It does not change the frozen product candidate and does
not authorize a production rollout.

## Frozen candidate and delivery state

- Product source: `8905cbb82d4bad011eddae5e83228f52906c4b40`.
- Candidate image ID: `sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85`.
- Candidate Docker export SHA-256: `932b09eaf4359a1dc651b2367fabdecfbb116e29fed94407a48d6097ab2ff28f`.
- B8 acceptance ZIP SHA-256 was freshly revalidated after project-scoped
  access was restored: `b19af9b13990c43b3cff0c85d44de47a2cabd2732576fc966546ce997929e917`.
- The product image still comes from `8905cbb82d4b...`; later feature commits
  contain staging support, CI plumbing, documentation, or test-only cleanup.
- The latest code/support HEAD before this document refresh is
  `7e1d7ed03437ef40cccc80da6d14ce7f8dd5d1c8`.
- Remote `main` was freshly observed as
  `30ed40a56a5f4b5ab7b8dd3d685353db3a531c84`.
- GitHub Actions is now observed. `workflow_dispatch` was added without
  removing the existing `push` or `pull_request` triggers. Two initial manual
  runs exposed R1 test-only `errcheck` cleanup issues. After fixing those
  cleanup sites without changing product behavior, run `35196231446` on
  `7e1d7ed03437ef40cccc80da6d14ce7f8dd5d1c8` completed **success**: `frontend`,
  `test`, `golangci-lint`, and `shell` all passed; the `test` job completed both
  unit and integration tests successfully.

## Staging support changes observed on VPS

The synthetic fake upstream was moved into a pinned Compose service using
`mitmproxy/mitmproxy@sha256:00b77b5d8804c8ad18cb6caefbf9d5849e895e8986c5ce011f4ae30f4385962f`.
It has no published ports, runs non-root with a read-only root filesystem,
uses bounded resources, and has an HTTP health check and `unless-stopped`
restart policy. The fixture was cold-recreated once and returned healthy.
The staging app reached it by Docker DNS name.

The original temporary fixed-IP socat listener was replaced with an enabled
persistent systemd relay bound to `127.0.0.1:18081`. The deployed relay was
restarted successfully and health/static-asset checks plus three unauthenticated
negative checks remained good. Those checks created zero fake model requests.

The six core production/staging app, PostgreSQL, and Redis containers were
unchanged during the support qualification.

## Relay version qualification

Two relay revisions must remain distinct:

1. Runtime-observed revision SHA-256
   `4805ddb65111c7492a57d69f008df469c8b69d063a551f5f5b62bf1cf083ba00`
   passed 14 unit tests and an actual persistent-service restart.
2. Refined on-disk revision SHA-256
   `3fd23aec52e84cbb79cfc30bddf8aa56177ff4bfdcccc8036bbbcc5d17828ea1`
   narrows Docker inspect output to identity/network fields and adds two
   reconnect/no-replay unit tests. All 16 unit tests passed, but the final
   service reload/check/legacy-fixture cleanup operation was blocked by the
   execution safety layer and was not retried in another form.

The repository archives the refined source because it is the intended final
support implementation. Its unit-test result must not be presented as proof
that the VPS service loaded it.

## Product protocol evidence

Historical authenticated staging observations support HTTP JSON, SSE, and
client-zstd ingress. The previous compact checks support HTTP 200 plus the
expected summary and encrypted-content markers, but the response body was
deleted by the original test script. Its `OUTPUT_COUNT=0` value was a grep
pattern count rather than a parsed JSON array length. It cannot establish
either a product failure or complete opaque-structure preservation.

The following application end-to-end paths remain unobserved because the
execution safety layer rejected new authenticated staging requests and those
rejections were not bypassed:

- upstream-zstd JSON and SSE;
- successful WebSocket connection and subsequent turn;
- continuation/retry behavior through the authenticated app path;
- parsed compact opaque-structure preservation.

Fixture-only WS/zstd/retry success and unauthenticated 401 responses are not
substitutes for these product-path observations.

## Lifecycle / rollback evidence

Verified:

- pinned fake fixture cold recreation;
- persistent relay restart;
- internal DNS route from app to fake;
- staging health/static assets after those support changes.

Fresh database recovery verification is now complete. A protected production
custom-format dump was created at
`/opt/sub2api-deploy/backups/r1-b9-fresh-20260917T071304Z/production.dump` with
mode `0600` under a `0700` directory. It is 315,093,409 bytes, SHA-256
`0444079f558b26f288a4cfd93d1ff1485747587c873cf481c56b34df0aefbdc5`, and its
TOC contains 1,199 entries. The dump remained on the VPS and was not downloaded.

That dump was restored into a disposable PostgreSQL instance with
`network_mode=none`, no published ports, and a read-only backup bind mount.
`pg_restore --exit-on-error` returned 0. The final restored database contained
286 migration records, 435 indexes, 1,521 columns, 1,227 constraints, and 103
user tables. The migrations and index manifests matched the live production
database exactly. The disposable restore instance was stopped and auto-removed.

The archive was also rendered with `pg_restore --schema-only` and compared with
a contemporaneous production `pg_dump --schema-only`. After removing only
session/control boilerplate, the SQL was byte-for-byte equal with normalized
SHA-256 `adfc9e4188d98d8bc18f3402974f5986d69f1b05710d65b20a5c6680f320b08a`.
Both sides contain 103 tables, 72 sequences, 80 foreign keys, 99 primary keys,
17 unique constraints, 2 check constraints, 319 explicit indexes, and 72
defaults.

Two caveats are retained instead of hidden. First, raw catalog-text hashes for
column defaults and constraints differ after restore even though the normalized
schema SQL is equal; catalog-rendering identity is therefore not used as a
recovery gate. Second, production remained live during the dump/verification:
at the end of the successful restore, live production had 32 more rows than the
snapshot across five tables. Live row-count equality is not a valid gate for an
online logical backup.

Not yet verified:

- complete staging application cold recreation;
- candidate -> previous-image -> candidate application round trip;
- host reboot qualification;
- loading the refined relay revision.

## Production boundary

This B9 work did not merge `main`, switch the production image, modify the
production Nginx/PostgreSQL/Redis/proxy configuration, copy production account
credentials, or send a real model request. Account 8 remained unused. The only
authorized production-data operation in this continuation was the fresh
read-only logical backup described above; the restore target was isolated and
disposable.

Production cutover remains separately gated by explicit authorization after
the unresolved B9 items are either completed or explicitly accepted as
release residual risks.

## Durable evidence references

- B8 acceptance revalidation: child task job `job_ViimAvBAIRv_DoviWCyM`.
- Remote refs / CI: child task job `job_-Fz5RZbvvZWHOLobFr9x`.
- VPS support source readback: child task job `job_QtPeJVpeSQZxo_yRz8M6`.
- Parent-task support deployment: `job_nqC78PSSnHo4Hcf24fs_`.
- Parent-task fake deployment: `job_pnAoitHoY1GD_DguoajF`.
- Parent-task lifecycle qualification: `job_OGvZcmEZiSbuwCOgFfrV`.
- Parent-task refined relay unit tests: `job__t-eVZ6JAEFVbFlvvG7Z`.
- Fresh production backup: continuation job `job_vHHlq4z4ZH5lMZprnhM_`.
- Complete isolated restore run: continuation job `job_0-5YUnhUPIaU-UKmm4jO`.
- Archive schema-only equivalence: continuation job `job_IdEWfLBb5woZbhZqqI5k`.
- Recovery-gate assessment: continuation job `job_eEEN28P3u_t1kN8Fr_jF`.
- First CI run (lint findings): `35194968223`.
- Second CI run (one remaining lint finding): `35195697013`.
- Successful CI run on `7e1d7ed...`: `35196231446`, durable poll job
  `job_tWGqHmG-D_PFU3Su6eQ6`.
- Parent task: `task_Wp1FXYEsm1bgOYAX9YEI`.
- Continuation task: `task_3sTpTF1TvzLkC2BK0-vN`.
