package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opentermius/opentermius/internal/domain"
	"github.com/opentermius/opentermius/internal/usecase"
)

type SMTPHandler struct {
	smtpUC *usecase.SMTPUseCase
}

func NewSMTPHandler(smtpUC *usecase.SMTPUseCase) *SMTPHandler {
	return &SMTPHandler{smtpUC: smtpUC}
}

func (h *SMTPHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.smtpUC.GetSettings(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load SMTP settings")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

type updateSMTPRequest struct {
	Enabled    bool   `json:"enabled"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	FromName   string `json:"from_name"`
	FromEmail  string `json:"from_email"`
	UseTLS     bool   `json:"use_tls"`
	WebBaseURL string `json:"web_base_url"`
}

func (h *SMTPHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSMTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid SMTP settings payload")
		return
	}

	updated, err := h.smtpUC.UpdateSettings(r.Context(), &domain.SMTPSettings{
		Enabled:    req.Enabled,
		Host:       req.Host,
		Port:       req.Port,
		Username:   req.Username,
		Password:   req.Password,
		FromName:   req.FromName,
		FromEmail:  req.FromEmail,
		UseTLS:     req.UseTLS,
		WebBaseURL: req.WebBaseURL,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save SMTP settings")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

type testSMTPRequest struct {
	ToEmail string `json:"to_email"`
}

func (h *SMTPHandler) TestSettings(w http.ResponseWriter, r *http.Request) {
	var req testSMTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ToEmail == "" {
		writeJSONError(w, http.StatusBadRequest, "to_email is required")
		return
	}
	if err := h.smtpUC.SendTestEmail(r.Context(), req.ToEmail); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

type createInviteRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (h *SMTPHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	callerID := getUserID(r)
	var req createInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fallbackOrigin := r.Header.Get("Origin")
	res, err := h.smtpUC.CreateInvite(r.Context(), callerID, req.Email, req.Name, fallbackOrigin)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			writeJSONError(w, http.StatusConflict, "email is already registered")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			writeJSONError(w, http.StatusBadRequest, "name and email are required")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to create invite")
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (h *SMTPHandler) ListInvites(w http.ResponseWriter, r *http.Request) {
	list, err := h.smtpUC.ListInvites(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list invites")
		return
	}
	if list == nil {
		list = []domain.UserInvite{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *SMTPHandler) DeleteInvite(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	if err := h.smtpUC.DeleteInvite(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "invite not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to revoke invite")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (h *SMTPHandler) ResendInvite(w http.ResponseWriter, r *http.Request) {
	callerID := getUserID(r)
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	res, err := h.smtpUC.ResendInvite(r.Context(), id, callerID, r.Header.Get("Origin"))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "invite not found")
			return
		}
		if errors.Is(err, domain.ErrConflict) {
			writeJSONError(w, http.StatusConflict, "user already registered or invite already used")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to resend invite")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *SMTPHandler) VerifyInvite(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	inv, err := h.smtpUC.VerifyInvite(r.Context(), token)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invite link is invalid or has expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email":      inv.Email,
		"name":       inv.Name,
		"expires_at": inv.ExpiresAt,
	})
}

type acceptInviteRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (h *SMTPHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, accessToken, refreshToken, err := h.smtpUC.AcceptInvite(r.Context(), req.Token, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidInput) {
			writeJSONError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		if errors.Is(err, domain.ErrConflict) {
			writeJSONError(w, http.StatusConflict, "account already exists")
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "invite link is invalid or has expired")
		return
	}
	writeJSON(w, http.StatusCreated, authResponse{
		User:         user,
		Token:        accessToken,
		RefreshToken: refreshToken,
	})
}
