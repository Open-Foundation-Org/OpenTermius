package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opentermius/opentermius/internal/domain"
	"github.com/opentermius/opentermius/internal/ports"
)

func generateRandomVaultSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return uuid.NewString() + uuid.NewString()
	}
	return hex.EncodeToString(buf)
}

type VaultUseCase struct {
	vaultRepo ports.VaultRepository
	userRepo  ports.UserRepository
}

func NewVaultUseCase(vaultRepo ports.VaultRepository, userRepo ports.UserRepository) *VaultUseCase {
	return &VaultUseCase{
		vaultRepo: vaultRepo,
		userRepo:  userRepo,
	}
}

func (v *VaultUseCase) CreateVault(ctx context.Context, ownerID uuid.UUID, name string, vaultType domain.VaultType, encryptedKey string) (*domain.Vault, error) {
	if name == "" || encryptedKey == "" {
		return nil, domain.ErrInvalidInput
	}

	vault := &domain.Vault{
		ID:           uuid.New(),
		OwnerID:      ownerID,
		Name:         name,
		Type:         vaultType,
		EncryptedKey: encryptedKey,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := v.vaultRepo.Create(ctx, vault); err != nil {
		return nil, err
	}

	// Add owner as vault member
	member := &domain.VaultMember{
		ID:                uuid.New(),
		VaultID:           vault.ID,
		UserID:            ownerID,
		Role:              domain.VaultRoleOwner,
		EncryptedVaultKey: encryptedKey,
		CreatedAt:         time.Now().UTC(),
	}
	if err := v.vaultRepo.AddMember(ctx, member); err != nil {
		return nil, err
	}

	return vault, nil
}

func (v *VaultUseCase) ListUserVaults(ctx context.Context, userID uuid.UUID) ([]domain.Vault, error) {
	vaults, err := v.vaultRepo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(vaults) == 0 && userID != uuid.Nil {
		personal, createErr := v.CreateVault(ctx, userID, "Personal Vault", domain.VaultTypePersonal, generateRandomVaultSecret())
		if createErr == nil && personal != nil {
			return []domain.Vault{*personal}, nil
		}
	}
	return vaults, nil
}

func (v *VaultUseCase) GetVault(ctx context.Context, vaultID, userID uuid.UUID) (*domain.Vault, error) {
	_, err := v.vaultRepo.GetMemberRole(ctx, vaultID, userID)
	if err != nil {
		return nil, domain.ErrForbidden
	}
	return v.vaultRepo.GetByID(ctx, vaultID)
}

func (v *VaultUseCase) CreateItem(ctx context.Context, userID, vaultID uuid.UUID, kind domain.ItemKind, label, ciphertext, nonce string) (*domain.VaultItem, error) {
	_, err := v.vaultRepo.GetMemberRole(ctx, vaultID, userID)
	if err != nil {
		return nil, domain.ErrForbidden
	}

	if label == "" || ciphertext == "" || nonce == "" {
		return nil, domain.ErrInvalidInput
	}

	item := &domain.VaultItem{
		ID:         uuid.New(),
		VaultID:    vaultID,
		Kind:       kind,
		Label:      label,
		Ciphertext: ciphertext,
		Nonce:      nonce,
		Version:    1,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	if err := v.vaultRepo.CreateItem(ctx, item); err != nil {
		return nil, err
	}

	return item, nil
}

func (v *VaultUseCase) ListItems(ctx context.Context, userID, vaultID uuid.UUID, kind *domain.ItemKind) ([]domain.VaultItem, error) {
	_, err := v.vaultRepo.GetMemberRole(ctx, vaultID, userID)
	if err != nil {
		return nil, domain.ErrForbidden
	}
	return v.vaultRepo.ListItemsByVault(ctx, vaultID, kind)
}

func (v *VaultUseCase) UpdateItem(ctx context.Context, userID, vaultID, itemID uuid.UUID, label, ciphertext, nonce string) (*domain.VaultItem, error) {
	role, err := v.vaultRepo.GetMemberRole(ctx, vaultID, userID)
	if err != nil {
		return nil, domain.ErrForbidden
	}
	if role == domain.VaultRoleMember {
		// members may have read-only in some configurations, but can edit if permitted
	}

	existing, err := v.vaultRepo.GetItemByID(ctx, vaultID, itemID)
	if err != nil {
		return nil, err
	}

	existing.Label = label
	existing.Ciphertext = ciphertext
	existing.Nonce = nonce
	existing.Version++
	existing.UpdatedAt = time.Now().UTC()

	if err := v.vaultRepo.UpdateItem(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}

func (v *VaultUseCase) DeleteItem(ctx context.Context, userID, vaultID, itemID uuid.UUID) error {
	role, err := v.vaultRepo.GetMemberRole(ctx, vaultID, userID)
	if err != nil {
		return domain.ErrForbidden
	}
	if role == domain.VaultRoleMember {
		if vault, vErr := v.vaultRepo.GetByID(ctx, vaultID); vErr == nil && vault != nil && vault.Type == domain.VaultTypeTeam {
			return domain.ErrForbidden
		}
	}
	return v.vaultRepo.DeleteItem(ctx, vaultID, itemID)
}

func (v *VaultUseCase) MoveItem(ctx context.Context, userID, sourceVaultID, targetVaultID, itemID uuid.UUID) error {
	sourceRole, err := v.vaultRepo.GetMemberRole(ctx, sourceVaultID, userID)
	if err != nil {
		return domain.ErrForbidden
	}
	if sourceRole == domain.VaultRoleMember {
		if srcVault, vErr := v.vaultRepo.GetByID(ctx, sourceVaultID); vErr == nil && srcVault != nil && srcVault.Type == domain.VaultTypeTeam {
			return domain.ErrForbidden
		}
	}
	if _, err := v.vaultRepo.GetMemberRole(ctx, targetVaultID, userID); err != nil {
		return domain.ErrForbidden
	}
	return v.vaultRepo.MoveItem(ctx, sourceVaultID, targetVaultID, itemID)
}

func (v *VaultUseCase) InviteMember(ctx context.Context, callerID, vaultID uuid.UUID, email string, role domain.VaultRole) (*domain.VaultMember, error) {
	callerRole, err := v.vaultRepo.GetMemberRole(ctx, vaultID, callerID)
	if err != nil || (callerRole != domain.VaultRoleOwner && callerRole != domain.VaultRoleAdmin) {
		return nil, domain.ErrForbidden
	}

	normalizedRole := domain.VaultRole(strings.ToLower(strings.TrimSpace(string(role))))
	if normalizedRole == "" {
		normalizedRole = domain.VaultRoleMember
	}
	switch normalizedRole {
	case domain.VaultRoleOwner, domain.VaultRoleAdmin, domain.VaultRoleMember:
	default:
		return nil, domain.ErrInvalidInput
	}
	if normalizedRole == domain.VaultRoleOwner && callerRole != domain.VaultRoleOwner {
		return nil, domain.ErrForbidden
	}

	vault, err := v.vaultRepo.GetByID(ctx, vaultID)
	if err != nil {
		return nil, err
	}

	targetUser, err := v.userRepo.GetByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, domain.ErrNotFound
	}

	member := &domain.VaultMember{
		ID:                uuid.New(),
		VaultID:           vaultID,
		UserID:            targetUser.ID,
		Name:              targetUser.Name,
		Email:             targetUser.Email,
		Role:              normalizedRole,
		EncryptedVaultKey: vault.EncryptedKey,
		CreatedAt:         time.Now().UTC(),
	}

	if err := v.vaultRepo.AddMember(ctx, member); err != nil {
		return nil, err
	}

	return member, nil
}

func (v *VaultUseCase) ListMembers(ctx context.Context, callerID, vaultID uuid.UUID) ([]domain.VaultMember, error) {
	_, err := v.vaultRepo.GetMemberRole(ctx, vaultID, callerID)
	if err != nil {
		return nil, domain.ErrForbidden
	}
	return v.vaultRepo.ListMembers(ctx, vaultID)
}

func (v *VaultUseCase) RemoveMember(ctx context.Context, callerID, vaultID, targetUserID uuid.UUID) error {
	callerRole, err := v.vaultRepo.GetMemberRole(ctx, vaultID, callerID)
	if err != nil || (callerRole != domain.VaultRoleOwner && callerRole != domain.VaultRoleAdmin) {
		return domain.ErrForbidden
	}
	targetRole, err := v.vaultRepo.GetMemberRole(ctx, vaultID, targetUserID)
	if err != nil {
		return domain.ErrNotFound
	}
	if targetRole == domain.VaultRoleOwner {
		return domain.ErrForbidden
	}
	if callerRole == domain.VaultRoleAdmin && targetRole != domain.VaultRoleMember {
		return domain.ErrForbidden
	}
	return v.vaultRepo.RemoveMember(ctx, vaultID, targetUserID)
}
