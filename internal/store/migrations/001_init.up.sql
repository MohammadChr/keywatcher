CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE asset_type AS ENUM ('certificate','token','api_key','secret','custom');

CREATE TABLE assets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    type        asset_type NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tags        JSONB NOT NULL DEFAULT '{}',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  TEXT NOT NULL,
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_assets_expires_at  ON assets(expires_at) WHERE deleted_at IS NULL;
CREATE INDEX idx_assets_type        ON assets(type)       WHERE deleted_at IS NULL;
CREATE INDEX idx_assets_deleted_at  ON assets(deleted_at);
CREATE INDEX idx_assets_tags        ON assets USING GIN(tags);

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT UNIQUE NOT NULL,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT,
    auth_method   TEXT NOT NULL CHECK (auth_method IN ('local','oidc','ldap')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login    TIMESTAMPTZ
);

CREATE TABLE notification_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id   UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    days_left  INT NOT NULL,
    sent_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    channel    TEXT NOT NULL
);
CREATE INDEX idx_notif_asset_sent ON notification_log(asset_id, sent_at DESC);
