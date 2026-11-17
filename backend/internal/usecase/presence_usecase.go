package usecase

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opentermius/opentermius/internal/domain"
	"github.com/opentermius/opentermius/internal/ports"
)

type RelayParticipant struct {
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	Mode     string `json:"mode"` // "host" | "control" | "view"
}

type ActiveSessionInfo struct {
	SessionID         string             `json:"session_id"`
	VaultID           string             `json:"vault_id"`
	VaultName         string             `json:"vault_name"`
	HostLabel         string             `json:"host_label"`
	StartedAt         time.Time          `json:"started_at"`
	ApprovedRequestID string             `json:"approved_request_id,omitempty"`
	ApprovedMode      string             `json:"approved_mode,omitempty"`
	Viewers           []RelayParticipant `json:"viewers,omitempty"`
}

type DevicePresence struct {
	UserID         uuid.UUID           `json:"user_id"`
	UserName       string              `json:"user_name"`
	UserEmail      string              `json:"user_email"`
	DeviceID       string              `json:"device_id"`
	DeviceName     string              `json:"device_name"`
	Platform       string              `json:"platform"`
	Online         bool                `json:"online"`
	LastSeenAt     time.Time           `json:"last_seen_at"`
	ActiveSessions []ActiveSessionInfo `json:"active_sessions"`
}

type RemoteRequest struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`
	VaultID        string    `json:"vault_id"`
	HostLabel      string    `json:"host_label"`
	OwnerID        uuid.UUID `json:"owner_id"`
	RequesterID    uuid.UUID `json:"requester_id"`
	RequesterName  string    `json:"requester_name"`
	RequesterEmail string    `json:"requester_email"`
	Mode           string    `json:"mode"`   // "view" | "control"
	Status         string    `json:"status"` // "pending" | "approved" | "rejected"
	CreatedAt      time.Time `json:"created_at"`
}

type PresenceUseCase struct {
	mu             sync.RWMutex
	devices        map[string]*DevicePresence    // key: userID:deviceID
	requests       map[string]*RemoteRequest     // key: requestID
	sessionViewers map[string][]RelayParticipant // key: sessionID
	vaultRepo      ports.VaultRepository
	userRepo       ports.UserRepository
}

func NewPresenceUseCase(vaultRepo ports.VaultRepository, userRepo ports.UserRepository) *PresenceUseCase {
	return &PresenceUseCase{
		devices:        make(map[string]*DevicePresence),
		requests:       make(map[string]*RemoteRequest),
		sessionViewers: make(map[string][]RelayParticipant),
		vaultRepo:      vaultRepo,
		userRepo:       userRepo,
	}
}

func (p *PresenceUseCase) SetSessionViewers(sessionID string, viewers []RelayParticipant) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(viewers) == 0 {
		delete(p.sessionViewers, sessionID)
		return
	}
	cp := make([]RelayParticipant, len(viewers))
	copy(cp, viewers)
	p.sessionViewers[sessionID] = cp
}

func (p *PresenceUseCase) GetUserName(ctx context.Context, userID uuid.UUID) string {
	user, err := p.userRepo.GetByID(ctx, userID)
	if err == nil && user != nil && user.Name != "" {
		return user.Name
	}
	return "Operator"
}

func (p *PresenceUseCase) Heartbeat(
	ctx context.Context,
	userID uuid.UUID,
	deviceID, deviceName, platform string,
	sessions []ActiveSessionInfo,
) ([]RemoteRequest, error) {
	user, err := p.userRepo.GetByID(ctx, userID)
	userName := "Operator"
	userEmail := ""
	if err == nil && user != nil {
		userName = user.Name
		userEmail = user.Email
	}

	if deviceID == "" {
		deviceID = userID.String()
	}
	if deviceName == "" {
		deviceName = "OpenTermius Client"
	}
	if platform == "" {
		platform = "desktop"
	}
	if sessions == nil {
		sessions = []ActiveSessionInfo{}
	}

	now := time.Now().UTC()
	key := userID.String() + ":" + deviceID

	p.mu.Lock()
	defer p.mu.Unlock()

	p.devices[key] = &DevicePresence{
		UserID:         userID,
		UserName:       userName,
		UserEmail:      userEmail,
		DeviceID:       deviceID,
		DeviceName:     deviceName,
		Platform:       platform,
		Online:         true,
		LastSeenAt:     now,
		ActiveSessions: sessions,
	}

	// Return pending remote requests targeted at this user (created in last 2 minutes)
	var pending []RemoteRequest
	for id, req := range p.requests {
		if now.Sub(req.CreatedAt) > 10*time.Minute {
			delete(p.requests, id)
			continue
		}
		if req.OwnerID == userID && req.Status == "pending" {
			pending = append(pending, *req)
		}
	}
	if pending == nil {
		pending = []RemoteRequest{}
	}
	return pending, nil
}

func (p *PresenceUseCase) ListPresence(ctx context.Context, callerID uuid.UUID, vaultIDFilter string) ([]DevicePresence, error) {
	if vaultIDFilter != "" {
		vid, err := uuid.Parse(vaultIDFilter)
		if err != nil {
			return nil, domain.ErrInvalidInput
		}
		if _, err := p.vaultRepo.GetMemberRole(ctx, vid, callerID); err != nil {
			return nil, domain.ErrForbidden
		}
	}

	now := time.Now().UTC()
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]DevicePresence, 0, len(p.devices))
	for _, d := range p.devices {
		isOnline := now.Sub(d.LastSeenAt) <= 35*time.Second
		if !isOnline && now.Sub(d.LastSeenAt) > 10*time.Minute {
			continue
		}
		copyDev := *d
		copyDev.Online = isOnline
		if !isOnline {
			copyDev.ActiveSessions = []ActiveSessionInfo{}
		} else {
			var enriched []ActiveSessionInfo
			for _, s := range d.ActiveSessions {
				if vaultIDFilter != "" && s.VaultID != vaultIDFilter {
					continue
				}
				if viewers, ok := p.sessionViewers[s.SessionID]; ok && len(viewers) > 0 {
					s.Viewers = append([]RelayParticipant(nil), viewers...)
				}
				for _, req := range p.requests {
					if req.SessionID == s.SessionID && req.RequesterID == callerID && req.Status == "approved" {
						s.ApprovedRequestID = req.ID
						s.ApprovedMode = req.Mode
						if req.Mode == "control" {
							break
						}
					}
				}
				enriched = append(enriched, s)
			}
			if enriched == nil {
				enriched = []ActiveSessionInfo{}
			}
			copyDev.ActiveSessions = enriched
		}
		out = append(out, copyDev)
	}
	return out, nil
}

func (p *PresenceUseCase) RequestRemote(ctx context.Context, requesterID uuid.UUID, sessionID, mode string) (*RemoteRequest, error) {
	if mode != "control" {
		mode = "view"
	}

	var ownerID uuid.UUID
	var matchedSession *ActiveSessionInfo
	now := time.Now().UTC()

	p.mu.RLock()
	for _, d := range p.devices {
		if now.Sub(d.LastSeenAt) > 35*time.Second {
			continue
		}
		for i := range d.ActiveSessions {
			if d.ActiveSessions[i].SessionID == sessionID {
				ownerID = d.UserID
				s := d.ActiveSessions[i]
				matchedSession = &s
				break
			}
		}
		if matchedSession != nil {
			break
		}
	}
	p.mu.RUnlock()

	if matchedSession == nil {
		return nil, domain.ErrNotFound
	}

	// Verify both requester and session belong to the same vault
	if matchedSession.VaultID != "" {
		vid, err := uuid.Parse(matchedSession.VaultID)
		if err != nil {
			return nil, domain.ErrForbidden
		}
		if _, err := p.vaultRepo.GetMemberRole(ctx, vid, requesterID); err != nil {
			return nil, domain.ErrForbidden
		}
	} else if ownerID != requesterID {
		return nil, domain.ErrForbidden
	}

	// Reuse already-approved access for this session+user so they don't wait again
	p.mu.RLock()
	for _, existing := range p.requests {
		if existing.SessionID == sessionID && existing.RequesterID == requesterID && existing.Status == "approved" {
			if existing.Mode == "control" || existing.Mode == mode {
				copyReq := *existing
				p.mu.RUnlock()
				return &copyReq, nil
			}
		}
	}
	p.mu.RUnlock()

	reqUser, err := p.userRepo.GetByID(ctx, requesterID)
	reqName := "Teammate"
	reqEmail := ""
	if err == nil && reqUser != nil {
		reqName = reqUser.Name
		reqEmail = reqUser.Email
	}

	req := &RemoteRequest{
		ID:             uuid.NewString(),
		SessionID:      sessionID,
		VaultID:        matchedSession.VaultID,
		HostLabel:      matchedSession.HostLabel,
		OwnerID:        ownerID,
		RequesterID:    requesterID,
		RequesterName:  reqName,
		RequesterEmail: reqEmail,
		Mode:           mode,
		Status:         "pending",
		CreatedAt:      now,
	}

	p.mu.Lock()
	p.requests[req.ID] = req
	p.mu.Unlock()

	copyReq := *req
	return &copyReq, nil
}

func (p *PresenceUseCase) RespondRemote(ownerID uuid.UUID, requestID, action, mode string) (*RemoteRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	req, ok := p.requests[requestID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if req.OwnerID != ownerID {
		return nil, domain.ErrForbidden
	}

	if action == "approve" {
		req.Status = "approved"
		if mode == "view" || mode == "control" {
			req.Mode = mode
		}
	} else {
		req.Status = "rejected"
	}

	copyReq := *req
	return &copyReq, nil
}

func (p *PresenceUseCase) GetRequest(callerID uuid.UUID, requestID string) (*RemoteRequest, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	req, ok := p.requests[requestID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if req.RequesterID != callerID && req.OwnerID != callerID {
		return nil, domain.ErrForbidden
	}
	copyReq := *req
	return &copyReq, nil
}

func (p *PresenceUseCase) ValidateRelayJoin(sessionID string, userID uuid.UUID, role, requestID string) (allowedMode string, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if role == "host" {
		for _, d := range p.devices {
			if d.UserID == userID {
				for _, s := range d.ActiveSessions {
					if s.SessionID == sessionID {
						return "host", true
					}
				}
			}
		}
		// Allow host to pre-open relay even before next heartbeat
		return "host", true
	}

	req, exists := p.requests[requestID]
	if !exists || req.SessionID != sessionID || req.RequesterID != userID || req.Status != "approved" {
		return "", false
	}
	return req.Mode, true
}
