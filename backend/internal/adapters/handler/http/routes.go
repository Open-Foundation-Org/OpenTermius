package http

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opentermius/opentermius/internal/ports"
)

type contextKey string

const userIDKey contextKey = "user_id"

type ipRateLimiter struct {
	mu      sync.Mutex
	hits    map[string][]time.Time
	limit   int
	window  time.Duration
	lastGC  time.Time
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		hits:   make(map[string][]time.Time),
		limit:  limit,
		window: window,
		lastGC: time.Now(),
	}
}

func (l *ipRateLimiter) allow(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastGC) > l.window {
		for k, times := range l.hits {
			var kept []time.Time
			for _, t := range times {
				if t.After(cutoff) {
					kept = append(kept, t)
				}
			}
			if len(kept) == 0 {
				delete(l.hits, k)
			} else {
				l.hits[k] = kept
			}
		}
		l.lastGC = now
	}

	var recent []time.Time
	for _, t := range l.hits[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.limit {
		l.hits[ip] = recent
		return false
	}
	l.hits[ip] = append(recent, now)
	return true
}

type Router struct {
	mux                 *http.ServeMux
	authService         ports.AuthService
	authHandler         *AuthHandler
	vaultHandler        *VaultHandler
	collabHandler       *CollabHandler
	smtpHandler         *SMTPHandler
	authLimiter         *ipRateLimiter
	adminEmail          string
	allowPublicRegister bool
	allowedOrigins      map[string]bool
}

func NewRouter(
	authService ports.AuthService,
	authHandler *AuthHandler,
	vaultHandler *VaultHandler,
	collabHandler *CollabHandler,
	smtpHandler *SMTPHandler,
) *Router {
	adminEmail := strings.ToLower(strings.TrimSpace(os.Getenv("DEFAULT_ADMIN_EMAIL")))
	if adminEmail == "" {
		adminEmail = "admin@opentermius.local"
	}
	allowPublic := strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_PUBLIC_REGISTER")), "true")

	allowedOrigins := make(map[string]bool)
	if raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS")); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				allowedOrigins[trimmed] = true
			}
		}
		// Always allow Tauri native desktop & Android WebView origins
		allowedOrigins["tauri://localhost"] = true
		allowedOrigins["http://tauri.localhost"] = true
		allowedOrigins["https://tauri.localhost"] = true
	}

	r := &Router{
		mux:                 http.NewServeMux(),
		authService:         authService,
		authHandler:         authHandler,
		vaultHandler:        vaultHandler,
		collabHandler:       collabHandler,
		smtpHandler:         smtpHandler,
		authLimiter:         newIPRateLimiter(20, time.Minute),
		adminEmail:          adminEmail,
		allowPublicRegister: allowPublic,
		allowedOrigins:      allowedOrigins,
	}
	r.registerRoutes()
	return r
}

func (r *Router) registerRoutes() {
	// Public Auth Endpoints (rate-limited)
	r.mux.Handle("POST /api/v1/auth/register", r.rateLimitAuth(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.allowPublicRegister {
			writeJSONError(w, http.StatusForbidden, "public registration is disabled; contact your Gateway admin")
			return
		}
		r.authHandler.Register(w, req)
	})))
	r.mux.Handle("POST /api/v1/auth/login", r.rateLimitAuth(http.HandlerFunc(r.authHandler.Login)))
	r.mux.Handle("POST /api/v1/auth/refresh", r.rateLimitAuth(http.HandlerFunc(r.authHandler.Refresh)))

	// Protected Profile & Admin-Only User Management (Gateway Admin Console)
	r.mux.Handle("GET /api/v1/auth/me", r.authMiddleware(http.HandlerFunc(r.authHandler.Me)))
	r.mux.Handle("GET /api/v1/users", r.adminMiddleware(http.HandlerFunc(r.authHandler.ListUsers)))
	r.mux.Handle("POST /api/v1/users", r.adminMiddleware(http.HandlerFunc(r.authHandler.CreateUser)))
	r.mux.Handle("DELETE /api/v1/users/{id}", r.adminMiddleware(http.HandlerFunc(r.authHandler.DeleteUser)))

	// SMTP Settings & Email Invites
	if r.smtpHandler != nil {
		r.mux.Handle("GET /api/v1/settings/smtp", r.adminMiddleware(http.HandlerFunc(r.smtpHandler.GetSettings)))
		r.mux.Handle("PUT /api/v1/settings/smtp", r.adminMiddleware(http.HandlerFunc(r.smtpHandler.UpdateSettings)))
		r.mux.Handle("POST /api/v1/settings/smtp/test", r.adminMiddleware(http.HandlerFunc(r.smtpHandler.TestSettings)))
		r.mux.Handle("GET /api/v1/invites", r.adminMiddleware(http.HandlerFunc(r.smtpHandler.ListInvites)))
		r.mux.Handle("POST /api/v1/invites", r.adminMiddleware(http.HandlerFunc(r.smtpHandler.CreateInvite)))
		r.mux.Handle("POST /api/v1/invites/{id}/resend", r.adminMiddleware(http.HandlerFunc(r.smtpHandler.ResendInvite)))
		r.mux.Handle("DELETE /api/v1/invites/{id}", r.adminMiddleware(http.HandlerFunc(r.smtpHandler.DeleteInvite)))
		// Public invite verification & password setup (rate-limited)
		r.mux.Handle("GET /api/v1/invites/verify", r.rateLimitAuth(http.HandlerFunc(r.smtpHandler.VerifyInvite)))
		r.mux.Handle("POST /api/v1/invites/accept", r.rateLimitAuth(http.HandlerFunc(r.smtpHandler.AcceptInvite)))
	}

	// Protected Vaults
	r.mux.Handle("GET /api/v1/vaults", r.authMiddleware(http.HandlerFunc(r.vaultHandler.ListVaults)))
	r.mux.Handle("POST /api/v1/vaults", r.authMiddleware(http.HandlerFunc(r.vaultHandler.CreateVault)))
	r.mux.Handle("GET /api/v1/vaults/{id}", r.authMiddleware(http.HandlerFunc(r.vaultHandler.GetVault)))

	// Protected Vault Items
	r.mux.Handle("GET /api/v1/vaults/{id}/items", r.authMiddleware(http.HandlerFunc(r.vaultHandler.ListItems)))
	r.mux.Handle("POST /api/v1/vaults/{id}/items", r.authMiddleware(http.HandlerFunc(r.vaultHandler.CreateItem)))
	r.mux.Handle("PUT /api/v1/vaults/{id}/items/{itemId}", r.authMiddleware(http.HandlerFunc(r.vaultHandler.UpdateItem)))
	r.mux.Handle("PUT /api/v1/vaults/{id}/items/{itemId}/move", r.authMiddleware(http.HandlerFunc(r.vaultHandler.MoveItem)))
	r.mux.Handle("DELETE /api/v1/vaults/{id}/items/{itemId}", r.authMiddleware(http.HandlerFunc(r.vaultHandler.DeleteItem)))

	// Protected Vault Members
	r.mux.Handle("GET /api/v1/vaults/{id}/members", r.authMiddleware(http.HandlerFunc(r.vaultHandler.ListMembers)))
	r.mux.Handle("POST /api/v1/vaults/{id}/members", r.authMiddleware(http.HandlerFunc(r.vaultHandler.InviteMember)))

	// Protected Presence & Remote Collaboration Relay (Zero-SSH Blind Relay)
	r.mux.Handle("POST /api/v1/presence/heartbeat", r.authMiddleware(http.HandlerFunc(r.collabHandler.Heartbeat)))
	r.mux.Handle("GET /api/v1/presence", r.authMiddleware(http.HandlerFunc(r.collabHandler.ListPresence)))
	r.mux.Handle("POST /api/v1/sessions/{sessionId}/request", r.authMiddleware(http.HandlerFunc(r.collabHandler.RequestRemote)))
	r.mux.Handle("GET /api/v1/sessions/requests/{requestId}", r.authMiddleware(http.HandlerFunc(r.collabHandler.GetRequest)))
	r.mux.Handle("POST /api/v1/sessions/requests/{requestId}/respond", r.authMiddleware(http.HandlerFunc(r.collabHandler.RespondRemote)))
	r.mux.HandleFunc("GET /api/v1/relay/{sessionId}", r.collabHandler.RelayWS)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Global CORS Handler (respects CORS_ALLOWED_ORIGINS when configured)
	origin := req.Header.Get("Origin")
	if len(r.allowedOrigins) == 0 {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else if origin != "" && r.allowedOrigins[origin] {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	r.mux.ServeHTTP(w, req)
}

func clientIP(req *http.Request) string {
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

func (r *Router) rateLimitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.authLimiter.allow(clientIP(req)) {
			writeJSONError(w, http.StatusTooManyRequests, "too many authentication attempts; try again in a minute")
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *Router) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		userID, err := r.authService.ValidateToken(req.Context(), tokenString)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := context.WithValue(req.Context(), userIDKey, userID.String())
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func (r *Router) adminMiddleware(next http.Handler) http.Handler {
	return r.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		uid := getUserID(req)
		if uid == uuid.Nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := r.authService.GetProfile(req.Context(), uid)
		if err != nil || user == nil || !strings.EqualFold(strings.TrimSpace(user.Email), r.adminEmail) {
			writeJSONError(w, http.StatusForbidden, "gateway admin privileges required")
			return
		}
		next.ServeHTTP(w, req)
	}))
}

func getUserID(r *http.Request) uuid.UUID {
	val, ok := r.Context().Value(userIDKey).(string)
	if !ok || val == "" {
		return uuid.Nil
	}
	uid, _ := uuid.Parse(val)
	return uid
}

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
