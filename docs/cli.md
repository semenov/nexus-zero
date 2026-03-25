# CLI Client

A command-line client for testing and development. Stores keys and contacts as local JSON files — no installation or account required.

## Build

```bash
cd cli
go build -o mcli .
```

Move the binary somewhere on your PATH if you want:

```bash
mv mcli /usr/local/bin/mcli
```

## Config directory

All state (keys, contacts) lives in a single directory. Default is `~/.mcli`.

Override with the `--home` flag or the `MCLI_HOME` environment variable:

```bash
mcli --home /path/to/dir <command>
MCLI_HOME=/tmp/alice mcli <command>
```

`MCLI_HOME` is especially useful when running two identities on the same machine for testing.

## Commands

### `init`

Generate a keypair and register with the server. Run once.

```bash
mcli init
mcli init --server http://your-server:8080
```

Prints your identity key — the string you share with people who want to message you.

---

### `whoami`

Print your identity key.

```bash
mcli whoami
# rFRcbOjg1XZRcqafKu9EbzV8-k1cMDTgVx_pxCJlKH0
```

---

### `add`

Add a contact by their identity key. Fetches their encryption key from the server and saves the contact locally.

```bash
mcli add <identity_key> [nickname]
```

```bash
mcli add ZQxPguZTpCQlYWZvhHo9whe3HZvgaywhurtkdH7CtvE bob
```

If no nickname is given, the first 8 characters of the key are used.

---

### `contacts`

List all saved contacts.

```bash
mcli contacts

NICKNAME          IDENTITY KEY
----------------------------------------------------------------------
bob               ZQxPguZTpCQlYWZvhHo9whe3HZvgaywhurtkdH7CtvE
alice             rFRcbOjg1XZRcqafKu9EbzV8-k1cMDTgVx_pxCJlKH0
```

---

### `send`

Encrypt and send a message to a contact. The contact is looked up by nickname or identity key.

```bash
mcli send <nickname> <message>
```

```bash
mcli send bob "Hey, this is end-to-end encrypted"
# → bob: Hey, this is end-to-end encrypted
```

---

### `recv`

Fetch and decrypt all pending messages. Messages are marked as delivered on the server after fetching — they won't appear again.

```bash
mcli recv

[14:32:01] bob: Hey, this is end-to-end encrypted
[14:32:05] bob: Can you read this?
```

---

### `listen`

Connect via WebSocket and print messages as they arrive in real-time. Blocks until you press Ctrl+C.

```bash
mcli listen

listening on http://localhost:8080 — press Ctrl+C to exit
[14:35:10] bob: This arrives instantly
```

---

### `chat`

Interactive bidirectional chat with a contact. Type a message and press Enter to send. Incoming messages appear inline. Press Ctrl+C to exit.

```bash
mcli chat <nickname>
```

```bash
mcli chat bob

chatting with bob — type a message and press Enter (Ctrl+C to exit)

> hey!
[14:40:01] you: hey!
[14:40:04] bob: hello back
> how's it going?
[14:40:09] you: how's it going?
>
```

## Testing two identities locally

Use `MCLI_HOME` to run Alice and Bob in separate terminal windows on the same machine.

**Terminal 1 — Alice:**

```bash
export MCLI_HOME=/tmp/alice
mcli init
mcli whoami   # copy this key → give to Bob
```

**Terminal 2 — Bob:**

```bash
export MCLI_HOME=/tmp/bob
mcli init
mcli whoami   # copy this key → give to Alice
```

**Exchange contacts:**

```bash
# Alice adds Bob (paste Bob's key)
MCLI_HOME=/tmp/alice mcli add <bobs_key> bob

# Bob adds Alice (paste Alice's key)
MCLI_HOME=/tmp/bob mcli add <alices_key> alice
```

**Real-time chat:**

```bash
# Terminal 2 — Bob listens
MCLI_HOME=/tmp/bob mcli listen

# Terminal 1 — Alice sends
MCLI_HOME=/tmp/alice mcli send bob "hello!"

# Or use interactive chat
MCLI_HOME=/tmp/alice mcli chat bob
```

## Running the server locally

Requires Docker:

```bash
# Start PostgreSQL
docker run -d --name messenger-pg \
  -e POSTGRES_PASSWORD=messenger \
  -e POSTGRES_DB=messenger \
  -p 5432:5432 \
  postgres:16-alpine

# Apply schema
docker exec -i messenger-pg psql -U postgres -d messenger < server/schema.sql

# Start server
cd server
DATABASE_URL="postgres://postgres:messenger@localhost:5432/messenger" go run .
```

Server runs on `http://localhost:8080` by default. Change with the `PORT` env var.
