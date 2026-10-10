package account

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
)

// Settings represents user account and notification preferences.
type Settings struct {
	ProfileVisible   bool `json:"profileVisible"`
	NotifyNewContact bool `json:"notifyNewContact"`
	WeeklyReport     bool `json:"weeklyReport"`
	ProductUpdates   bool `json:"productUpdates"`
}

type Service struct {
	db       *sql.DB
	mu       sync.RWMutex
	settings map[string]*Settings // userID -> Settings
}

func NewService(db *sql.DB) *Service {
	s := &Service{
		db:       db,
		settings: make(map[string]*Settings),
	}
	if db != nil {
		if err := s.initSchema(); err != nil {
			log.Printf("account: warning - schema init error: %v", err)
		}
	}
	return s
}

func (s *Service) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS account_settings (
		user_id VARCHAR(64) PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		profile_visible BOOLEAN NOT NULL DEFAULT TRUE,
		notify_new_contact BOOLEAN NOT NULL DEFAULT TRUE,
		weekly_report BOOLEAN NOT NULL DEFAULT TRUE,
		product_updates BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

func defaultSettings() *Settings {
	return &Settings{
		ProfileVisible:   true,
		NotifyNewContact: true,
		WeeklyReport:     true,
		ProductUpdates:   false,
	}
}

// HandleGetSettings returns current account settings for authenticated user.
func (s *Service) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	res := defaultSettings()

	if s.db != nil {
		query := `SELECT profile_visible, notify_new_contact, weekly_report, product_updates FROM account_settings WHERE user_id = $1`
		_ = s.db.QueryRow(query, user.ID).Scan(&res.ProfileVisible, &res.NotifyNewContact, &res.WeeklyReport, &res.ProductUpdates)
	} else {
		s.mu.RLock()
		if cached, exists := s.settings[user.ID]; exists {
			res = cached
		}
		s.mu.RUnlock()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// HandleUpdateSettings saves updated account settings.
func (s *Service) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req Settings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if s.db != nil {
		query := `
		INSERT INTO account_settings (user_id, profile_visible, notify_new_contact, weekly_report, product_updates, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			profile_visible = EXCLUDED.profile_visible,
			notify_new_contact = EXCLUDED.notify_new_contact,
			weekly_report = EXCLUDED.weekly_report,
			product_updates = EXCLUDED.product_updates,
			updated_at = NOW()
		`
		if _, err := s.db.Exec(query, user.ID, req.ProfileVisible, req.NotifyNewContact, req.WeeklyReport, req.ProductUpdates); err != nil {
			http.Error(w, `{"error":"failed to update settings"}`, http.StatusInternalServerError)
			return
		}
	}

	s.mu.Lock()
	s.settings[user.ID] = &req
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"settings": req,
	})
}
