# R2 E5 Acceptance

Status: **PASSED**

## Validated source and images

- Product source commit: `a1d287191d7a3c0a83f27bac3691a109d59d17bb`
- Baseline source: `434112e816951080f3e9619f30d5f5bc1165c8a0`
- Baseline image: `sha256:cfe0fd71df69e26730c12509ec686b8ad580850c3821e7627db0595bc4b52627`
- Candidate image: `sha256:6a6fc0dd9163b3d36ec04256308b05f9785639ef6e281348e0da8c5360efe700`
- The candidate binary reports commit `a1d287191d7a3c0a83f27bac3691a109d59d17bb`.

The final candidate includes the lint-only fix that explicitly ignores the
best-effort `sql.Rows.Close` return. The earlier `7895fc2d4` candidate and
interrupted soak01 are superseded and are not acceptance evidence.

## Exact-head gates

- R2-targeted backend tests passed.
- Full repository compile-only test pass completed.
- `golangci-lint`: 0 issues.
- `govulncheck`: 0 reachable vulnerabilities.
- `BenchmarkBuildPlan`: approximately 63 microseconds/op, below the frozen
  1 millisecond planner budget.
- Candidate Docker image built successfully with the exact product source
  commit embedded.
- Isolated migration/lifecycle/rollback rehearsal passed: the candidate
  created `codex_r2_policy_bindings`, rollback to the 434 baseline remained
  healthy, the migrated R2 table was preserved, and owned resources were
  removed.

## Final soak02

Run: `sub2api-r2-e5-soak02`

- Warmup: 300 seconds
- Measured duration: 7200 seconds
- Rate: 20 requests/second/version
- Actual elapsed time: 7499.985 seconds
- Baseline requests: 150,000; errors: 0
- Candidate requests: 150,000; errors: 0
- Upstream requests: 300,020
- Upstream duplicates: 0
- Invalid upstream inputs: 0
- Measured latency samples per version: 144,000

| Metric | Baseline | Candidate | Candidate change |
| --- | ---: | ---: | ---: |
| stream P95 | 56.604947 ms | 56.544953 ms | -0.10599% |
| non-stream P95 | 39.842522 ms | 39.803354 ms | -0.09831% |
| stable RSS median | 174,288,896 B | 174,891,008 B | +602,112 B |

All frozen gates passed, including P95 regression <= +5%, added stable RSS
<= 64 MiB, complete sample count, zero request errors, zero duplicate/invalid
upstream requests, bounded tracker memory, and complete owned-resource cleanup.

The independent verifier re-read the raw latency and memory artifacts and
recomputed the same passing result.

## Scope limitation

The full-image load workload is intentionally API-key HTTP/SSE to a synthetic
fake upstream. It does not itself activate OAuth-only R2 enforcement. Active
R2 HTTP/SSE/compact/HTTP-to-WS/direct-WS semantic paths are covered separately
by the E4 authenticated synthetic protocol tests. No real model request,
production credential, production database, production Redis, production
proxy, or production deployment was used by E5.
