# R2 endpoint and writer inventory

Baseline: 434112e. This is the required inventory, not a coverage pass.

| Purpose / ingress | Integration owner | First-release rule | Required evidence |
|---|---|---|---|
| Native OAuth HTTP Responses | openai_gateway_forward.go; openai_gateway_passthrough.go | Explicit supported profile + durable admission, one identity writer | Authenticated JSON/SSE, OFF/shadow equivalence, tool-loop/lineage |
| HTTP-to-WS | openai_ws_forwarder_v2.go; openai_ws_forwarder_payload.go | Same plan and scoped connection descriptor | Cross-client reuse rejection, later turn reuse, prewarm |
| Direct WS | openai_ws_forwarder_ingress.go; openai_ws_v2_passthrough_adapter.go | Each response.create/session.update has its own raw snapshot | First/later message, cancel, disconnect and no replay |
| Compact | native HTTP and WS compact adapters | Purpose-specific projection; opaque untouched | JSON structure and opaque byte preservation |
| Models / manifest | openai_codex_models_service.go | Carry a real request profile only when present; otherwise explicit background | No fabricated installation/session |
| Quota / usage / privacy | respective existing clients | Explicit background or authentic caller context | No latest-client UA cache, existing route guard retained |
| Alpha search / image / account test | native service adapters | Purpose profile or documented unsupported/native N/A | No blanket inference headers on other purposes |
| OAuth refresh / token exchange | credential plane context helpers | Preserve existing credential ownership and background policy | No arbitrary caller identity on scheduled refresh |
| Memory / guardian / subagent | execution lane and metadata adapters | Preserve existing distinct execution lanes and parent references | Background task does not cancel foreground turn |
| API-key native reference | fixed official source fixtures | Reference evidence only until profile independently verified | Do not extrapolate API-key test to OAuth |
| ChatCompletions / Messages bridges | existing bridge adapters | Legacy; coverage records unsupported/native N/A | No fabricated Codex provenance |
| Setup-token / Agent Identity | existing credential adapters | Legacy until independent scoped profile evidence | No bearer-based stable OAuth identity or permission widening |

HTTP header/body identity projection and WS handshake/message projection are distinct. R2 cannot reuse canonical Ubuntu identity enforcement after preserving a caller UA, and cannot run both the legacy account namespace writer and v2 writer on the same attempt.
