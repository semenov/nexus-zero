# Messenger API Documentation

## Overview

Messenger is a private, end-to-end encrypted messaging service. The server never
has access to plaintext. All cryptographic operations happen exclusively on client
devices.

- **Transport:** HTTPS (HTTP for local development)
- **Encoding:** All binary data (public keys, ciphertexts, signatures) is encoded
  as **base64url without padding** (RFC 4648 §5, no `=` characters).
- **Timestamps:** ISO 8601 / RFC 3339 (`2024-01-15T10:30:00Z`).
- **Errors:** Every error response has the shape `{"error": "<human-readable message>"}`.
- **Base URL:** `http://localhost:8080` (development default).

---

## Identity Model

Every user has **two keypairs** generated on-device. No password or email is
required.

| Keypair | Algorithm | Purpose |
|---------|-----------|---------|
| `signingKey` | Ed25519 (Curve25519.Signing) | Authentication to server, identity |
| `agreementKey` | X25519 (Curve25519.KeyAgreement) | Message encryption |

The **identity key** is the base64url-encoded Ed25519 public key. It is used as
the primary identifier (`identity_key`) everywhere in the API.

The **encryption key** is the base64url-encoded X25519 public key. It is
published to the server so senders can encrypt messages to a recipient without
any prior interaction.

---

## Authentication Scheme

All endpoints marked **Auth required** must include an `Authorization` header.

### Header Format

```
Authorization: Ed25519 <pubkey>.<timestamp>.<signature>
```

| Field | Description |
|-------|-------------|
| `pubkey` | base64url-encoded Ed25519 public key (the caller's identity) |
| `timestamp` | Unix timestamp in whole seconds (decimal integer) |
| `signature` | base64url-encoded Ed25519 signature |

### Signed Message

The signature covers the following UTF-8 string:

```
<pubkey>.<timestamp>
```

(i.e., the first two dot-separated fields of the token, concatenated with a
literal `.`.)

### Validity Window

The server rejects tokens whose `timestamp` differs from server time by more than
**30 seconds** in either direction. Clients must keep their clocks roughly in
sync (NTP is sufficient).

### Example

```
Authorization: Ed25519 ABC123...xyz.1710000000.SIG456...abc
```

### WebSocket Authentication

WebSocket connections authenticate via the `auth` query parameter instead of a
header (browsers do not support custom headers during the WebSocket handshake):

```
GET /v1/ws?auth=<pubkey>.<timestamp>.<signature>
```

The value of `auth` is identical to the token portion of the `Authorization`
header — the `Ed25519 ` prefix is omitted.

---

## Encryption Scheme

Messages are encrypted with **ephemeral ECDH + HKDF + ChaChaPoly** (a variant
of the Noise `X` pattern).

### Per-Message Encryption Steps (Sender)

1. Fetch recipient's X25519 `encryption_key` from `GET /v1/users/{identity_key}`.
2. Generate a fresh ephemeral X25519 keypair (`ephemeral_priv`, `ephemeral_pub`).
3. Compute `shared_secret = ECDH(ephemeral_priv, recipient_encryption_pub)`.
4. Derive a 32-byte symmetric key:
   ```
   symmetric_key = HKDF-SHA256(
       ikm  = shared_secret,
       salt = "messenger-v1"  (UTF-8 bytes),
       info = "message"       (UTF-8 bytes),
       len  = 32
   )
   ```
5. Encrypt:
   ```
   sealed = ChaChaPoly.seal(plaintext_utf8, using: symmetric_key)
   ciphertext = base64url(sealed.combined)   // nonce (12 B) || ciphertext || tag (16 B)
   ```
6. Send to server: `{ sender_key, recipient_key, ephemeral_key, ciphertext }`.

### Decryption Steps (Recipient)

1. Receive `{ ephemeral_key, ciphertext }` from the server.
2. Compute `shared_secret = ECDH(local_agreement_priv, ephemeral_pub)`.
3. Derive `symmetric_key` with the same HKDF parameters.
4. Decrypt: `plaintext = ChaChaPoly.open(combined_ciphertext, using: symmetric_key)`.

**Forward secrecy:** Each message uses a freshly generated ephemeral keypair.
Compromise of the long-term `agreementKey` does not expose past messages (as
long as the attacker did not store the ciphertexts during transmission).

---

## Endpoints

### POST /v1/users

Registers a new user. No authentication required. Idempotent registration is
rejected with 409 if the identity key is already taken.

**Request body:**

```json
{
  "identity_key":   "ABC123...",
  "encryption_key": "XYZ789..."
}
```

| Field | Type | Description |
|-------|------|-------------|
| `identity_key` | string | base64url Ed25519 public key |
| `encryption_key` | string | base64url X25519 public key |

**Success response — 201 Created:**

```json
{
  "identity_key":   "ABC123...",
  "encryption_key": "XYZ789...",
  "created_at":     "2024-01-15T10:30:00Z"
}
```

**Error responses:**

| Status | Condition |
|--------|-----------|
| 400 | Missing or malformed fields |
| 409 | `identity_key` already registered |

---

### GET /v1/users/{identity_key}

Returns the public profile of a user. No authentication required. Used by
senders to obtain a recipient's `encryption_key` before composing a message.

**Path parameter:**

| Parameter | Description |
|-----------|-------------|
| `identity_key` | base64url Ed25519 public key of the target user |

**Success response — 200 OK:**

```json
{
  "identity_key":   "ABC123...",
  "encryption_key": "XYZ789...",
  "created_at":     "2024-01-15T10:30:00Z"
}
```

**Error responses:**

| Status | Condition |
|--------|-----------|
| 404 | User not found |

---

### POST /v1/messages

Sends an encrypted message to a recipient. **Auth required.**

The server stores the opaque ciphertext without inspecting it and attempts
real-time delivery to the recipient over WebSocket (if connected). If the
recipient is offline the message is queued for retrieval via
`GET /v1/messages/pending`.

**Request body:**

```json
{
  "recipient_key": "ABC123...",
  "ephemeral_key": "EPH456...",
  "ciphertext":    "BASE64URL_CIPHERTEXT"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `recipient_key` | string | Recipient's base64url Ed25519 identity key |
| `ephemeral_key` | string | base64url X25519 ephemeral public key (generated per message) |
| `ciphertext` | string | base64url combined ChaChaPoly output (nonce + ct + tag) |

**Success response — 201 Created:**

```json
{
  "id":         "550e8400-e29b-41d4-a716-446655440000",
  "created_at": "2024-01-15T10:30:00.123Z"
}
```

**Error responses:**

| Status | Condition |
|--------|-----------|
| 400 | Missing or malformed fields |
| 401 | Missing, expired, or invalid authorization token |
| 404 | Recipient not registered |

---

### GET /v1/messages/pending

Returns all messages queued for the authenticated user that have not yet been
delivered, then atomically marks them as delivered. **Auth required.**

Clients should call this endpoint on startup (to catch messages received while
offline) in addition to maintaining a WebSocket connection for real-time
delivery.

**Success response — 200 OK:**

```json
[
  {
    "id":            "550e8400-e29b-41d4-a716-446655440000",
    "sender_key":    "SENDER_IDENTITY_KEY",
    "recipient_key": "MY_IDENTITY_KEY",
    "ephemeral_key": "EPH456...",
    "ciphertext":    "BASE64URL_CIPHERTEXT",
    "created_at":    "2024-01-15T10:30:00Z"
  }
]
```

Returns an empty array `[]` when there are no pending messages.

**Error responses:**

| Status | Condition |
|--------|-----------|
| 401 | Missing, expired, or invalid authorization token |

---

## WebSocket Protocol

The WebSocket endpoint enables real-time push delivery of messages.

### Connection

```
GET /v1/ws?auth=<pubkey>.<timestamp>.<signature>
Upgrade: websocket
```

The `auth` query parameter uses the same token format as the `Authorization`
header, minus the `Ed25519 ` scheme prefix.

The server upgrades to WebSocket on success. On authentication failure it
returns HTTP 401 and does not upgrade.

### Server → Client Frames

The server pushes a message frame whenever a new message arrives for the
connected user:

```json
{
  "type":         "message",
  "id":           "550e8400-e29b-41d4-a716-446655440000",
  "sender_key":   "SENDER_IDENTITY_KEY",
  "ephemeral_key":"EPH456...",
  "ciphertext":   "BASE64URL_CIPHERTEXT",
  "created_at":   "2024-01-15T10:30:00Z"
}
```

### Client → Server Frames

After successfully decrypting and persisting a message the client sends an
acknowledgement:

```json
{
  "type": "ack",
  "id":   "550e8400-e29b-41d4-a716-446655440000"
}
```

The server marks the message as delivered upon receipt of the ACK.

### Keep-alive

The server sends a WebSocket `PING` frame every 30 seconds. Clients must reply
with a `PONG` (the standard WebSocket library handles this automatically). If
no `PONG` is received within 60 seconds of the last message/ping the server
closes the connection.

### Reconnection

Clients should implement exponential back-off reconnection (starting at 1 s,
doubling up to a maximum of 30 s) after unexpected disconnections.

---

## Error Response Format

All error responses use a consistent JSON envelope:

```json
{
  "error": "human-readable description of what went wrong"
}
```

### Common HTTP Status Codes

| Code | Meaning |
|------|---------|
| 200 | OK |
| 201 | Created |
| 400 | Bad Request — malformed JSON or missing required fields |
| 401 | Unauthorized — invalid, expired, or missing auth token |
| 404 | Not Found |
| 409 | Conflict — resource already exists |
| 500 | Internal Server Error |

---

## Data Types Reference

| Type | Format | Example |
|------|--------|---------|
| Identity key | base64url (no padding), 32 raw bytes → 43 chars | `ABC123DEF456...` |
| Encryption key | base64url (no padding), 32 raw bytes → 43 chars | `XYZ789GHI012...` |
| Ephemeral key | base64url (no padding), 32 raw bytes → 43 chars | `EPH456JKL789...` |
| Ciphertext | base64url (no padding), variable length | `nonce+ct+tag combined` |
| Message ID | UUID v4 string | `550e8400-e29b-41d4-a716-446655440000` |
| Timestamp | ISO 8601 UTC | `2024-01-15T10:30:00Z` |

---

## Example Flow

### 1. Alice registers

```http
POST /v1/users HTTP/1.1
Content-Type: application/json

{
  "identity_key":   "ALICEidentityPublicKey43chars00",
  "encryption_key": "ALICEencryptionPublicKey43chars0"
}
```

### 2. Bob registers

```http
POST /v1/users HTTP/1.1
Content-Type: application/json

{
  "identity_key":   "BOBidentityPublicKeyXXX43chars0",
  "encryption_key": "BOBencryptionPublicKeyXXX43chars"
}
```

### 3. Alice fetches Bob's encryption key

```http
GET /v1/users/BOBidentityPublicKeyXXX43chars0 HTTP/1.1
```

### 4. Alice encrypts and sends a message to Bob

```http
POST /v1/messages HTTP/1.1
Authorization: Ed25519 ALICEidentityPublicKey43chars00.1710000000.ALICE_SIG
Content-Type: application/json

{
  "recipient_key": "BOBidentityPublicKeyXXX43chars0",
  "ephemeral_key": "ephemeralPubKey43chars000000000",
  "ciphertext":    "base64urlEncodedChaChaCiphertext"
}
```

### 5. Bob retrieves the message (offline)

```http
GET /v1/messages/pending HTTP/1.1
Authorization: Ed25519 BOBidentityPublicKeyXXX43chars0.1710000001.BOB_SIG
```

### 6. Bob receives the message (online, via WebSocket)

```
GET /v1/ws?auth=BOBidentityPublicKeyXXX43chars0.1710000001.BOB_SIG
Upgrade: websocket
```

Server pushes frame:

```json
{
  "type":          "message",
  "id":            "550e8400-e29b-41d4-a716-446655440000",
  "sender_key":    "ALICEidentityPublicKey43chars00",
  "ephemeral_key": "ephemeralPubKey43chars000000000",
  "ciphertext":    "base64urlEncodedChaChaCiphertext",
  "created_at":    "2024-01-15T10:30:00Z"
}
```

Bob decrypts locally and ACKs:

```json
{
  "type": "ack",
  "id":   "550e8400-e29b-41d4-a716-446655440000"
}
```
