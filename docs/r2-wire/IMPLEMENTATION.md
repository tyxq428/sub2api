# R2.2 Official Wire Conformance implementation record

## Safety boundary

R2.2 is independently gated by gateway.codex_r2.wire_contract_mode, whose
default is off. Existing R2 bindings remain on policy revision r2-v1.
Only a newly admitted R2.2 session may use r2.2-wire-v1, and its immutable
wire contract is persisted transactionally in
codex_r2_wire_contract_bindings.

Changing this repository does not authorize production activation.

## Main changes

1. internal/pkg/codexwire
   - pinned contract registry;
   - typed canonical metadata parser;
   - unknown-field preservation;
   - relationship-preserving differential normalizer;
   - deterministic reference-manifest diff.
2. internal/pkg/codexidentity
   - prompt-cache/session coupling is now an explicit profile policy instead of
     an implicit universal rule.
3. internal/pkg/httputil
   - compressed inbound request limits read one byte beyond the budget and
     return http.MaxBytesError rather than accepting a truncated N-byte prefix.
4. R2 runtime
   - wire contract selection is read-only in shadow;
   - new enforce admissions persist the contract beside the policy binding;
   - existing r2-v1 sessions cannot be silently upgraded;
   - compatibility selectors gate R2/R2.2 eligibility rather than general
     request admission: an otherwise normal unpinned Codex version records
     unknown-profile telemetry and falls back to the pre-R2.2 path instead of
     receiving a generic 502;
   - deterministic conflicts after a known profile is selected still fail
     closed and are attributed to the local compatibility policy, not to the
     upstream provider;
   - the WS compatibility digest includes the persisted contract for R2.2
     bindings.
5. Final HTTP envelope
   - after existing Zstd compression, the final body is decoded locally and
     checked against the projected semantic bytes;
   - transport_ready is separate from the earlier prepared observation.
6. Turn-state lifecycle
   - provenance also records an explicit turn id when present;
   - a previous-turn opaque state is stripped on a later explicit turn even
     when the session and account remain unchanged;
   - missing turn identity does not cause the gateway to invent one.

## Differential tool

The codex-wire-verify command supports:

- mode contracts
- mode diff-fields with before/after JSON
- mode diff-manifest with before/after JSON

The tool has no production write path and does not auto-update profile
selection.

## Evidence status

- 0.154.0 pinned file hashes are inherited from the immutable original R2
  manifest.
- 0.155.1 is pinned to commit
  be2951ea34f0d295ed0becf97079f92fa5f6950e and its fixed-source files have
  been refreshed with both Git blob ids and SHA-256 digests.
- That refresh exposed two version-specific behaviors that are now part of the
  conformance gate rather than guessed from 0.154:
  - 0.155.1 remote compaction v2 uses the normal streaming Responses path with
    a compaction trigger/request_kind instead of the removed unary
    /responses/compact client path;
  - cached WebSocket/turn routing state is invalidated when the authenticated
    ChatGPT owner changes. R2.2 0.155.1 enforce therefore requires both
    workspace/account id and ChatGPT user id to be known.
- Existing r2-v1 sessions are still never reinterpreted by these R2.2 gates.
