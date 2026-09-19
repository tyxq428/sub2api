-- R2 Codex semantic-identity correctness state and bounded local diagnostics.
-- No raw client identifiers, prompts, credentials, or full user-agent values are stored.

CREATE TABLE IF NOT EXISTS codex_r2_policy_bindings (
    id                  BIGSERIAL PRIMARY KEY,
    account_id          BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    auth_scope_digest   VARCHAR(64) NOT NULL,
    session_digest      VARCHAR(64) NOT NULL,
    profile_revision    VARCHAR(128) NOT NULL,
    policy_revision     VARCHAR(64) NOT NULL,
    mapping_algorithm   VARCHAR(64) NOT NULL,
    mapping_key_epoch   VARCHAR(64) NOT NULL,
    namespace_digest    VARCHAR(64) NOT NULL,
    ua_policy           VARCHAR(64) NOT NULL,
    status              VARCHAR(16) NOT NULL DEFAULT 'active',
    version             BIGINT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT codex_r2_policy_bindings_status_chk
        CHECK (status IN ('active', 'draining', 'retired')),
    CONSTRAINT codex_r2_policy_bindings_version_chk CHECK (version > 0),
    CONSTRAINT codex_r2_policy_bindings_unique_scope
        UNIQUE (account_id, auth_scope_digest, session_digest)
);

CREATE INDEX IF NOT EXISTS idx_codex_r2_policy_bindings_account_status
    ON codex_r2_policy_bindings (account_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS codex_r2_lineage_anchors (
    id              BIGSERIAL PRIMARY KEY,
    binding_id      BIGINT NOT NULL REFERENCES codex_r2_policy_bindings(id) ON DELETE CASCADE,
    entity_kind     VARCHAR(16) NOT NULL,
    entity_digest   VARCHAR(64) NOT NULL,
    relation        VARCHAR(24) NOT NULL,
    target_digest   VARCHAR(64) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT codex_r2_lineage_entity_kind_chk
        CHECK (entity_kind IN ('thread', 'turn')),
    CONSTRAINT codex_r2_lineage_relation_chk
        CHECK (relation IN ('parent', 'forked_from', 'root')),
    CONSTRAINT codex_r2_lineage_unique_anchor
        UNIQUE (binding_id, entity_kind, entity_digest, relation)
);

CREATE INDEX IF NOT EXISTS idx_codex_r2_lineage_binding
    ON codex_r2_lineage_anchors (binding_id, entity_kind);

CREATE TABLE IF NOT EXISTS codex_r2_shadow_entities (
    bucket_date     DATE NOT NULL,
    account_id      BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    stage           VARCHAR(24) NOT NULL,
    identity_class  VARCHAR(24) NOT NULL,
    identity_digest VARCHAR(64) NOT NULL,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    seen_count      BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (bucket_date, account_id, stage, identity_class, identity_digest),
    CONSTRAINT codex_r2_shadow_entities_count_chk CHECK (seen_count > 0)
);

CREATE INDEX IF NOT EXISTS idx_codex_r2_shadow_entities_account_date
    ON codex_r2_shadow_entities (account_id, bucket_date DESC);

CREATE TABLE IF NOT EXISTS codex_r2_shadow_edges (
    bucket_date     DATE NOT NULL,
    account_id      BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    relation        VARCHAR(32) NOT NULL,
    from_class      VARCHAR(24) NOT NULL,
    from_digest     VARCHAR(64) NOT NULL,
    to_class        VARCHAR(24) NOT NULL,
    to_digest       VARCHAR(64) NOT NULL,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    seen_count      BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (
        bucket_date, account_id, relation, from_class, from_digest, to_class, to_digest
    ),
    CONSTRAINT codex_r2_shadow_edges_count_chk CHECK (seen_count > 0)
);

CREATE INDEX IF NOT EXISTS idx_codex_r2_shadow_edges_account_date
    ON codex_r2_shadow_edges (account_id, bucket_date DESC);

CREATE TABLE IF NOT EXISTS codex_r2_shadow_daily (
    bucket_date         DATE NOT NULL,
    account_id          BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    stage               VARCHAR(24) NOT NULL,
    purpose             VARCHAR(32) NOT NULL,
    profile             VARCHAR(128) NOT NULL DEFAULT '',
    result              VARCHAR(64) NOT NULL DEFAULT '',
    total_events        BIGINT NOT NULL DEFAULT 0,
    complete_events     BIGINT NOT NULL DEFAULT 0,
    incomplete_events   BIGINT NOT NULL DEFAULT 0,
    conflict_events     BIGINT NOT NULL DEFAULT 0,
    unknown_events      BIGINT NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_date, account_id, stage, purpose, profile, result),
    CONSTRAINT codex_r2_shadow_daily_nonnegative_chk
        CHECK (
            total_events >= 0 AND complete_events >= 0 AND incomplete_events >= 0
            AND conflict_events >= 0 AND unknown_events >= 0
        )
);

CREATE INDEX IF NOT EXISTS idx_codex_r2_shadow_daily_account_date
    ON codex_r2_shadow_daily (account_id, bucket_date DESC);
