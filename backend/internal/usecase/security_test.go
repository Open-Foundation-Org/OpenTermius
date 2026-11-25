package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opentermius/opentermius/internal/domain"
)

type stubUserRepo struct {
	users map[uuid.UUID]*domain.User
}

func (s *stubUserRepo) Create(_ context.Context, u *domain.User) error {
	s.users[u.ID] = u
	return nil
}
func (s *stubUserRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	if u, ok := s.users[id]; ok {
		return u, nil
	}
	return nil, domain.ErrNotFound
}
func (s *stubUserRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (s *stubUserRepo) ListAll(_ context.Context) ([]domain.User, error) { return nil, nil }
func (s *stubUserRepo) Update(_ context.Context, u *domain.User) error  { s.users[u.ID] = u; return nil }
func (s *stubUserRepo) Delete(_ context.Context, id uuid.UUID) error    { delete(s.users, id); return nil }

type stubVaultRepo struct {
	vaults  map[uuid.UUID]*domain.Vault
	members map[string]domain.VaultRole // vaultID:userID -> role
	deleted int
	moved   int
}

func (s *stubVaultRepo) Create(_ context.Context, v *domain.Vault) error {
	s.vaults[v.ID] = v
	return nil
}
func (s *stubVaultRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Vault, error) {
	if v, ok := s.vaults[id]; ok {
		return v, nil
	}
	return nil, domain.ErrNotFound
}
func (s *stubVaultRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]domain.Vault, error) {
	var out []domain.Vault
	for vid, v := range s.vaults {
		if _, ok := s.members[vid.String()+":"+userID.String()]; ok {
			out = append(out, *v)
		}
	}
	return out, nil
}
func (s *stubVaultRepo) AddMember(_ context.Context, m *domain.VaultMember) error {
	s.members[m.VaultID.String()+":"+m.UserID.String()] = m.Role
	return nil
}
func (s *stubVaultRepo) GetMemberRole(_ context.Context, vaultID, userID uuid.UUID) (domain.VaultRole, error) {
	if role, ok := s.members[vaultID.String()+":"+userID.String()]; ok {
		return role, nil
	}
	return "", domain.ErrForbidden
}
func (s *stubVaultRepo) ListMembers(_ context.Context, _ uuid.UUID) ([]domain.VaultMember, error) {
	return nil, nil
}
func (s *stubVaultRepo) CreateItem(_ context.Context, _ *domain.VaultItem) error { return nil }
func (s *stubVaultRepo) GetItemByID(_ context.Context, _, _ uuid.UUID) (*domain.VaultItem, error) {
	return nil, domain.ErrNotFound
}
func (s *stubVaultRepo) ListItemsByVault(_ context.Context, _ uuid.UUID, _ *domain.ItemKind) ([]domain.VaultItem, error) {
	return nil, nil
}
func (s *stubVaultRepo) UpdateItem(_ context.Context, _ *domain.VaultItem) error { return nil }
func (s *stubVaultRepo) DeleteItem(_ context.Context, _, _ uuid.UUID) error {
	s.deleted++
	return nil
}
func (s *stubVaultRepo) MoveItem(_ context.Context, _, _, _ uuid.UUID) error {
	s.moved++
	return nil
}

func TestSecurityPresenceIsolationAndRelayHostOwnership(t *testing.T) {
	ctx := context.Background()
	aliceID := uuid.New()
	bobID := uuid.New()
	personalVaultID := uuid.New()

	users := &stubUserRepo{users: map[uuid.UUID]*domain.User{
		aliceID: {ID: aliceID, Email: "alice@example.com", Name: "Alice", Role: "user"},
		bobID:   {ID: bobID, Email: "bob@example.com", Name: "Bob", Role: "user"},
	}}
	vaults := &stubVaultRepo{
		vaults:  map[uuid.UUID]*domain.Vault{personalVaultID: {ID: personalVaultID, OwnerID: aliceID, Type: domain.VaultTypePersonal}},
		members: map[string]domain.VaultRole{personalVaultID.String() + ":" + aliceID.String(): domain.VaultRoleOwner},
	}

	presence := NewPresenceUseCase(vaults, users)
	_, err := presence.Heartbeat(ctx, aliceID, "dev-alice", "Alice Laptop", "desktop", []ActiveSessionInfo{
		{SessionID: "sess-alice-secret", VaultID: personalVaultID.String(), HostLabel: "prod-db"},
	})
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}

	// 1. Bob (not in Alice's vault and not admin) must NOT see Alice's device or session_id
	bobView, err := presence.ListPresence(ctx, bobID, "")
	if err != nil {
		t.Fatalf("ListPresence failed: %v", err)
	}
	if len(bobView) != 0 {
		t.Fatalf("expected Bob to see 0 devices, got %d", len(bobView))
	}

	// 2. Bob must NOT be able to hijack Alice's session as role=host
	if _, ok := presence.ValidateRelayJoin("sess-alice-secret", bobID, "host", ""); ok {
		t.Fatalf("expected Bob role=host hijack to be rejected")
	}
	if mode, ok := presence.ValidateRelayJoin("sess-alice-secret", aliceID, "host", ""); !ok || mode != "host" {
		t.Fatalf("expected Alice role=host to succeed, got ok=%v mode=%q", ok, mode)
	}
}

func TestSecurityTeamVaultRBACAndAuthBounds(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	adminID := uuid.New()
	memberID := uuid.New()
	outsiderID := uuid.New()
	teamVaultID := uuid.New()
	personalVaultID := uuid.New()

	users := &stubUserRepo{users: map[uuid.UUID]*domain.User{
		ownerID:    {ID: ownerID, Email: "owner@example.com", Name: "Owner"},
		adminID:    {ID: adminID, Email: "admin@example.com", Name: "Admin"},
		memberID:   {ID: memberID, Email: "member@example.com", Name: "Member"},
		outsiderID: {ID: outsiderID, Email: "outsider@example.com", Name: "Outsider"},
	}}
	vaults := &stubVaultRepo{
		vaults: map[uuid.UUID]*domain.Vault{
			teamVaultID:     {ID: teamVaultID, OwnerID: ownerID, Type: domain.VaultTypeTeam, EncryptedKey: "key"},
			personalVaultID: {ID: personalVaultID, OwnerID: memberID, Type: domain.VaultTypePersonal, EncryptedKey: "pkey"},
		},
		members: map[string]domain.VaultRole{
			teamVaultID.String() + ":" + ownerID.String():      domain.VaultRoleOwner,
			teamVaultID.String() + ":" + adminID.String():      domain.VaultRoleAdmin,
			teamVaultID.String() + ":" + memberID.String():     domain.VaultRoleMember,
			personalVaultID.String() + ":" + memberID.String(): domain.VaultRoleOwner,
		},
	}

	vaultUC := NewVaultUseCase(vaults, users)

	// Member cannot delete or move Team Vault items
	if err := vaultUC.DeleteItem(ctx, memberID, teamVaultID, uuid.New()); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when member deletes team item, got %v", err)
	}
	if err := vaultUC.MoveItem(ctx, memberID, teamVaultID, personalVaultID, uuid.New()); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when member moves team item, got %v", err)
	}

	// Admin cannot escalate someone to owner or pass invalid role
	if _, err := vaultUC.InviteMember(ctx, adminID, teamVaultID, "outsider@example.com", domain.VaultRoleOwner); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when admin assigns owner role, got %v", err)
	}
	if _, err := vaultUC.InviteMember(ctx, ownerID, teamVaultID, "outsider@example.com", "superadmin"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for invalid role enum, got %v", err)
	}

	// Auth password length bounds & email normalization
	authUC := NewAuthUseCase(users, "test-jwt-secret-32-bytes-minimum!", time.Hour, 24*time.Hour)
	if _, _, _, err := authUC.Register(ctx, "short@example.com", "1234567", "Short"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for <8 char password, got %v", err)
	}
	if _, _, _, err := authUC.Register(ctx, "long@example.com", strings.Repeat("a", 73), "Long"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for >72 byte password, got %v", err)
	}
	u, _, _, err := authUC.Register(ctx, "  MixedCase@Example.COM ", "ValidPass123!", "  User Name ")
	if err != nil || u.Email != "mixedcase@example.com" || u.Name != "User Name" {
		t.Fatalf("expected normalized user registration, got u=%+v err=%v", u, err)
	}
}
