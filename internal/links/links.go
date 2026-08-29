package links

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

	mu    sync.RWMutex
	links map[string][]models.CustomLink // userID -> list of CustomLink
}

func NewService(db *sql.DB) *Service {
	s := &Service{
		db:    db,
		links: make(map[string][]models.CustomLink),
	}
	s.seedDefaults()
	return s
}

func (s *Service) seedDefaults() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.links["demo-user-id"] = []models.CustomLink{
		{ID: "lnk-1", UserID: "demo-user-id", Label: "My Design Portfolio", URL: "https://precious.design", Order: 1, CreatedAt: time.Now()},
		{ID: "lnk-2", UserID: "demo-user-id", Label: "Book 1-on-1 Consultation", URL: "https://calendly.com/precious-onuigbo/30min", Order: 2, CreatedAt: time.Now()},
		{ID: "lnk-3", UserID: "demo-user-id", Label: "Watch YouTube Builds", URL: "https://youtube.com/@precious_builds", Order: 3, CreatedAt: time.Now()},
	}
}

// GetUserLinks returns all custom links created by user
func (s *Service) GetUserLinks(ctx context.Context, userID string) ([]models.CustomLink, error) {
	if s.db != nil {
		rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, label, url, link_order, created_at FROM custom_links WHERE user_id = $1 ORDER BY link_order ASC, created_at ASC`, userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var list []models.CustomLink
		for rows.Next() {
			var item models.CustomLink
			if err := rows.Scan(&item.ID, &item.UserID, &item.Label, &item.URL, &item.Order, &item.CreatedAt); err == nil {
				list = append(list, item)
			}
		}
		if list == nil {
			list = []models.CustomLink{}
		}
		return list, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	list, exists := s.links[userID]
	if !exists {
		return []models.CustomLink{}, nil
	}
	return list, nil
}

// CreateLink adds a new custom link button
func (s *Service) CreateLink(ctx context.Context, userID string, payload models.CreateCustomLinkPayload) (*models.CustomLink, error) {
	label := strings.TrimSpace(payload.Label)
	urlVal := strings.TrimSpace(payload.URL)

	if label == "" || urlVal == "" {
		return nil, errors.New("label and url are required")
	}

	if s.db != nil {
		var item models.CustomLink
		query := `
		INSERT INTO custom_links (user_id, label, url, link_order, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id, user_id, label, url, link_order, created_at
		`
		err := s.db.QueryRowContext(ctx, query, userID, label, urlVal, payload.Order).Scan(
			&item.ID, &item.UserID, &item.Label, &item.URL, &item.Order, &item.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		return &item, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item := models.CustomLink{
		ID:        fmt.Sprintf("lnk-%d", time.Now().UnixNano()),
		UserID:    userID,
		Label:     label,
		URL:       urlVal,
		Order:     payload.Order,
		CreatedAt: time.Now(),
	}

	s.links[userID] = append(s.links[userID], item)
	return &item, nil
}

// DeleteLink removes a custom link by ID
func (s *Service) DeleteLink(ctx context.Context, userID, id string) error {
	if s.db != nil {
		res, err := s.db.ExecContext(ctx, `DELETE FROM custom_links WHERE id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return errors.New("custom link not found")
		}
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.links[userID]
	var filtered []models.CustomLink
	found := false
	for _, item := range list {
		if item.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !found {
		return errors.New("custom link not found")
	}
	s.links[userID] = filtered
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

// HandleGetLinks handles GET /api/custom-links
func (h *Handler) HandleGetLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	list, err := h.svc.GetUserLinks(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, list)
}

// HandleCreateLink handles POST /api/custom-links
func (h *Handler) HandleCreateLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var payload models.CreateCustomLinkPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	item, err := h.svc.CreateLink(r.Context(), user.ID, payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, item)
}

// HandleDeleteLink handles DELETE /api/custom-links/{id}
func (h *Handler) HandleDeleteLink(w http.ResponseWriter, r *http.Request) {
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
		id = strings.TrimPrefix(r.URL.Path, "/api/custom-links/")
		id = strings.TrimPrefix(id, ":")
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "link id is required")
		return
	}

	if err := h.svc.DeleteLink(r.Context(), user.ID, id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "custom link deleted successfully"})
}
