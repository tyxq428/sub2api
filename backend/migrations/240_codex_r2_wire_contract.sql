-- R2.2 wire-contract selection is correctness state, not telemetry.
-- Existing R2 bindings intentionally have no row and continue using the
-- already deployed R2 contract. Only newly admitted R2.2 bindings are added.
CREATE TABLE IF NOT EXISTS codex_r2_wire_contract_bindings (
    binding_id        BIGINT PRIMARY KEY
                      REFERENCES codex_r2_policy_bindings(id) ON DELETE CASCADE,
    contract_id       VARCHAR(128) NOT NULL,
    contract_sha256   CHAR(64) NOT NULL,
    reference_commit  CHAR(40) NOT NULL,
    graph_revision    VARCHAR(128) NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT codex_r2_wire_contract_id_chk CHECK (length(trim(contract_id)) > 0),
    CONSTRAINT codex_r2_wire_contract_sha_chk CHECK (contract_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT codex_r2_wire_reference_commit_chk CHECK (reference_commit ~ '^[0-9a-f]{40}$'),
    CONSTRAINT codex_r2_wire_graph_revision_chk CHECK (length(trim(graph_revision)) > 0)
);

CREATE INDEX IF NOT EXISTS idx_codex_r2_wire_contract_id
    ON codex_r2_wire_contract_bindings (contract_id, updated_at DESC);
