package profiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
	"github.com/onuigboprecious/infarbloom/backend/internal/cards"
	"github.com/onuigboprecious/infarbloom/backend/internal/models"
)

type Handler struct {
	svc     *Service
	authSvc *auth.Service
}

func NewHandler(svc *Service, authSvc *auth.Service) *Handler {
	return &Handler{
		svc:     svc,
		authSvc: authSvc,
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// HandleGetProfile handles GET /api/profile/@:username, GET /api/profile/:username, or GET /api/profile/card/:cardUid?sig=...
func (h *Handler) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	username := r.PathValue("username")
	if username == "" {
		username = r.URL.Query().Get("username")
	}
	if username == "" {
		path := strings.TrimPrefix(r.URL.Path, "/api/profile/")
		if path != "" && path != "/api/profile" {
			username = path
		}
	}
	if username == "" {
		username = "precious"
	}

	// Verify HMAC signature if sig parameter is present
	sig := r.URL.Query().Get("sig")
	if sig != "" {
		if !cards.VerifyCardSignature(username, sig) {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error":   "invalid_signature",
				"message": "Hardware card signature verification failed",
			})
			return
		}
	}

	profile, err := h.svc.GetByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, ErrUnregisteredCard) || errors.Is(err, ErrProfileNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error":   "unregistered_card",
				"message": "This card has not been registered or provisioned in our system",
			})
			return
		}
		if errors.Is(err, ErrCardUnclaimed) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":   "unclaimed_card",
				"cardUid": username,
				"message": "This card has been provisioned but not claimed yet",
			})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, profile)
}

// HandleGetMyProfile handles GET /api/profile/me or GET /api/profile (Protected)
func (h *Handler) HandleGetMyProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	profile, err := h.svc.GetByUsername(r.Context(), user.Username)
	if err != nil {
		profile, err = h.svc.GetByUsername(r.Context(), user.Email)
	}
	if err != nil {
		// Fallback to default
		profile, _ = h.svc.GetByUsername(r.Context(), "precious")
	}

	writeJSON(w, http.StatusOK, profile)
}

// HandleUpdateMyProfile handles PUT /api/profile/me and PUT /api/profile (Protected)
func (h *Handler) HandleUpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required to update profile")
		return
	}

	var req models.UpdateBloomProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	profile, err := h.svc.UpdateMyProfile(r.Context(), user.ID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, profile)
}

// HandleCheckHandle handles GET /api/profile/check-handle?username=handle
func (h *Handler) HandleCheckHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	username := r.URL.Query().Get("username")
	available := h.svc.IsUsernameAvailable(r.Context(), username)

	writeJSON(w, http.StatusOK, map[string]bool{"available": available})
}

// HandleClaimCard handles POST /api/cards/claim (Protected)
func (h *Handler) HandleClaimCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required to claim card")
		return
	}

	var req models.ClaimCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CardUid == "" {
		writeError(w, http.StatusBadRequest, "cardUid is required")
		return
	}

	profile, err := h.svc.ClaimCard(r.Context(), user.ID, req.CardUid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, profile)
}

// HandleGetVCard handles GET /api/vcard/@:username or GET /api/vcard/:username
func (h *Handler) HandleGetVCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	username := r.PathValue("username")
	if username == "" {
		username = r.URL.Query().Get("username")
	}
	if username == "" {
		path := strings.TrimPrefix(r.URL.Path, "/api/vcard/")
		if path != "" {
			username = path
		}
	}
	if username == "" {
		username = "precious"
	}

	profile, err := h.svc.GetByUsername(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusNotFound, "profile not found for vCard")
		return
	}

	vcardStr := h.svc.GenerateVCard(profile)
	filename := fmt.Sprintf("%s.vcf", profile.Username)

	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(vcardStr))
}

