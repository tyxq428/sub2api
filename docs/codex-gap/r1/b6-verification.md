# R1 B6 — replay and continuation verification

Parent: `c41a86b1b7907baccd980ce0110d3c6220fe3f4e`.

## Contract

R1 distinguishes downstream write safety from upstream replay safety. `UpstreamFailoverError` now carries `RequestMayHaveBeenSent`. `ShouldRetryNextAccount()` refuses retry when that flag is true, which also blocks same-account retry because the failover loops check this gate first.

OpenAI transport errors are classified `DefinitelyPreSend` only for provable dial/DNS/proxy-connect failures. Ambiguous transport errors are maybe-sent. Post-response technical failures (first-output timeout, response-body read error, stream truncation/missing terminal/scanner/staging failures across native Responses, passthrough, messages, raw chat-completions and WS-to-HTTP bridge) are replay-unsafe even when the client received no semantic bytes.

Silent-refusal and empty `response.completed` anomalies are also replay-unsafe because the upstream attempt completed. Their existing error codes and ops records remain. Explicit semantic failures such as capacity, 429 and `response.failed` keep the existing business recovery policy.

The existing previous-response ownership, continuation, sticky-account, client-cancel and post-output failover tests remain in place. No broad outbound-account clone was added because this batch did not reproduce a credential/metadata hot-update corruption requiring that architecture; B3 final-dispatch route validation remains the egress boundary.

## Evidence

- Red characterization: `job_ABA5rUbfiCDuFSCAtvmS` — three replay-unsafe contracts failed; explicit business failover control passed.
- Core targeted rerun after migrating the old WS replay test: `job_iCpQxP6HL_w6c_itREX4` — 167/167 top-level + 150 subtests passed.
- Broad stream/replay run after post-response and silent/empty-completed migration: `job_ZlqwRkMI3JFz0i0RzLzV` — 81/81 top-level + 18 subtests passed.
- Required gate: `job_dZfqwRmCSspFXn5RfOrW` — 59/59 required, 74 top-level + 159 subtests, zero failures and zero skipped-required. Gate negative self-test passed.

The migrated WebSocket handler contract proves the first upstream had already received the complete request before the first-output timeout; the second account must receive zero replayed requests and the client receives a stable retry-later close instead.

## Boundary

This change prevents silent duplicate execution when upstream acceptance cannot be ruled out. It does not claim OpenAI-side idempotency, risk-score effects, or that every external provider shares this policy. Other providers are unchanged unless they explicitly set `RequestMayHaveBeenSent`.
