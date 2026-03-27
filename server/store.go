package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User represents a registered user.
type User struct {
	IdentityKey   string    `json:"identity_key"`
	EncryptionKey string    `json:"encryption_key"`
	CreatedAt     time.Time `json:"created_at"`
}

// Message represents a stored encrypted message.
type Message struct {
	ID                 string    `json:"id"`
	SenderKey          string    `json:"sender_key"`
	RecipientKey       string    `json:"recipient_key"`
	EphemeralKey       string    `json:"ephemeral_key"`
	Ciphertext         string    `json:"ciphertext"`
	SenderEphemeralKey *string   `json:"sender_ephemeral_key,omitempty"`
	SenderCiphertext   *string   `json:"sender_ciphertext,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

// Store wraps a pgx connection pool and provides typed data-access methods.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store using an existing pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreateUser inserts a new user record. Returns an error whose message is
// "conflict" when a user with the given identity_key already exists.
func (s *Store) CreateUser(ctx context.Context, identityKey, encryptionKey string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (identity_key, encryption_key) VALUES ($1, $2)`,
		identityKey, encryptionKey,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return errors.New("conflict")
		}
		return err
	}
	return nil
}

// GetUser returns the user with the given identity key, or nil, nil when no
// such user exists.
func (s *Store) GetUser(ctx context.Context, identityKey string) (*User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT identity_key, encryption_key, created_at FROM users WHERE identity_key = $1`,
		identityKey,
	)
	u := &User{}
	err := row.Scan(&u.IdentityKey, &u.EncryptionKey, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// SaveMessage persists an encrypted message and returns the stored record.
// senderEphKey and senderCT are optional sender-copy fields (may be nil).
func (s *Store) SaveMessage(ctx context.Context, senderKey, recipientKey, ephemeralKey, ciphertext string, senderEphKey, senderCT *string) (*Message, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO messages (sender_key, recipient_key, ephemeral_key, ciphertext, sender_ephemeral_key, sender_ciphertext)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, sender_key, recipient_key, ephemeral_key, ciphertext, sender_ephemeral_key, sender_ciphertext, created_at`,
		senderKey, recipientKey, ephemeralKey, ciphertext, senderEphKey, senderCT,
	)
	m := &Message{}
	err := row.Scan(&m.ID, &m.SenderKey, &m.RecipientKey, &m.EphemeralKey, &m.Ciphertext, &m.SenderEphemeralKey, &m.SenderCiphertext, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// GetPendingMessages returns all undelivered messages for the given recipient,
// ordered by creation time (oldest first).
func (s *Store) GetPendingMessages(ctx context.Context, recipientKey string) ([]*Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, sender_key, recipient_key, ephemeral_key, ciphertext, sender_ephemeral_key, sender_ciphertext, created_at
		 FROM messages
		 WHERE recipient_key = $1 AND delivered_at IS NULL
		 ORDER BY created_at ASC`,
		recipientKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*Message
	for rows.Next() {
		m := &Message{}
		if err := rows.Scan(&m.ID, &m.SenderKey, &m.RecipientKey, &m.EphemeralKey, &m.Ciphertext, &m.SenderEphemeralKey, &m.SenderCiphertext, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetHistory returns messages involving ownerKey — both received messages and
// sent messages for which a sender copy was stored.
//
// Pagination modes (mutually exclusive; beforeID takes priority over since):
//   - beforeID set: return up to limit messages older than the given message ID,
//     ordered DESC (caller should reverse for display).
//   - since set: return up to limit messages with created_at > since, ordered ASC
//     (incremental sync).
//   - neither set: return the most recent limit messages, ordered DESC (caller
//     should reverse for display).
func (s *Store) GetHistory(ctx context.Context, ownerKey string, limit int, since *time.Time, beforeID *string) ([]*Message, error) {
	const owner = `(recipient_key = $1 OR (sender_key = $1 AND sender_ciphertext IS NOT NULL))`
	const cols = `SELECT id, sender_key, recipient_key, ephemeral_key, ciphertext, sender_ephemeral_key, sender_ciphertext, created_at FROM messages`

	var (
		query string
		args  []any
	)

	switch {
	case beforeID != nil:
		// Older-page fetch: messages created before the anchor message.
		query = cols + ` WHERE ` + owner + ` AND created_at < (SELECT created_at FROM messages WHERE id = $2::uuid) ORDER BY created_at DESC LIMIT $3`
		args = []any{ownerKey, *beforeID, limit}
	case since != nil:
		// Incremental sync: messages newer than the given timestamp.
		query = cols + ` WHERE ` + owner + ` AND created_at > $2 ORDER BY created_at ASC LIMIT $3`
		args = []any{ownerKey, *since, limit}
	default:
		// Initial load: most recent N messages.
		query = cols + ` WHERE ` + owner + ` ORDER BY created_at DESC LIMIT $2`
		args = []any{ownerKey, limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*Message
	for rows.Next() {
		m := &Message{}
		if err := rows.Scan(&m.ID, &m.SenderKey, &m.RecipientKey, &m.EphemeralKey, &m.Ciphertext, &m.SenderEphemeralKey, &m.SenderCiphertext, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// MarkDelivered sets delivered_at to NOW() for all messages whose IDs are in
// the provided list. IDs not present in the database are silently ignored.
func (s *Store) MarkDelivered(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET delivered_at = NOW()
		 WHERE id = ANY($1::uuid[]) AND delivered_at IS NULL`,
		ids,
	)
	return err
}

// Contact represents a contact stored server-side for a user.
type Contact struct {
	ContactKey    string    `json:"contact_key"`
	Nickname      string    `json:"nickname"`
	EncryptionKey *string   `json:"encryption_key,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// GetContacts returns all contacts for the given owner, ordered by nickname.
// Joins the users table to include each contact's encryption key.
func (s *Store) GetContacts(ctx context.Context, ownerKey string) ([]*Contact, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.contact_key, c.nickname, u.encryption_key, c.updated_at
		 FROM contacts c
		 LEFT JOIN users u ON u.identity_key = c.contact_key
		 WHERE c.owner_key = $1 ORDER BY c.nickname ASC`,
		ownerKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var contacts []*Contact
	for rows.Next() {
		c := &Contact{}
		if err := rows.Scan(&c.ContactKey, &c.Nickname, &c.EncryptionKey, &c.UpdatedAt); err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

// UpsertContact inserts or updates a contact for the given owner.
func (s *Store) UpsertContact(ctx context.Context, ownerKey, contactKey, nickname string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO contacts (owner_key, contact_key, nickname, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (owner_key, contact_key)
		 DO UPDATE SET nickname = EXCLUDED.nickname, updated_at = NOW()`,
		ownerKey, contactKey, nickname,
	)
	return err
}

// DeleteContact removes a contact for the given owner.
func (s *Store) DeleteContact(ctx context.Context, ownerKey, contactKey string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM contacts WHERE owner_key = $1 AND contact_key = $2`,
		ownerKey, contactKey,
	)
	return err
}

// UpsertDeviceToken stores a device token for the given identity key.
func (s *Store) UpsertDeviceToken(ctx context.Context, identityKey, token string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO device_tokens (identity_key, token, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (identity_key, token) DO UPDATE SET updated_at = NOW()`,
		identityKey, token,
	)
	return err
}

// GetDeviceTokens returns all device tokens for the given identity key.
func (s *Store) GetDeviceTokens(ctx context.Context, identityKey string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT token FROM device_tokens WHERE identity_key = $1`,
		identityKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// DeleteDeviceToken removes a specific device token.
func (s *Store) DeleteDeviceToken(ctx context.Context, identityKey, token string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM device_tokens WHERE identity_key = $1 AND token = $2`,
		identityKey, token,
	)
	return err
}

// isUniqueViolation returns true when the error is a PostgreSQL unique-
// constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	// pgx wraps pgconn.PgError; check the code via interface.
	type pgErr interface{ SQLState() string }
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == "23505"
	}
	return false
}
