CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Users: identity is an Ed25519 public key; username is chosen by the user.
CREATE TABLE IF NOT EXISTS users (
    identity_key   TEXT PRIMARY KEY,
    encryption_key TEXT NOT NULL,
    username       TEXT UNIQUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Nexuses: group chats.
CREATE TABLE IF NOT EXISTS nexuses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    creator_key TEXT NOT NULL REFERENCES users(identity_key) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Nexus membership.
CREATE TABLE IF NOT EXISTS nexus_members (
    nexus_id     UUID NOT NULL REFERENCES nexuses(id) ON DELETE CASCADE,
    identity_key TEXT NOT NULL REFERENCES users(identity_key) ON DELETE CASCADE,
    role         TEXT NOT NULL DEFAULT 'member',
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (nexus_id, identity_key)
);

CREATE INDEX IF NOT EXISTS idx_nexus_members_user
    ON nexus_members(identity_key);

-- Kicked members: blocked from rejoining a nexus.
CREATE TABLE IF NOT EXISTS kicked_members (
    nexus_id     UUID NOT NULL REFERENCES nexuses(id) ON DELETE CASCADE,
    identity_key TEXT NOT NULL REFERENCES users(identity_key) ON DELETE CASCADE,
    kicked_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (nexus_id, identity_key)
);

-- Invite codes for joining a nexus.
CREATE TABLE IF NOT EXISTS invite_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nexus_id   UUID NOT NULL REFERENCES nexuses(id) ON DELETE CASCADE,
    code       TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users(identity_key) ON DELETE CASCADE,
    max_uses   INT,
    use_count  INT NOT NULL DEFAULT 0,
    revoked    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_invite_codes_code
    ON invite_codes(code);

-- Messages: one row per recipient (fan-out encryption).
CREATE TABLE IF NOT EXISTS messages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nexus_id      UUID NOT NULL REFERENCES nexuses(id) ON DELETE CASCADE,
    sender_key    TEXT NOT NULL REFERENCES users(identity_key),
    recipient_key TEXT NOT NULL REFERENCES users(identity_key),
    ephemeral_key TEXT NOT NULL,
    ciphertext    TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_messages_nexus_time
    ON messages(nexus_id, created_at);

CREATE INDEX IF NOT EXISTS idx_messages_recipient_pending
    ON messages(recipient_key, created_at)
    WHERE delivered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_messages_recipient_nexus
    ON messages(recipient_key, nexus_id, created_at);

-- Device tokens for APNs push notifications.
CREATE TABLE IF NOT EXISTS device_tokens (
    identity_key TEXT NOT NULL REFERENCES users(identity_key) ON DELETE CASCADE,
    token        TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (identity_key, token)
);
