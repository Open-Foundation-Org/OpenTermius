package usecase

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/opentermius/opentermius/internal/domain"
)

type countingUserRepo struct {
	calls int
	user  *domain.User
}

func (s *countingUserRepo) Create(ctx context.Context, user *domain.User) error { return nil }
func (s *countingUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return s.user, nil
}
func (s *countingUserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	s.calls++
	return s.user, nil
}
func (s *countingUserRepo) ListAll(ctx context.Context) ([]domain.User, error) { return nil, nil }
func (s *countingUserRepo) Update(ctx context.Context, user *domain.User) error { return nil }
func (s *countingUserRepo) Delete(ctx context.Context, id uuid.UUID) error      { return nil }

func TestHeartbeatUserCacheAndOneTimeTicket(t *testing.T) {
	uid := uuid.New()
	uRepo := &countingUserRepo{
		user: &domain.User{
			ID:    uid,
			Name:  "Alice",
			Email: "alice@example.com",
			Role:  "admin",
		},
	}
	uc := NewPresenceUseCase(nil, uRepo)

	// 1. Multiple heartbeats must hit userRepo only once due to 10-minute in-memory cache
	for i := 0; i < 5; i++ {
		_, err := uc.Heartbeat(context.Background(), uid, "dev-1", "MacBook", "desktop", []ActiveSessionInfo{
			{SessionID: "sess-1", HostLabel: "prod-1"},
		})
		if err != nil {
			t.Fatalf("heartbeat failed: %v", err)
		}
	}
	if uRepo.calls != 1 {
		t.Fatalf("expected 1 SQL user lookup across 5 heartbeats, got %d", uRepo.calls)
	}

	// 2. Issue one-time relay ticket for session owner
	ticket, err := uc.IssueRelayTicket("sess-1", uid, "host", "")
	if err != nil {
		t.Fatalf("expected ticket issue to succeed, got %v", err)
	}

	// First redemption succeeds
	gotUID, mode, ok := uc.RedeemRelayTicket("sess-1", ticket.Token)
	if !ok || gotUID != uid || mode != "host" {
		t.Fatalf("expected first ticket redemption to succeed, got ok=%v mode=%s", ok, mode)
	}

	// Second redemption (replay) must fail immediately
	if _, _, ok := uc.RedeemRelayTicket("sess-1", ticket.Token); ok {
		t.Fatalf("expected replayed ticket to be rejected")
	}
}
