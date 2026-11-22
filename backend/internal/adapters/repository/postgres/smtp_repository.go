package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/opentermius/opentermius/internal/domain"
)

type SMTPRepository struct {
	pool *pgxpool.Pool
	key  [32]byte
}

func NewSMTPRepository(pool *pgxpool.Pool, appSecret string) *SMTPRepository {
	return &SMTPRepository{
		pool: pool,
		key:  sha256.Sum256([]byte(appSecret)),
	}
}

func (r *SMTPRepository) encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(r.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (r *SMTPRepository) decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(r.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted secret")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (r *SMTPRepository) Get(ctx context.Context) (*domain.SMTPSettings, error) {
	if r.pool == nil {
		return &domain.SMTPSettings{Port: 587, FromName: "OpenTermius Gateway", UseTLS: true}, nil
	}
	const query = `
		SELECT enabled, host, port, username, password_enc, from_name, from_email, use_tls, web_base_url, updated_at
		FROM smtp_settings
		WHERE id = 1
	`
	var cfg domain.SMTPSettings
	var passEnc string
	err := r.pool.QueryRow(ctx, query).Scan(
		&cfg.Enabled,
		&cfg.Host,
		&cfg.Port,
		&cfg.Username,
		&passEnc,
		&cfg.FromName,
		&cfg.FromEmail,
		&cfg.UseTLS,
		&cfg.WebBaseURL,
		&cfg.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.SMTPSettings{Port: 587, FromName: "OpenTermius Gateway", UseTLS: true}, nil
	}
	if err != nil {
		return nil, err
	}
	if passEnc != "" {
		cfg.HasPassword = true
		if dec, decErr := r.decrypt(passEnc); decErr == nil {
			cfg.Password = dec
		}
	}
	return &cfg, nil
}

func (r *SMTPRepository) Upsert(ctx context.Context, cfg *domain.SMTPSettings) error {
	if r.pool == nil {
		return errors.New("database not connected")
	}
	passEnc, err := r.encrypt(cfg.Password)
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO smtp_settings (id, enabled, host, port, username, password_enc, from_name, from_email, use_tls, web_base_url, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			host = EXCLUDED.host,
			port = EXCLUDED.port,
			username = EXCLUDED.username,
			password_enc = CASE WHEN EXCLUDED.password_enc = '' THEN smtp_settings.password_enc ELSE EXCLUDED.password_enc END,
			from_name = EXCLUDED.from_name,
			from_email = EXCLUDED.from_email,
			use_tls = EXCLUDED.use_tls,
			web_base_url = EXCLUDED.web_base_url,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err = r.pool.Exec(ctx, query,
		cfg.Enabled,
		cfg.Host,
		cfg.Port,
		cfg.Username,
		passEnc,
		cfg.FromName,
		cfg.FromEmail,
		cfg.UseTLS,
		cfg.WebBaseURL,
	)
	return err
}

type InviteRepository struct {
	pool *pgxpool.Pool
}

func NewInviteRepository(pool *pgxpool.Pool) *InviteRepository {
	return &InviteRepository{pool: pool}
}

func (r *InviteRepository) Create(ctx context.Context, inv *domain.UserInvite) error {
	const query = `
		INSERT INTO user_invites (id, email, name, token_hash, invited_by, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.pool.Exec(ctx, query,
		inv.ID, inv.Email, inv.Name, inv.TokenHash, inv.InvitedBy, inv.ExpiresAt, inv.CreatedAt,
	)
	return err
}

func (r *InviteRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.UserInvite, error) {
	const query = `
		SELECT id, email, name, token_hash, invited_by, expires_at, used_at, created_at
		FROM user_invites
		WHERE token_hash = $1
	`
	var inv domain.UserInvite
	err := r.pool.QueryRow(ctx, query, tokenHash).Scan(
		&inv.ID, &inv.Email, &inv.Name, &inv.TokenHash, &inv.InvitedBy, &inv.ExpiresAt, &inv.UsedAt, &inv.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *InviteRepository) MarkUsed(ctx context.Context, id uuid.UUID) error {
	const query = `UPDATE user_invites SET used_at = CURRENT_TIMESTAMP WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *InviteRepository) ListPending(ctx context.Context) ([]domain.UserInvite, error) {
	const query = `
		SELECT id, email, name, token_hash, invited_by, expires_at, used_at, created_at
		FROM user_invites
		WHERE used_at IS NULL AND expires_at > CURRENT_TIMESTAMP
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.UserInvite
	for rows.Next() {
		var inv domain.UserInvite
		if err := rows.Scan(
			&inv.ID, &inv.Email, &inv.Name, &inv.TokenHash, &inv.InvitedBy, &inv.ExpiresAt, &inv.UsedAt, &inv.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, inv)
	}
	return list, rows.Err()
}

func (r *InviteRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `DELETE FROM user_invites WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
