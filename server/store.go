package main

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Models ───────────────────────────────────────────────────────────────────

type User struct {
	IdentityKey   string    `json:"identity_key"`
	EncryptionKey string    `json:"encryption_key"`
	Username      *string   `json:"username,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type Nexus struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatorKey string    `json:"creator_key"`
	CreatedAt  time.Time `json:"created_at"`
	Role       string    `json:"role,omitempty"` // filled when listing user's nexuses
}

type NexusMember struct {
	IdentityKey   string    `json:"identity_key"`
	Username      *string   `json:"username,omitempty"`
	EncryptionKey string    `json:"encryption_key"`
	Role          string    `json:"role"`
	JoinedAt      time.Time `json:"joined_at"`
}

type InviteCode struct {
	ID        string     `json:"id"`
	NexusID   string     `json:"nexus_id"`
	Code      string     `json:"code"`
	CreatedBy string     `json:"created_by"`
	MaxUses   *int       `json:"max_uses,omitempty"`
	UseCount  int        `json:"use_count"`
	Revoked   bool       `json:"revoked"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type Message struct {
	ID            string    `json:"id"`
	NexusID       string    `json:"nexus_id"`
	SenderKey     string    `json:"sender_key"`
	RecipientKey  string    `json:"recipient_key"`
	EphemeralKey  string    `json:"ephemeral_key"`
	Ciphertext    string    `json:"ciphertext"`
	CreatedAt     time.Time `json:"created_at"`
}

type MessageEnvelope struct {
	RecipientKey string `json:"recipient_key"`
	EphemeralKey string `json:"ephemeral_key"`
	Ciphertext   string `json:"ciphertext"`
}

// ── Store ────────────────────────────────────────────────────────────────────

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ── Users ────────────────────────────────────────────────────────────────────

func (s *Store) CreateUser(ctx context.Context, identityKey, encryptionKey string, username *string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (identity_key, encryption_key, username) VALUES ($1, $2, $3)`,
		identityKey, encryptionKey, username,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return errors.New("conflict")
		}
		return err
	}
	return nil
}

func (s *Store) GetUser(ctx context.Context, identityKey string) (*User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT identity_key, encryption_key, username, created_at FROM users WHERE identity_key = $1`,
		identityKey,
	)
	u := &User{}
	err := row.Scan(&u.IdentityKey, &u.EncryptionKey, &u.Username, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}


func (s *Store) SetUsername(ctx context.Context, identityKey, username string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE users SET username = $2 WHERE identity_key = $1`,
		identityKey, username,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errors.New("not_found")
	}
	return nil
}

// ── Nexuses ──────────────────────────────────────────────────────────────────

func (s *Store) CreateNexus(ctx context.Context, name, creatorKey string) (*Nexus, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO nexuses (name, creator_key) VALUES ($1, $2)
		 RETURNING id, name, creator_key, created_at`,
		name, creatorKey,
	)
	n := &Nexus{}
	if err := row.Scan(&n.ID, &n.Name, &n.CreatorKey, &n.CreatedAt); err != nil {
		return nil, err
	}
	// Creator is automatically an admin member.
	_, err := s.pool.Exec(ctx,
		`INSERT INTO nexus_members (nexus_id, identity_key, role) VALUES ($1, $2, 'admin')`,
		n.ID, creatorKey,
	)
	if err != nil {
		return nil, err
	}
	n.Role = "admin"
	return n, nil
}

func (s *Store) GetNexus(ctx context.Context, nexusID string) (*Nexus, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, name, creator_key, created_at FROM nexuses WHERE id = $1::uuid`,
		nexusID,
	)
	n := &Nexus{}
	err := row.Scan(&n.ID, &n.Name, &n.CreatorKey, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Store) GetUserNexuses(ctx context.Context, identityKey string) ([]*Nexus, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT n.id, n.name, n.creator_key, n.created_at, nm.role
		 FROM nexuses n
		 JOIN nexus_members nm ON nm.nexus_id = n.id
		 WHERE nm.identity_key = $1
		 ORDER BY n.created_at DESC`,
		identityKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nexuses []*Nexus
	for rows.Next() {
		n := &Nexus{}
		if err := rows.Scan(&n.ID, &n.Name, &n.CreatorKey, &n.CreatedAt, &n.Role); err != nil {
			return nil, err
		}
		nexuses = append(nexuses, n)
	}
	return nexuses, rows.Err()
}

func (s *Store) UpdateNexus(ctx context.Context, nexusID, name string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE nexuses SET name = $2 WHERE id = $1::uuid`,
		nexusID, name,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errors.New("not_found")
	}
	return nil
}

func (s *Store) DeleteNexus(ctx context.Context, nexusID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM nexuses WHERE id = $1::uuid`, nexusID)
	return err
}

// ── Nexus Members ────────────────────────────────────────────────────────────

func (s *Store) GetNexusMembers(ctx context.Context, nexusID string) ([]*NexusMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.identity_key, u.username, u.encryption_key, nm.role, nm.joined_at
		 FROM nexus_members nm
		 JOIN users u ON u.identity_key = nm.identity_key
		 WHERE nm.nexus_id = $1::uuid
		 ORDER BY nm.joined_at ASC`,
		nexusID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []*NexusMember
	for rows.Next() {
		m := &NexusMember{}
		if err := rows.Scan(&m.IdentityKey, &m.Username, &m.EncryptionKey, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *Store) AddNexusMember(ctx context.Context, nexusID, identityKey, role string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO nexus_members (nexus_id, identity_key, role) VALUES ($1::uuid, $2, $3)`,
		nexusID, identityKey, role,
	)
	if err != nil && isUniqueViolation(err) {
		return errors.New("already_member")
	}
	return err
}

func (s *Store) RemoveNexusMember(ctx context.Context, nexusID, identityKey string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM nexus_members WHERE nexus_id = $1::uuid AND identity_key = $2`,
		nexusID, identityKey,
	)
	return err
}

func (s *Store) IsNexusMember(ctx context.Context, nexusID, identityKey string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM nexus_members WHERE nexus_id = $1::uuid AND identity_key = $2)`,
		nexusID, identityKey,
	).Scan(&exists)
	return exists, err
}

func (s *Store) IsNexusAdmin(ctx context.Context, nexusID, identityKey string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM nexus_members WHERE nexus_id = $1::uuid AND identity_key = $2 AND role = 'admin')`,
		nexusID, identityKey,
	).Scan(&exists)
	return exists, err
}

// ── Kicked Members ───────────────────────────────────────────────────────────

func (s *Store) KickMember(ctx context.Context, nexusID, identityKey string) error {
	// Remove from members and add to kicked list in one transaction.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`DELETE FROM nexus_members WHERE nexus_id = $1::uuid AND identity_key = $2`,
		nexusID, identityKey,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO kicked_members (nexus_id, identity_key) VALUES ($1::uuid, $2)
		 ON CONFLICT DO NOTHING`,
		nexusID, identityKey,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) IsKicked(ctx context.Context, nexusID, identityKey string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM kicked_members WHERE nexus_id = $1::uuid AND identity_key = $2)`,
		nexusID, identityKey,
	).Scan(&exists)
	return exists, err
}

// ── Invite Codes ─────────────────────────────────────────────────────────────

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous chars

func generateCode(length int) (string, error) {
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeChars))))
		if err != nil {
			return "", err
		}
		b[i] = codeChars[n.Int64()]
	}
	return string(b), nil
}

func (s *Store) CreateInviteCode(ctx context.Context, nexusID, createdBy string, maxUses *int, expiresAt *time.Time) (*InviteCode, error) {
	code, err := generateCode(8)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO invite_codes (nexus_id, code, created_by, max_uses, expires_at)
		 VALUES ($1::uuid, $2, $3, $4, $5)
		 RETURNING id, nexus_id, code, created_by, max_uses, use_count, revoked, created_at, expires_at`,
		nexusID, code, createdBy, maxUses, expiresAt,
	)
	ic := &InviteCode{}
	if err := row.Scan(&ic.ID, &ic.NexusID, &ic.Code, &ic.CreatedBy, &ic.MaxUses, &ic.UseCount, &ic.Revoked, &ic.CreatedAt, &ic.ExpiresAt); err != nil {
		return nil, err
	}
	return ic, nil
}

func (s *Store) GetInviteCodes(ctx context.Context, nexusID string) ([]*InviteCode, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, nexus_id, code, created_by, max_uses, use_count, revoked, created_at, expires_at
		 FROM invite_codes WHERE nexus_id = $1::uuid ORDER BY created_at DESC`,
		nexusID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var codes []*InviteCode
	for rows.Next() {
		ic := &InviteCode{}
		if err := rows.Scan(&ic.ID, &ic.NexusID, &ic.Code, &ic.CreatedBy, &ic.MaxUses, &ic.UseCount, &ic.Revoked, &ic.CreatedAt, &ic.ExpiresAt); err != nil {
			return nil, err
		}
		codes = append(codes, ic)
	}
	return codes, rows.Err()
}

func (s *Store) RevokeInviteCode(ctx context.Context, inviteID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE invite_codes SET revoked = TRUE WHERE id = $1::uuid`,
		inviteID,
	)
	return err
}

// ValidateAndUseInviteCode checks the code, increments use_count, and returns
// the nexus ID. Returns an error string for user-facing issues.
func (s *Store) ValidateAndUseInviteCode(ctx context.Context, code, identityKey string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx,
		`SELECT id, nexus_id, max_uses, use_count, revoked, expires_at
		 FROM invite_codes WHERE code = $1 FOR UPDATE`,
		code,
	)
	var (
		inviteID  string
		nexusID   string
		maxUses   *int
		useCount  int
		revoked   bool
		expiresAt *time.Time
	)
	if err := row.Scan(&inviteID, &nexusID, &maxUses, &useCount, &revoked, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("invalid_code")
		}
		return "", err
	}

	if revoked {
		return "", errors.New("code_revoked")
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		return "", errors.New("code_expired")
	}
	if maxUses != nil && useCount >= *maxUses {
		return "", errors.New("code_exhausted")
	}

	// Check if user is kicked from this nexus.
	kicked, err := s.IsKicked(ctx, nexusID, identityKey)
	if err != nil {
		return "", err
	}
	if kicked {
		return "", errors.New("kicked")
	}

	// Check if already a member.
	member, err := s.IsNexusMember(ctx, nexusID, identityKey)
	if err != nil {
		return "", err
	}
	if member {
		return "", errors.New("already_member")
	}

	// Increment use count and add member.
	_, err = tx.Exec(ctx,
		`UPDATE invite_codes SET use_count = use_count + 1 WHERE id = $1::uuid`,
		inviteID,
	)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO nexus_members (nexus_id, identity_key, role) VALUES ($1::uuid, $2, 'member')`,
		nexusID, identityKey,
	)
	if err != nil {
		return "", err
	}

	return nexusID, tx.Commit(ctx)
}

// ── Messages ─────────────────────────────────────────────────────────────────

// SaveNexusMessages batch-inserts one message row per recipient envelope.
// All rows share the same message ID (logical message) and timestamp.
func (s *Store) SaveNexusMessages(ctx context.Context, nexusID, senderKey string, envelopes []MessageEnvelope) (string, time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback(ctx)

	// Generate a shared message ID and timestamp.
	var msgID string
	var createdAt time.Time
	err = tx.QueryRow(ctx, `SELECT gen_random_uuid(), NOW()`).Scan(&msgID, &createdAt)
	if err != nil {
		return "", time.Time{}, err
	}

	for _, env := range envelopes {
		_, err = tx.Exec(ctx,
			`INSERT INTO messages (id, nexus_id, sender_key, recipient_key, ephemeral_key, ciphertext, created_at)
			 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7)`,
			msgID, nexusID, senderKey, env.RecipientKey, env.EphemeralKey, env.Ciphertext, createdAt,
		)
		if err != nil {
			return "", time.Time{}, err
		}
	}

	return msgID, createdAt, tx.Commit(ctx)
}

func (s *Store) GetNexusHistory(ctx context.Context, nexusID, recipientKey string, limit int, since *time.Time, beforeID *string) ([]*Message, error) {
	const cols = `SELECT id, nexus_id, sender_key, recipient_key, ephemeral_key, ciphertext, created_at FROM messages`
	where := ` WHERE nexus_id = $1::uuid AND recipient_key = $2`

	var (
		query string
		args  []any
	)

	switch {
	case beforeID != nil:
		query = cols + where + ` AND created_at < (SELECT created_at FROM messages WHERE id = $3::uuid AND recipient_key = $2 LIMIT 1) ORDER BY created_at DESC LIMIT $4`
		args = []any{nexusID, recipientKey, *beforeID, limit}
	case since != nil:
		query = cols + where + ` AND created_at > $3 ORDER BY created_at ASC LIMIT $4`
		args = []any{nexusID, recipientKey, *since, limit}
	default:
		query = cols + where + ` ORDER BY created_at DESC LIMIT $3`
		args = []any{nexusID, recipientKey, limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*Message
	for rows.Next() {
		m := &Message{}
		if err := rows.Scan(&m.ID, &m.NexusID, &m.SenderKey, &m.RecipientKey, &m.EphemeralKey, &m.Ciphertext, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) GetPendingMessages(ctx context.Context, recipientKey string) ([]*Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, nexus_id, sender_key, recipient_key, ephemeral_key, ciphertext, created_at
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
		if err := rows.Scan(&m.ID, &m.NexusID, &m.SenderKey, &m.RecipientKey, &m.EphemeralKey, &m.Ciphertext, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

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

// ── Device Tokens ────────────────────────────────────────────────────────────

func (s *Store) UpsertDeviceToken(ctx context.Context, identityKey, token string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO device_tokens (identity_key, token, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (identity_key, token) DO UPDATE SET updated_at = NOW()`,
		identityKey, token,
	)
	return err
}

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

func (s *Store) DeleteDeviceToken(ctx context.Context, identityKey, token string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM device_tokens WHERE identity_key = $1 AND token = $2`,
		identityKey, token,
	)
	return err
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func isUniqueViolation(err error) bool {
	type pgErr interface{ SQLState() string }
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == "23505"
	}
	return false
}
