# Codex R1 VPS staging support

This directory archives the non-secret staging support configuration used by
the R1 pre-production qualification. It is not part of the Sub2API product
runtime image.

The frozen candidate application remains source commit
`8905cbb82d4bad011eddae5e83228f52906c4b40` and image ID
`sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85`.

The VPS deployment uses an internal Docker network, a pinned fake-upstream
image, and a loopback-only relay. `fixtures.compose.json` is intended to be
combined with the staging base Compose file. The relay checks the exact
container name, Compose project/service, image ID, running state, and single
internal network before forwarding bytes. It never reads application
credentials.

Qualification status is recorded in
`docs/codex-gap/r1/b9-staging-verification.md`. In particular, the refined
`staging_relay.py` in this directory passed 16 local unit tests on the VPS, but
at the time it was archived it had not yet been reloaded into the persistent
systemd service. Do not treat the repository copy itself as runtime proof.

No `.env`, client key, refresh token, production database content, or account
8 credential is stored here.

## B9 disposable lifecycle rehearsal

`rehearse_lifecycle.py` is the audited runner for the final lifecycle gate. It
defaults to plan-only mode and does not call Docker unless both `--execute` and
`--ack DISPOSABLE_ONLY` are supplied. The execution path creates only
`sub2api-r1-rehearsal-b9-*` resources on an internal Docker network with no
published host ports, generates synthetic secrets in-process, and validates
candidate startup, candidate cold recreation, previous-image startup, return to
candidate, persistence/config continuity, unchanged production/current-staging
core identities, and exact owned-resource cleanup.

The assistant-side lifecycle execution was safety-blocked before mutation, so
this script or its plan output is **not** lifecycle acceptance evidence by
itself. The gate closes only after a human executes the reviewed runner on the
VPS and its non-sensitive JSON result is independently read back with
`passed: true`.

Safe plan-only validation:

```sh
python3 tools/codex-r1-staging/rehearse_lifecycle.py --plan
```
