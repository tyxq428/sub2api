# R1 B4/B5 verification — reverse proxy facts and application identity contract

Source before this batch: `93378ce4fd30b90fbfb08ea511ade0b8319db445`.

## B4 reverse-proxy evidence

Production was read-only inspected; no Nginx file was changed. The active Sub2API server uses Ubuntu 24.04.3 and `nginx=1.24.0-2ubuntu7.18`. Its `/` location proxies to `127.0.0.1:8081`, uses HTTP/1.1, forwards Upgrade, forces `Connection: upgrade`, and sets 600s read/send timeouts. No explicit `underscores_in_headers` or `proxy_buffering off` was observed.

An isolated local image installed the exact same nginx Debian package version and used the relevant production proxy directives in front of a synthetic echo/SSE upstream. Successful probe job: `job_c3WaU5tEtZQqu0Wcvdzv`.

Observed behavior:

- hyphen header `session-id` survived end to end; underscore `session_id` was absent at the fake upstream;
- synthetic underscore header was dropped while its hyphenated twin survived;
- two repeated `X-Multi` values survived;
- `Upgrade: websocket` and `Connection: upgrade` survived;
- two SSE events emitted about two seconds apart became visible to the client together at about 2.0006s, consistent with proxy buffering under this configuration.

This proves the reverse-proxy behavior for the reproduced HTTP path. It does not authorize a production Nginx change. The application therefore avoids depending on underscore-only ingress identity fields; fixing SSE buffering in production remains a B9/production-config decision.

## B4 live reference limit

The live budget is durable at `D:\Temp\sub2api-build\r1\live-budget.json`. Current reservations: baseline=7/12, candidate=0/12, total=7/24.

A1–A5 were already consumed by earlier interrupted runs. A5 was classified as model-capacity, not auth or 429. A6 (`gpt-5.6-sol`) and A7 (`gpt-5.5`) both returned nonzero after possible send; fixed redacted keyword checks found network-class indicators and no capacity/auth/429/OK. Raw A6/A7 temporary files were removed after storing size/hash metadata. The reference remains Codex 0.154.0 and logged in, on the same Docker network with proxy env `http://172.21.0.1:8083`; host port 8083 is listening.

Per R1 stop rules, baseline live sampling stops here rather than consuming the remaining five baseline slots. There is no successful fresh A sample, so **fresh A/B application or transport equivalence is not claimed**. Historical captures remain background evidence only.

## B5 red characterization

`b5-red.jsonl` / SHA-256 `db4029b50f795d9161329930b7f05816f701e4e308102810a4e9f8428f55132b` actually ran all three service contracts. Before runtime changes:

- configured exec/tui/vscode surface pairing passed;
- hyphenated session/conversation ingress failed to drive passthrough session isolation;
- default fallback failed the locked 0.154.0 / Ubuntu 24.04.3 reference contract (runtime was 0.146.0 / Ubuntu 22.4.0).

## B5 minimal implementation

1. Default fallback version changes `0.146.0 → 0.154.0` and Linux suffix `Ubuntu 22.4.0 → Ubuntu 24.04.3`. Default originator is not changed and accounts are not globally forced to `codex_exec`.
2. Normal and passthrough allowlists accept standard hyphen aliases `session-id` and `conversation-id` in addition to legacy underscore forms.
3. OAuth passthrough prefers the hyphen ingress aliases, removes the raw aliases before account scoping, rebuilds the canonical isolated underscore values, then projects the same scoped values back to hyphen aliases. This prevents raw/scoped contradictions and double-scoping.
4. Existing `credentials.user_agent` remains the surface selector. exec/tui/vscode candidates preserve their surface and environment suffix while version is normalized through the canonical resolver. No proxy geo, host OS, browser cookie or attestation is invented.

## B5 verification

Targeted identity/account/fingerprint/compact regressions: `55 primary + 66 subtests passed, no failures/skips` (`job_M32kSHslQdHD0qwjylNt`).

True `OpenAIGatewayHandler.Responses → service → fake HTTPUpstream` wire contract for hyphen aliases passed (`job_tRs0sJg0DgqbSRBcXdm7`). It uses a synthetic API-key account and proves the real handler path preserves Nginx-safe hyphen aliases; the OAuth namespace transformation is separately covered at the service request-builder boundary.

Expanded checked-in gate after adding B5 contracts: **48/48 required tests executed and passed; 63 primary + 153 subtests; zero failures; zero skipped-required** (`job_r77U6r1Ri9-kQV43_Dva`).

## Remaining limits

B5 does not prove fresh official-client live equivalence because B4 live A sampling did not produce a successful sample. TLS/H2/WS family equivalence, zstd conditions, request-body byte preservation, lifecycle/replay, and production SSE buffering remain separate B6/B7/B9 decisions. Production account 8 and production Nginx are unchanged.
