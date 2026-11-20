package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/opentermius/opentermius/internal/domain"
	"github.com/opentermius/opentermius/internal/ports"
	"golang.org/x/crypto/bcrypt"
)

type AuthUseCase struct {
	userRepo   ports.UserRepository
	jwtSecret  []byte
	tokenTTL   time.Duration
	refreshTTL time.Duration
}

func NewAuthUseCase(userRepo ports.UserRepository, jwtSecret string, tokenTTL, refreshTTL time.Duration) *AuthUseCase {
	if tokenTTL <= 0 {
		tokenTTL = 24 * time.Hour
	}
	if refreshTTL <= 0 {
		refreshTTL = 90 * 24 * time.Hour
	}
	return &AuthUseCase{
		userRepo:   userRepo,
		jwtSecret:  []byte(jwtSecret),
		tokenTTL:   tokenTTL,
		refreshTTL: refreshTTL,
	}
}

func (a *AuthUseCase) Register(ctx context.Context, email, password, name string) (*domain.User, string, string, error) {
	if email == "" || password == "" || name == "" {
		return nil, "", "", domain.ErrInvalidInput
	}

	existing, err := a.userRepo.GetByEmail(ctx, email)
	if err == nil && existing != nil {
		return nil, "", "", domain.ErrConflict
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", "", err
	}

	user := &domain.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: string(hash),
		Name:         name,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := a.userRepo.Create(ctx, user); err != nil {
		return nil, "", "", err
	}

	accessToken, refreshToken, err := a.generateTokenPair(user.ID)
	if err != nil {
		return nil, "", "", err
	}

	return user, accessToken, refreshToken, nil
}

func (a *AuthUseCase) Login(ctx context.Context, email, password string) (*domain.User, string, string, error) {
	user, err := a.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, "", "", domain.ErrUnauthorized
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", "", domain.ErrUnauthorized
	}

	accessToken, refreshToken, err := a.generateTokenPair(user.ID)
	if err != nil {
		return nil, "", "", err
	}

	return user, accessToken, refreshToken, nil
}

func (a *AuthUseCase) Refresh(ctx context.Context, refreshTokenString string) (*domain.User, string, string, error) {
	if refreshTokenString == "" {
		return nil, "", "", domain.ErrUnauthorized
	}

	uid, typ, err := a.parseToken(refreshTokenString)
	if err != nil || typ != "refresh" {
		return nil, "", "", domain.ErrUnauthorized
	}

	user, err := a.userRepo.GetByID(ctx, uid)
	if err != nil || user == nil {
		return nil, "", "", domain.ErrUnauthorized
	}

	accessToken, newRefreshToken, err := a.generateTokenPair(user.ID)
	if err != nil {
		return nil, "", "", err
	}

	return user, accessToken, newRefreshToken, nil
}

func (a *AuthUseCase) ValidateToken(ctx context.Context, tokenString string) (uuid.UUID, error) {
	uid, typ, err := a.parseToken(tokenString)
	if err != nil {
		return uuid.Nil, domain.ErrUnauthorized
	}
	// Reject refresh tokens on normal API endpoints; accept legacy tokens without typ for smooth upgrade
	if typ == "refresh" {
		return uuid.Nil, domain.ErrUnauthorized
	}
	return uid, nil
}

func (a *AuthUseCase) parseToken(tokenString string) (uuid.UUID, string, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return a.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return uuid.Nil, "", domain.ErrUnauthorized
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return uuid.Nil, "", domain.ErrUnauthorized
	}

	sub, ok := claims["sub"].(string)
	if !ok {
		return uuid.Nil, "", domain.ErrUnauthorized
	}

	uid, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, "", domain.ErrUnauthorized
	}

	typ, _ := claims["typ"].(string)
	return uid, typ, nil
}

func (a *AuthUseCase) GetProfile(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	return a.userRepo.GetByID(ctx, userID)
}

func (a *AuthUseCase) ListUsers(ctx context.Context) ([]domain.User, error) {
	return a.userRepo.ListAll(ctx)
}

func (a *AuthUseCase) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	return a.userRepo.Delete(ctx, userID)
}

func (a *AuthUseCase) generateTokenPair(userID uuid.UUID) (string, string, error) {
	accessToken, err := a.generateSignedToken(userID, "access", a.tokenTTL)
	if err != nil {
		return "", "", err
	}
	refreshToken, err := a.generateSignedToken(userID, "refresh", a.refreshTTL)
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

func (a *AuthUseCase) generateSignedToken(userID uuid.UUID, typ string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": userID.String(),
		"typ": typ,
		"exp": now.Add(ttl).Unix(),
		"iat": now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.jwtSecret)
}
