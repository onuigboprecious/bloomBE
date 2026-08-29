package socials

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
	"github.com/onuigboprecious/infarbloom/backend/internal/models"
)

type Service struct {
	db *sql.DB

	mu      sync.RWMutex
	socials map[string][]models.SocialHandle // userID -> list of SocialHandle
}

func NewService(db *sql.DB) *Service {
	s := &Service{
		db:      db,
		socials: make(map[string][]models.SocialHandle),
	}
	s.seedDefaults()
	return s
}

func (s *Service) seedDefaults() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.socials["demo-user-id"] = []models.SocialHandle{
		{ID: "soc-1", UserID: "demo-user-id", Platform: "instagram", Handle: "precious.design", CreatedAt: time.Now()},
		{ID: "soc-2", UserID: "demo-user-id", Platform: "linkedin", Handle: "preciousonuigbo", CreatedAt: time.Now()},
		{ID: "soc-3", UserID: "demo-user-id", Platform: "twitter", Handle: "preciousonuigbo", CreatedAt: time.Now()},
		{ID: "soc-4", UserID: "demo-user-id", Platform: "whatsapp", Handle: "+2348031234567", CreatedAt: time.Now()},
	}
}

// GetUserSocials retrieves all social handles connected to user
func (s *Service) GetUserSocials(ctx context.Context, userID string) ([]models.SocialHandle, error) {
	if s.db != nil {
		rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, platform, handle, created_at FROM social_handles WHERE user_id = $1 ORDER BY created_at ASC`, userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var list []models.SocialHandle
		for rows.Next() {
			var item models.SocialHandle
			if err := rows.Scan(&item.ID, &item.UserID, &item.Platform, &item.Handle, &item.CreatedAt); err == nil {
				list = append(list, item)
			}
		}
		if list == nil {
			list = []models.SocialHandle{}
		}
		return list, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	list, exists := s.socials[userID]
	if !exists {
		return []models.SocialHandle{}, nil
	}
	return list, nil
}

// SyncUserSocials adds or bulk syncs social handles for user
func (s *Service) SyncUserSocials(ctx context.Context, userID string, payload models.BulkSocialsPayload) ([]models.SocialHandle, error) {
	if s.db != nil {
		// Single handle post check
		if payload.Platform != "" && payload.Handle != "" {
			var item models.SocialHandle
			err := s.db.QueryRowContext(ctx,
				`INSERT INTO social_handles (user_id, platform, handle, created_at) VALUES ($1, $2, $3, NOW()) RETURNING id, user_id, platform, handle, created_at`,
				userID, strings.ToLower(strings.TrimSpace(payload.Platform)), strings.TrimSpace(payload.Handle),
			).Scan(&item.ID, &item.UserID, &item.Platform, &item.Handle, &item.CreatedAt)
			if err != nil {
				return nil, err
			}
			return s.GetUserSocials(ctx, userID)
		}

		// Bulk sync
		if len(payload.Socials) > 0 {
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				return nil, err
			}
			defer tx.Rollback()

			_, _ = tx.ExecContext(ctx, `DELETE FROM social_handles WHERE user_id = $1`, userID)

			for _, sItem := range payload.Socials {
				pName := strings.ToLower(strings.TrimSpace(sItem.Platform))
				hVal := strings.TrimSpace(sItem.Handle)
				if pName != "" && hVal != "" {
					_, _ = tx.ExecContext(ctx, `INSERT INTO social_handles (user_id, platform, handle, created_at) VALUES ($1, $2, $3, NOW())`, userID, pName, hVal)
				}
			}

			if err := tx.Commit(); err != nil {
				return nil, err
			}
		}

		return s.GetUserSocials(ctx, userID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var updated []models.SocialHandle
	if payload.Platform != "" && payload.Handle != "" {
		newS := models.SocialHandle{
			ID:        fmt.Sprintf("soc-%d", time.Now().UnixNano()),
			UserID:    userID,
			Platform:  strings.ToLower(strings.TrimSpace(payload.Platform)),
			Handle:    strings.TrimSpace(payload.Handle),
			CreatedAt: time.Now(),
		}
		s.socials[userID] = append(s.socials[userID], newS)
		return s.socials[userID], nil
	}

	for i, sItem := range payload.Socials {
		if sItem.Platform != "" && sItem.Handle != "" {
			updated = append(updated, models.SocialHandle{
				ID:        fmt.Sprintf("soc-%d-%d", time.Now().UnixNano(), i),
				UserID:    userID,
				Platform:  strings.ToLower(strings.TrimSpace(sItem.Platform)),
				Handle:    strings.TrimSpace(sItem.Handle),
				CreatedAt: time.Now(),
			})
		}
	}
	s.socials[userID] = updated
	return updated, nil
}

// DeleteSocial deletes a social handle by ID
func (s *Service) DeleteSocial(ctx context.Context, userID, id string) error {
	if s.db != nil {
		res, err := s.db.ExecContext(ctx, `DELETE FROM social_handles WHERE id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return errors.New("social handle not found")
		}
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.socials[userID]
	var filtered []models.SocialHandle
	found := false
	for _, item := range list {
		if item.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !found {
		return errors.New("social handle not found")
	}
	s.socials[userID] = filtered
	return nil
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// HandleGetSocials handles GET /api/socials
func (h *Handler) HandleGetSocials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	list, err := h.svc.GetUserSocials(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, list)
}

// HandleSyncSocials handles POST /api/socials
func (h *Handler) HandleSyncSocials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var payload models.BulkSocialsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	list, err := h.svc.SyncUserSocials(r.Context(), user.ID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, list)
}

// HandleDeleteSocial handles DELETE /api/socials/{id}
func (h *Handler) HandleDeleteSocial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		id = strings.TrimPrefix(r.URL.Path, "/api/socials/")
		id = strings.TrimPrefix(id, ":")
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "social id is required")
		return
	}

	if err := h.svc.DeleteSocial(r.Context(), user.ID, id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "social handle deleted successfully"})
}
