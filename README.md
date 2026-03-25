# Nexus Zero

A private, end-to-end encrypted messenger. The server stores only ciphertext — it never has access to plaintext messages or private keys. All cryptographic operations happen exclusively on client devices.

## Clients

| Client | Stack | Path |
|--------|-------|------|
| iOS app | Swift / SwiftUI | `ios/` |
| Web app | TypeScript / Vite | `web/` |
| Terminal UI | Go / Bubble Tea | `tui/` |
| CLI | Go | `cli/` |

All clients share the same cryptographic scheme and are fully interoperable — you can use the same account across all of them simultaneously.

---

## Cryptography

Every user has two Curve25519 keypairs generated locally:

| Keypair | Algorithm | Purpose |
|---------|-----------|---------|
| Signing key | Ed25519 | Server authentication, identity |
| Agreement key | X25519 | Message encryption |

The **identity key** (Ed25519 public key, base64url) is the user's permanent identifier. The **encryption key** (X25519 public key, base64url) is published to the server so anyone can encrypt messages to the user without prior interaction.

### Per-message encryption

1. Generate a fresh ephemeral X25519 keypair
2. `shared_secret = ECDH(ephemeral_priv, recipient_pub)`
3. `key = HKDF-SHA256(shared_secret, salt="messenger-v1", info="message", 32 bytes)`
4. `ciphertext = nonce(12) ‖ ChaCha20-Poly1305(plaintext, key) ‖ tag(16)`
5. Send `(base64url(ephemeral_pub), base64url(ciphertext))` to the server

The server cannot decrypt any message because it never holds a private key.

### Sender copies

When sending, the client also encrypts the plaintext for its own X25519 public key (`sender_ephemeral_key` + `sender_ciphertext`). This sender copy is stored alongside the message and used to reconstruct outgoing message history after reinstall, and to sync sent messages to the user's other devices in real time.

### Authentication

Every authenticated request includes:
```
Authorization: Ed25519 <pubkey_b64>.<unix_timestamp>.<signature_b64>
```
The signature covers `"<pubkey_b64>.<unix_timestamp>"` using Ed25519. The server validates the signature and rejects tokens older than ±30 seconds.

### Account backup

Private keys are exported as a backup code:
```
<signingPriv_base64url>.<agreementPriv_base64url>
```
This code can restore the account on any client, including recovering full message history from the server.

---

## Server

Go backend with PostgreSQL. Provides a REST API and a WebSocket endpoint for real-time delivery.

### Requirements

- Go 1.22+
- PostgreSQL 14+

### Setup

```bash
cd server

# Create database
createdb messenger
psql messenger < schema.sql

# Run
DATABASE_URL="postgres://user:pass@localhost/messenger" PORT=8888 go run .
```

### API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/v1/users` | No | Register identity + encryption key |
| `GET` | `/v1/users/:key` | No | Look up a user's public keys |
| `POST` | `/v1/messages` | Yes | Send encrypted message |
| `GET` | `/v1/messages/pending` | Yes | Fetch + mark delivered unread messages |
| `GET` | `/v1/messages/history` | Yes | Paginated message history |
| `GET` | `/v1/contacts` | Yes | List server-side contacts |
| `PUT` | `/v1/contacts/:key` | Yes | Add / update a contact |
| `DELETE` | `/v1/contacts/:key` | Yes | Remove a contact |
| `GET` | `/v1/ws` | Via `?auth=` | WebSocket for real-time delivery |

#### History pagination

```
GET /v1/messages/history?limit=100&since=<ISO8601>&before_id=<uuid>
```

- `since` — incremental sync: only messages newer than this timestamp
- `before_id` — load older page: messages before this message ID (descending)
- `limit` — max results (default 100, max 500)

#### WebSocket

Connect with `GET /v1/ws?auth=<token>` (same token format as the Authorization header, without the `Ed25519 ` prefix).

Incoming frame:
```json
{ "type": "message", "id": "…", "sender_key": "…", "ephemeral_key": "…", "ciphertext": "…", "created_at": "…" }
```

For messages sent from the user's own other devices, the frame additionally includes:
```json
{ "recipient_key": "…", "sender_ephemeral_key": "…", "sender_ciphertext": "…" }
```

Client ACK:
```json
{ "type": "ack", "id": "…" }
```

The server supports multiple simultaneous WebSocket connections per identity key, enabling the same account to be active on multiple devices at once.

---

## iOS App

SwiftUI app targeting iOS 16+.

### Setup

1. Open `ios/Messenger.xcodeproj` in Xcode
2. Set the server URL in `ios/Messenger/AppState.swift` (`serverBaseURL`)
3. Build and run on a simulator or device

### Features

- Generate keys or restore from backup code
- Real-time messages via WebSocket with exponential backoff reconnection
- Paginated history with "Load earlier messages"
- Local notifications (suppressed when chat is active)
- Tap notification to open the relevant chat
- Server-synced contact list

---

## Web App

Vite + TypeScript SPA. All crypto runs in the browser using [`@noble`](https://paulmillr.com/noble/) libraries.

### Setup

```bash
cd web
npm install
npm run dev        # dev server at http://localhost:5173
npm run build      # production build → dist/
```

The server URL defaults to `http://localhost:8888` and can be changed in the Settings panel.

### Features

- Same onboarding and backup code flow as iOS (codes are cross-platform compatible)
- Real-time WebSocket with auto-reconnect
- Incremental history sync on startup, 60-second background poll
- Paginated "load earlier messages"
- Browser notifications
- Cyberpunk terminal theme matching the iOS app

---

## Terminal UI

Full-featured terminal client built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

### Setup

```bash
cd tui
go run . --server http://localhost:8888
```

On first run, keys are generated and stored in `~/.config/nexus-zero/`. Use `--keys <backup-code>` to restore an existing account.

---

## CLI

Minimal command-line tool for scripting and testing.

```bash
cd cli
go run . --server http://localhost:8888 send --to <identity_key> "hello"
go run . --server http://localhost:8888 chat --with <identity_key>
```

---

## Data flow

```
Sender                     Server                    Recipient
  │                           │                           │
  │  encrypt(plaintext,       │                           │
  │    recipient_pub_key)     │                           │
  │──POST /v1/messages───────▶│                           │
  │                           │──WebSocket push──────────▶│
  │◀─echo via WebSocket───────│  (real-time, if connected)│
  │  (sender copy, for other  │                           │
  │   devices on same account)│  or stored as pending     │
  │                           │  until GET /v1/messages/  │
  │                           │  pending is called        │
```

---

## Project structure

```
nexus-zero/
├── server/          Go REST API + WebSocket server
│   ├── main.go      Entry point, router
│   ├── handlers.go  HTTP handlers
│   ├── ws.go        WebSocket hub (multi-client per identity)
│   ├── store.go     PostgreSQL queries
│   ├── auth.go      Ed25519 token verification
│   └── schema.sql   Database schema + migrations
├── ios/             SwiftUI iOS client
│   └── Messenger/
│       ├── Crypto/  CryptoEngine (ECDH + HKDF + ChaCha20)
│       ├── Network/ APIClient, WebSocketClient
│       ├── Storage/ LocalStore (JSON files in Documents)
│       ├── Models/  Contact, StoredMessage
│       └── Views/   SwiftUI screens
├── web/             TypeScript/Vite web client
│   └── src/
│       ├── crypto.ts   Same crypto scheme in JS (@noble)
│       ├── storage.ts  localStorage persistence
│       ├── api.ts      HTTP + WebSocket client
│       └── app.ts      App state + UI
├── tui/             Terminal UI (Bubble Tea)
└── cli/             Command-line client
```
