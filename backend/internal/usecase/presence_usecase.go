package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opentermius/opentermius/internal/domain"
	"github.com/opentermius/opentermius/internal/ports"
)

type cachedUserProfile struct {
	name      string
	email     string
	role      string
	expiresAt time.Time
}

type RelayTicket struct {
	Token     string    `json:"ticket"`
	SessionID string    `json:"session_id"`
	UserID    uuid.UUID `json:"user_id"`
	Mode      string    `json:"mode"` // "host" | "view" | "control"
	ExpiresAt time.Time `json:"expires_at"`
}

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
	tickets        map[string]*RelayTicket       // key: random 32-byte hex ticket (20s TTL, one-time use)
	userCache      map[uuid.UUID]cachedUserProfile
	vaultRepo      ports.VaultRepository
	userRepo       ports.UserRepository
}

func NewPresenceUseCase(vaultRepo ports.VaultRepository, userRepo ports.UserRepository) *PresenceUseCase {
	return &PresenceUseCase{
		devices:        make(map[string]*DevicePresence),
		requests:       make(map[string]*RemoteRequest),
		sessionViewers: make(map[string][]RelayParticipant),
		tickets:        make(map[string]*RelayTicket),
		userCache:      make(map[uuid.UUID]cachedUserProfile),
		vaultRepo:      vaultRepo,
		userRepo:       userRepo,
	}
}

func (p *PresenceUseCase) getCachedUser(ctx context.Context, userID uuid.UUID) (name, email, role string) {
	now := time.Now().UTC()
	p.mu.RLock()
	cached, ok := p.userCache[userID]
	p.mu.RUnlock()
	if ok && now.Before(cached.expiresAt) {
		return cached.name, cached.email, cached.role
	}

	user, err := p.userRepo.GetByID(ctx, userID)
	name = "Operator"
	if err == nil && user != nil {
		if user.Name != "" {
			name = user.Name
		}
		email = user.Email
		role = user.Role
		p.mu.Lock()
		if len(p.userCache) > 8192 {
			for k, v := range p.userCache {
				if now.After(v.expiresAt) {
					delete(p.userCache, k)
				}
			}
		}
		p.userCache[userID] = cachedUserProfile{
			name:      name,
			email:     email,
			role:      role,
			expiresAt: now.Add(10 * time.Minute),
		}
		p.mu.Unlock()
	}
	return name, email, role
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

func (p *PresenceUseCase) RevokeSessionApprovals(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, req := range p.requests {
		if req.SessionID == sessionID {
			delete(p.requests, id)
		}
	}
}

func (p *PresenceUseCase) GetUserName(ctx context.Context, userID uuid.UUID) string {
	name, _, _ := p.getCachedUser(ctx, userID)
	return name
}

func (p *PresenceUseCase) Heartbeat(
	ctx context.Context,
	userID uuid.UUID,
	deviceID, deviceName, platform string,
	sessions []ActiveSessionInfo,
) ([]RemoteRequest, error) {
	userName, userEmail, _ := p.getCachedUser(ctx, userID)

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

	for devKey, dev := range p.devices {
		if devKey != key && now.Sub(dev.LastSeenAt) > 30*time.Minute {
			delete(p.devices, devKey)
		}
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
	var allowedVaults map[string]bool
	isAdmin := false

	if vaultIDFilter != "" {
		vid, err := uuid.Parse(vaultIDFilter)
		if err != nil {
			return nil, domain.ErrInvalidInput
		}
		if _, err := p.vaultRepo.GetMemberRole(ctx, vid, callerID); err != nil {
			return nil, domain.ErrForbidden
		}
	} else {
		_, _, callerRole := p.getCachedUser(ctx, callerID)
		if strings.EqualFold(callerRole, "admin") {
			isAdmin = true
		} else {
			allowedVaults = make(map[string]bool)
			if vaults, err := p.vaultRepo.ListByUser(ctx, callerID); err == nil {
				for _, v := range vaults {
					allowedVaults[v.ID.String()] = true
				}
			}
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
		isOwnDevice := d.UserID == callerID
		if vaultIDFilter == "" && !isAdmin && !isOwnDevice && len(allowedVaults) == 0 {
			continue
		}

		copyDev := *d
		copyDev.Online = isOnline
		if !isOnline {
			if vaultIDFilter != "" || (!isAdmin && !isOwnDevice) {
				continue
			}
			copyDev.ActiveSessions = []ActiveSessionInfo{}
		} else {
			var enriched []ActiveSessionInfo
			for _, s := range d.ActiveSessions {
				if vaultIDFilter != "" && s.VaultID != vaultIDFilter {
					continue
				}
				if vaultIDFilter == "" && !isAdmin && !isOwnDevice {
					if s.VaultID == "" || !allowedVaults[s.VaultID] {
						continue
					}
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
			if !isAdmin && !isOwnDevice && len(enriched) == 0 {
				continue
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

	reqName, reqEmail, _ := p.getCachedUser(ctx, requesterID)
	if reqName == "" {
		reqName = "Teammate"
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
			for _, s := range d.ActiveSessions {
				if s.SessionID == sessionID {
					if d.UserID != userID {
						return "", false
					}
					return "host", true
				}
			}
		}
		// Allow session owner to pre-open relay before first heartbeat if unclaimed
		return "host", true
	}

	req, exists := p.requests[requestID]
	if !exists || req.SessionID != sessionID || req.RequesterID != userID || req.Status != "approved" {
		return "", false
	}
	return req.Mode, true
}

func (p *PresenceUseCase) IssueRelayTicket(sessionID string, userID uuid.UUID, role, requestID string) (*RelayTicket, error) {
	if role != "host" {
		role = "viewer"
	}
	mode, allowed := p.ValidateRelayJoin(sessionID, userID, role, requestID)
	if !allowed {
		return nil, domain.ErrForbidden
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(raw)
	now := time.Now().UTC()
	ticket := &RelayTicket{
		Token:     token,
		SessionID: sessionID,
		UserID:    userID,
		Mode:      mode,
		ExpiresAt: now.Add(20 * time.Second),
	}

	p.mu.Lock()
	for k, t := range p.tickets {
		if now.After(t.ExpiresAt) {
			delete(p.tickets, k)
		}
	}
	p.tickets[token] = ticket
	p.mu.Unlock()

	return ticket, nil
}

func (p *PresenceUseCase) RedeemRelayTicket(sessionID, token string) (userID uuid.UUID, mode string, ok bool) {
	if sessionID == "" || token == "" {
		return uuid.Nil, "", false
	}
	now := time.Now().UTC()

	p.mu.Lock()
	defer p.mu.Unlock()

	ticket, exists := p.tickets[token]
	if !exists {
		return uuid.Nil, "", false
	}
	// Single-use: always delete upon redemption attempt
	delete(p.tickets, token)

	if ticket.SessionID != sessionID || now.After(ticket.ExpiresAt) {
		return uuid.Nil, "", false
	}
	return ticket.UserID, ticket.Mode, true
}
