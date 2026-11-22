package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opentermius/opentermius/internal/adapters/mail"
	"github.com/opentermius/opentermius/internal/domain"
	"github.com/opentermius/opentermius/internal/ports"
)

type SMTPUseCase struct {
	smtpRepo   ports.SMTPRepository
	inviteRepo ports.InviteRepository
	userRepo   ports.UserRepository
	authUC     *AuthUseCase
	mailer     *mail.SMTPMailer
}

func NewSMTPUseCase(
	smtpRepo ports.SMTPRepository,
	inviteRepo ports.InviteRepository,
	userRepo ports.UserRepository,
	authUC *AuthUseCase,
	mailer *mail.SMTPMailer,
) *SMTPUseCase {
	return &SMTPUseCase{
		smtpRepo:   smtpRepo,
		inviteRepo: inviteRepo,
		userRepo:   userRepo,
		authUC:     authUC,
		mailer:     mailer,
	}
}

func (u *SMTPUseCase) GetSettings(ctx context.Context) (*domain.SMTPSettings, error) {
	return u.smtpRepo.Get(ctx)
}

func (u *SMTPUseCase) UpdateSettings(ctx context.Context, cfg *domain.SMTPSettings) (*domain.SMTPSettings, error) {
	if cfg.Port <= 0 {
		cfg.Port = 587
	}
	if strings.TrimSpace(cfg.FromName) == "" {
		cfg.FromName = "OpenTermius Gateway"
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.FromEmail = strings.TrimSpace(cfg.FromEmail)
	cfg.WebBaseURL = strings.TrimRight(strings.TrimSpace(cfg.WebBaseURL), "/")

	if err := u.smtpRepo.Upsert(ctx, cfg); err != nil {
		return nil, err
	}
	return u.smtpRepo.Get(ctx)
}

func (u *SMTPUseCase) SendTestEmail(ctx context.Context, toEmail string) error {
	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" {
		return domain.ErrInvalidInput
	}
	cfg, err := u.smtpRepo.Get(ctx)
	if err != nil {
		return err
	}
	// Allow testing even before flipping enabled=true if host/from are filled
	testCfg := *cfg
	testCfg.Enabled = true
	body := "Hello,\n\nThis is a test email from your OpenTermius Gateway SMTP configuration.\nIf you received this message, email invites and notifications are ready to use.\n\n— OpenTermius Gateway\n"
	return u.mailer.Send(&testCfg, toEmail, "OpenTermius Gateway — SMTP Test Email", body)
}

// SendNotification sends a best-effort notification email if SMTP is enabled.
func (u *SMTPUseCase) SendNotification(ctx context.Context, toEmail, subject, body string) error {
	cfg, err := u.smtpRepo.Get(ctx)
	if err != nil || cfg == nil || !cfg.Enabled {
		return nil
	}
	return u.mailer.Send(cfg, toEmail, subject, body)
}

type InviteResult struct {
	Invite    *domain.UserInvite `json:"invite"`
	InviteURL string             `json:"invite_url"`
	EmailSent bool               `json:"email_sent"`
	EmailErr  string             `json:"email_error,omitempty"`
}

func hashInviteToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (u *SMTPUseCase) CreateInvite(ctx context.Context, invitedBy uuid.UUID, email, name, fallbackOrigin string) (*InviteResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if email == "" || name == "" {
		return nil, domain.ErrInvalidInput
	}

	if existing, err := u.userRepo.GetByEmail(ctx, email); err == nil && existing != nil {
		return nil, domain.ErrConflict
	}

	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, err
	}
	rawToken := hex.EncodeToString(rawBytes)

	now := time.Now().UTC()
	inv := &domain.UserInvite{
		ID:        uuid.New(),
		Email:     email,
		Name:      name,
		TokenHash: hashInviteToken(rawToken),
		InvitedBy: invitedBy,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}

	if err := u.inviteRepo.Create(ctx, inv); err != nil {
		return nil, err
	}

	cfg, _ := u.smtpRepo.Get(ctx)
	baseURL := ""
	if cfg != nil && cfg.WebBaseURL != "" {
		baseURL = strings.TrimRight(cfg.WebBaseURL, "/")
	} else if fallbackOrigin != "" {
		baseURL = strings.TrimRight(fallbackOrigin, "/")
	}
	inviteURL := fmt.Sprintf("%s/?invite=%s", baseURL, rawToken)

	res := &InviteResult{
		Invite:    inv,
		InviteURL: inviteURL,
	}

	if cfg != nil && cfg.Enabled {
		subject := "You're invited to OpenTermius Gateway"
		body := fmt.Sprintf(
			"Hi %s,\n\nYou have been invited to join OpenTermius Gateway.\n\nClick the link below to set your password and activate your account (valid for 24 hours):\n%s\n\nOnce activated, sign in using the OpenTermius Desktop or Mobile app.\n\n— OpenTermius Gateway\n",
			name,
			inviteURL,
		)
		if err := u.mailer.Send(cfg, email, subject, body); err != nil {
			res.EmailErr = err.Error()
		} else {
			res.EmailSent = true
		}
	}

	return res, nil
}

func (u *SMTPUseCase) ListInvites(ctx context.Context) ([]domain.UserInvite, error) {
	return u.inviteRepo.ListPending(ctx)
}

func (u *SMTPUseCase) DeleteInvite(ctx context.Context, id uuid.UUID) error {
	return u.inviteRepo.Delete(ctx, id)
}

func (u *SMTPUseCase) VerifyInvite(ctx context.Context, rawToken string) (*domain.UserInvite, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, domain.ErrInvalidInput
	}
	inv, err := u.inviteRepo.GetByTokenHash(ctx, hashInviteToken(rawToken))
	if err != nil {
		return nil, domain.ErrNotFound
	}
	if inv.UsedAt != nil || time.Now().UTC().After(inv.ExpiresAt) {
		return nil, domain.ErrUnauthorized
	}
	return inv, nil
}

func (u *SMTPUseCase) AcceptInvite(ctx context.Context, rawToken, password string) (*domain.User, string, string, error) {
	if len(password) < 8 {
		return nil, "", "", domain.ErrInvalidInput
	}
	inv, err := u.VerifyInvite(ctx, rawToken)
	if err != nil {
		return nil, "", "", err
	}

	user, accessToken, refreshToken, err := u.authUC.Register(ctx, inv.Email, password, inv.Name)
	if err != nil {
		return nil, "", "", err
	}
	_ = u.inviteRepo.MarkUsed(ctx, inv.ID)

	return user, accessToken, refreshToken, nil
}
