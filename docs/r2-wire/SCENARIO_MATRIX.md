# R2.2 Official Wire Conformance scenario matrix

Baseline: 6afffd9a27864fd6b81a05efe832f97d2e8e2362.

This matrix is a verification contract, not a statement that every row has
already been independently observed against a real OpenAI endpoint. R2.2 uses
synthetic credentials and fake upstreams only.

| Scenario | HTTP | HTTP-to-WS | Direct WS | Contract behavior |
|---|---:|---:|---:|---|
| root inference | required | required | required | versioned profile; default cache relationship only when pinned evidence allows it |
| explicit cache override | required | required | required | prompt cache remains an independent domain |
| child/subagent | required | required | required | session may remain stable while child thread differs; do not fabricate subagent metadata |
| fork/parent lineage | required | required | required | parent/fork references use thread domain; unknown parent stays unknown |
| compaction | required | required | required | purpose-specific envelope; opaque encrypted content untouched |
| memory/background | N/A unless authentic context exists | N/A unless authentic context exists | scenario-specific | no foreground identity is invented for server-background work |
| same-turn retry | required | required | required | server turn-state may echo only for the same explicit turn |
| next turn | required | required | required | previous-turn opaque turn-state is not replayed merely because session/account match |
| reconnect/prewarm | N/A for HTTP | required | required | compatibility digest pins policy/profile/identity/contract; no stale incompatible prewarm reuse |
| unknown client version | required | required | required | fail closed for R2.2 enforce; shadow/legacy remains explicit |
| malformed canonical metadata | required | required | required | no partial rewrite; enforce rejects before send |
| compressed inbound body | required | applicable bridge | N/A | decode is bounded and N+1 is an explicit size error |
| compressed outbound body | required | applicable bridge | N/A | final decoded wire body must byte-equal projected semantic body |

## Authentication evidence

The production-target family is OpenAI OAuth. A client-facing Sub2API API key
is only the local authenticated scope and must not be mistaken for an upstream
OpenAI API-key contract.

## Deliberate non-goals

- no TLS/JA3 or HTTP/2 fingerprint impersonation;
- no fabricated attestation, analytics, cookies, geo, timezone, workspace or
  sandbox values;
- no Group Rebind or scheduler redesign;
- no production shadow/enforce activation in this task;
- no request to a real model endpoint.
