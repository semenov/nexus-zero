CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
    identity_key   TEXT PRIMARY KEY,
    encryption_key TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS messages (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_key           TEXT NOT NULL,
    recipient_key        TEXT NOT NULL REFERENCES users(identity_key) ON DELETE CASCADE,
    ephemeral_key        TEXT NOT NULL,
    ciphertext           TEXT NOT NULL,
    sender_ephemeral_key TEXT,
    sender_ciphertext    TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at         TIMESTAMPTZ
);

-- Migration: add sender-copy columns if they don't exist yet.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS sender_ephemeral_key TEXT;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS sender_ciphertext TEXT;

CREATE INDEX IF NOT EXISTS idx_messages_recipient_pending
    ON messages(recipient_key, created_at)
    WHERE delivered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_messages_history
    ON messages(recipient_key, created_at);

CREATE INDEX IF NOT EXISTS idx_messages_sender_history
    ON messages(sender_key, created_at)
    WHERE sender_ciphertext IS NOT NULL;

CREATE TABLE IF NOT EXISTS contacts (
    owner_key   TEXT NOT NULL REFERENCES users(identity_key) ON DELETE CASCADE,
    contact_key TEXT NOT NULL,
    nickname    TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (owner_key, contact_key)
);

CREATE TABLE IF NOT EXISTS device_tokens (
    identity_key TEXT NOT NULL REFERENCES users(identity_key) ON DELETE CASCADE,
    token        TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (identity_key, token)
);
