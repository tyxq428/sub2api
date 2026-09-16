# R1 B7 — request encoding and transport decision

Parent source: `ecef7055923b...`.

## Confirmed compression gap

The locked Codex 0.154.0 reference source enables zstd request compression for the Responses HTTP path when request compression is enabled, authentication uses the Codex/ChatGPT backend, and the provider is OpenAI. The reference container reports `enable_request_compression stable true`. The tagged implementation uses zstd level 3 and `Content-Encoding: zstd`.

Before this batch Sub2API had no OpenAI request zstd encoder or outbound `Content-Encoding` write. The repository already depended on `github.com/klauspost/compress`; its `Encoder.EncodeAll` is documented concurrency-safe.

## Minimal implementation

`openai_request_compression.go` adds one shared level-3 zstd encoder and applies it only when:

- account platform is OpenAI;
- the account uses the Codex backend protocol (OAuth/setup-token/PAT/Agent Identity family);
- the request is the base HTTP `/responses` path.

The helper runs only after the final request body for that path has been decided. It replaces `req.Body`, sets an exact compressed `ContentLength`, a replayable `GetBody`, and `Content-Encoding: zstd`.

Explicit `/responses/compact` and `/responses/input_tokens` subpaths remain uncompressed because this batch has no locked-reference evidence for extending the rule to them. WebSocket, models, quota, privacy, search, live and image paths are untouched. OpenAI API-key and non-OpenAI OAuth accounts remain uncompressed.

For passthrough requests that require no body mutation, decompression reproduces the original request bytes exactly; JSON is not parsed/remarshaled merely to add compression. Native compaction v2 uses the base `/responses` endpoint, so it follows the same base Responses HTTP rule.

## Red/green evidence

Red characterization: `job_osLUKUu0REt462pT7SO8` actually ran all three initial contracts. Codex-backend passthrough and reconstructed Responses failed because no compression was present; API-key/other-provider exclusion passed.

After implementation:

- `job_4XSq_VL4X6DjXzFXWZ1d`: both Codex-backend HTTP builders compress successfully; API-key/other provider remain uncompressed; passthrough decompresses byte-for-byte to the original body.
- a fourth contract locks `/compact` and `/input_tokens` exclusion.
- the first broad builder run exposed eight old tests that treated compressed wire bytes as JSON; this was a test-fixture mismatch, not a product failure. The shared upstream recorder now keeps the actual compressed `req.Body` while decoding zstd only for existing semantic `lastBody/bodies` assertions.
- `job_yvVfz3Iwv2XDzc4hp6kp`: 181/181 top-level + 122 subtests passed, zero failures/skips after that fixture migration.
- `job_lw06RMJpdyp-zEq6G65b`: independent validation of the final required-test JSON log proves 67/67 required tests actually ran and passed; 82 top-level +163 subtests; zero failed/skipped-required.

## HTTP/2 and WebSocket transport decision

No new H2/WS/TLS transport code is added in B7 because the candidate already implements the planned controls:

- `gateway.openai_http2.enabled` defaults true;
- the OpenAI profile explicitly configures HTTP/2 and active connection-health PING (`ReadIdleTimeout` / `PingTimeout`);
- a proxy compatibility state machine can temporarily use H1 only after qualifying H2 proxy errors; ordinary header timeout does not activate fallback;
- the OpenAI profile has its own response-header-timeout behavior;
- WebSocket has a dedicated reader loop, idle peer-close handling, background Ping sweeps, slow-Pong tolerance, cached-connection eviction/reuse and real idle server-Ping/Pong tests;
- websocket compression/context takeover is already exercised throughout the WS test fixtures.

Transport evidence:

- `job_T-Q660U7k2FAvKtk4gFX`: the Go test process itself exited 0 with 8/8 selected H2/WS contracts passing. The outer wrapper returned 1 only because it incorrectly demanded at least 10 selected tests; this wrapper status is not used as completion evidence.
- `job_XjpU32ni9wpRNCjMPH7v`: full `TestHTTPUpstreamSuite`, 25/25 passed, including OpenAI default-H2, explicit H1 mode, H2 proxy-compat fallback, timeout-not-fallback and TLS-fingerprint timeout isolation.

## Residuals deliberately not modified

A fresh successful official A sample was unavailable in B4, so exact ALPN/H2 SETTINGS/TLS/JA3 equivalence is not claimed. Historical captures showed multiple official TLS families, so no global JA3 or one-size TLS profile is introduced. Optional TLS-fingerprint support remains opt-in; no browser cookies, attestation, geolocation, terminal metadata or side requests are fabricated.

B7 therefore closes the measured zstd gap, preserves no-mutation body bytes, and records H2/WS/TLS residuals instead of making speculative transport changes.
