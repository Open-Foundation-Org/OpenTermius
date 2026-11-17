package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/opentermius/opentermius/internal/domain"
	"github.com/opentermius/opentermius/internal/ports"
	"github.com/opentermius/opentermius/internal/usecase"
)

type relayPeer struct {
	conn       *websocket.Conn
	userID     string
	userName   string
	mode       string // "host" | "view" | "control"
	lastTyping time.Time
	mu         sync.Mutex
}

func (p *relayPeer) send(msgType int, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.conn.WriteMessage(msgType, data)
}

type relayRoom struct {
	mu      sync.RWMutex
	host    *relayPeer
	viewers map[*relayPeer]struct{}
}

type CollabHandler struct {
	presenceUC  *usecase.PresenceUseCase
	authService ports.AuthService
	upgrader    websocket.Upgrader
	roomsMu     sync.Mutex
	rooms       map[string]*relayRoom
}

func NewCollabHandler(presenceUC *usecase.PresenceUseCase, authService ports.AuthService) *CollabHandler {
	return &CollabHandler{
		presenceUC:  presenceUC,
		authService: authService,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		rooms: make(map[string]*relayRoom),
	}
}

func (h *CollabHandler) getOrCreateRoom(sessionID string) *relayRoom {
	h.roomsMu.Lock()
	defer h.roomsMu.Unlock()
	room, ok := h.rooms[sessionID]
	if !ok {
		room = &relayRoom{viewers: make(map[*relayPeer]struct{})}
		h.rooms[sessionID] = room
	}
	return room
}

type heartbeatRequest struct {
	DeviceID       string                      `json:"device_id"`
	DeviceName     string                      `json:"device_name"`
	Platform       string                      `json:"platform"`
	ActiveSessions []usecase.ActiveSessionInfo `json:"active_sessions"`
}

func (h *CollabHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	var req heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid heartbeat payload")
		return
	}

	pending, err := h.presenceUC.Heartbeat(r.Context(), uid, req.DeviceID, req.DeviceName, req.Platform, req.ActiveSessions)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to record heartbeat")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "online",
		"pending_requests": pending,
	})
}

func (h *CollabHandler) ListPresence(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	vaultID := r.URL.Query().Get("vault_id")

	devices, err := h.presenceUC.ListPresence(r.Context(), uid, vaultID)
	if err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			writeJSONError(w, http.StatusForbidden, "access denied to vault presence")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			writeJSONError(w, http.StatusBadRequest, "invalid vault_id")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to list presence")
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

type requestRemoteBody struct {
	Mode string `json:"mode"`
}

func (h *CollabHandler) RequestRemote(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "sessionId is required")
		return
	}

	var body requestRemoteBody
	_ = json.NewDecoder(r.Body).Decode(&body)

	req, err := h.presenceUC.RequestRemote(r.Context(), uid, sessionID, body.Mode)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "active session not found or offline")
			return
		}
		if errors.Is(err, domain.ErrForbidden) {
			writeJSONError(w, http.StatusForbidden, "you must belong to the same vault to request remote")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to create remote request")
		return
	}

	writeJSON(w, http.StatusCreated, req)
}

func (h *CollabHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	requestID := r.PathValue("requestId")

	req, err := h.presenceUC.GetRequest(uid, requestID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "request not found")
			return
		}
		if errors.Is(err, domain.ErrForbidden) {
			writeJSONError(w, http.StatusForbidden, "access denied")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to get request")
		return
	}

	writeJSON(w, http.StatusOK, req)
}

type respondRemoteBody struct {
	Action string `json:"action"` // "approve" | "reject"
	Mode   string `json:"mode"`   // "view" | "control"
}

func (h *CollabHandler) RespondRemote(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	requestID := r.PathValue("requestId")

	var body respondRemoteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req, err := h.presenceUC.RespondRemote(uid, requestID, body.Action, body.Mode)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "request not found")
			return
		}
		if errors.Is(err, domain.ErrForbidden) {
			writeJSONError(w, http.StatusForbidden, "only the session owner can respond")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to respond to request")
		return
	}

	writeJSON(w, http.StatusOK, req)
}

// RelayWS upgrades to a binary WebSocket relay that forwards encrypted PTY frames
// directly between the session host (russh) and approved team viewers.
// The Gateway never decrypts or initiates any SSH connection.
func (h *CollabHandler) RelayWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	token := r.URL.Query().Get("token")
	role := r.URL.Query().Get("role")
	requestID := r.URL.Query().Get("request_id")

	if sessionID == "" || token == "" {
		writeJSONError(w, http.StatusUnauthorized, "sessionId and token are required")
		return
	}

	userID, err := h.authService.ValidateToken(r.Context(), token)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	if role != "host" {
		role = "viewer"
	}

	mode, allowed := h.presenceUC.ValidateRelayJoin(sessionID, userID, role, requestID)
	if !allowed {
		writeJSONError(w, http.StatusForbidden, "remote relay access not approved")
		return
	}

	userName := h.presenceUC.GetUserName(r.Context(), userID)

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	peer := &relayPeer{conn: conn, userID: userID.String(), userName: userName, mode: mode}
	room := h.getOrCreateRoom(sessionID)

	broadcastState := func() {
		room.mu.RLock()
		hostPeer := room.host
		participants := make([]usecase.RelayParticipant, 0, len(room.viewers)+1)
		targets := make([]*relayPeer, 0, len(room.viewers)+1)
		if hostPeer != nil {
			participants = append(participants, usecase.RelayParticipant{
				UserID:   hostPeer.userID,
				UserName: hostPeer.userName,
				Mode:     "host",
			})
			targets = append(targets, hostPeer)
		}
		for v := range room.viewers {
			participants = append(participants, usecase.RelayParticipant{
				UserID:   v.userID,
				UserName: v.userName,
				Mode:     v.mode,
			})
			targets = append(targets, v)
		}
		viewerCount := len(room.viewers)
		room.mu.RUnlock()

		h.presenceUC.SetSessionViewers(sessionID, participants)
		ctrlMsg, _ := json.Marshal(map[string]any{
			"type":    "relay_state",
			"count":   viewerCount,
			"viewers": participants,
		})
		for _, t := range targets {
			_ = t.send(websocket.TextMessage, ctrlMsg)
		}
	}

	if mode == "host" {
		room.mu.Lock()
		if room.host != nil {
			_ = room.host.conn.Close()
		}
		room.host = peer
		room.mu.Unlock()
		broadcastState()
	} else {
		room.mu.Lock()
		room.viewers[peer] = struct{}{}
		room.mu.Unlock()
		broadcastState()
	}

	defer func() {
		_ = conn.Close()
		room.mu.Lock()
		if mode == "host" {
			if room.host == peer {
				room.host = nil
			}
			for v := range room.viewers {
				_ = v.conn.Close()
			}
			room.viewers = make(map[*relayPeer]struct{})
		} else {
			delete(room.viewers, peer)
		}
		empty := room.host == nil && len(room.viewers) == 0
		room.mu.Unlock()

		if empty {
			h.presenceUC.SetSessionViewers(sessionID, nil)
			h.roomsMu.Lock()
			delete(h.rooms, sessionID)
			h.roomsMu.Unlock()
		} else {
			broadcastState()
		}
	}()

	broadcastTyping := func(sender *relayPeer) {
		now := time.Now()
		if now.Sub(sender.lastTyping) < 700*time.Millisecond {
			return
		}
		sender.lastTyping = now

		typingMsg, _ := json.Marshal(map[string]any{
			"type":      "typing",
			"user_id":   sender.userID,
			"user_name": sender.userName,
		})

		room.mu.RLock()
		targets := make([]*relayPeer, 0, len(room.viewers)+1)
		if room.host != nil && room.host != sender {
			targets = append(targets, room.host)
		}
		for v := range room.viewers {
			if v != sender {
				targets = append(targets, v)
			}
		}
		room.mu.RUnlock()

		for _, t := range targets {
			_ = t.send(websocket.TextMessage, typingMsg)
		}
	}

	for {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if mode == "host" {
			if msgType == websocket.TextMessage {
				broadcastTyping(peer)
				continue
			}
			// Broadcast host PTY output frames to all approved viewers
			room.mu.RLock()
			viewers := make([]*relayPeer, 0, len(room.viewers))
			for v := range room.viewers {
				viewers = append(viewers, v)
			}
			room.mu.RUnlock()

			for _, v := range viewers {
				_ = v.send(msgType, payload)
			}
		} else if mode == "control" {
			if msgType == websocket.BinaryMessage {
				broadcastTyping(peer)
			}
			// Forward viewer input bytes to session host (dropped if mode == "view")
			room.mu.RLock()
			hostPeer := room.host
			room.mu.RUnlock()

			if hostPeer != nil {
				_ = hostPeer.send(msgType, payload)
			}
		}
	}
}

// Keep unused import check happy if uuid needed
var _ = uuid.Nil
