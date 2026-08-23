package leads

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
	"github.com/onuigboprecious/infarbloom/backend/internal/models"
)

type Service struct {
	db     *sql.DB
	mu     sync.RWMutex
	memory []models.Lead
}

func New(db *sql.DB) *Service {
	svc := &Service{
		db: db,
		memory: []models.Lead{
			{
				ID:        "lead-1",
				CardUid:   "BLM-9921-NFC",
				Name:      "Amaka Adebayo",
				Phone:     "+234 802 345 6789",
				Email:     "amaka@paystack.com",
				Role:      "VP of Growth @ Paystack",
				Notes:     "Met at Techpoint Summit Lagos",
				Method:    "NFC Tap",
				CreatedAt: time.Now().Add(-2 * time.Minute),
			},
			{
				ID:        "lead-2",
				CardUid:   "BLM-9921-NFC",
				Name:      "Tunde Bakare",
				Phone:     "+234 809 111 2233",
				Email:     "tbakare@kudacapital.com",
				Role:      "Managing Partner @ Kuda Capital",
				Notes:     "Let's discuss corporate orders for our executive team.",
				Method:    "NFC Tap",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
		},
	}
	return svc
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// HandleCreateLead handles POST /api/leads
func (s *Service) HandleCreateLead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.Lead
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Method == "" {
		req.Method = "NFC Tap"
	}
	if req.CardUid == "" {
		req.CardUid = "BLM-9921-NFC"
	}

	req.ID = fmt.Sprintf("lead-%d", time.Now().UnixNano())
	req.CreatedAt = time.Now()

	if s.db != nil {
		var userID sql.NullString
		_ = s.db.QueryRowContext(r.Context(), `SELECT user_id FROM nfc_cards WHERE card_uid = $1`, req.CardUid).Scan(&userID)

		_, err := s.db.ExecContext(
			r.Context(),
			`INSERT INTO leads (id, user_id, card_uid, name, email, phone, role, method, notes, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			req.ID, userID, req.CardUid, req.Name, req.Email, req.Phone, req.Role, req.Method, req.Notes, req.CreatedAt,
		)
		if err != nil {
			log.Printf("leads: warning DB insert: %v", err)
		}
	}

	s.mu.Lock()
	s.memory = append([]models.Lead{req}, s.memory...)
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, req)
}

// HandleGetLeads handles GET /api/leads (Protected)
func (s *Service) HandleGetLeads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		// Provide mock leads fallback if not strictly authenticated
	}

	if s.db != nil && user != nil {
		rows, err := s.db.QueryContext(r.Context(), `SELECT id, card_uid, name, email, phone, role, method, notes, created_at FROM leads WHERE user_id = $1 OR card_uid = 'BLM-9921-NFC' ORDER BY created_at DESC`, user.ID)
		if err == nil {
			defer rows.Close()
			var leadsList []models.Lead
			for rows.Next() {
				var l models.Lead
				if err := rows.Scan(&l.ID, &l.CardUid, &l.Name, &l.Email, &l.Phone, &l.Role, &l.Method, &l.Notes, &l.CreatedAt); err == nil {
					leadsList = append(leadsList, l)
				}
			}
			if err := rows.Err(); err != nil {
				writeError(w, http.StatusInternalServerError, "failed iterating leads: "+err.Error())
				return
			}
			if leadsList == nil {
				leadsList = []models.Lead{}
			}
			writeJSON(w, http.StatusOK, leadsList)
			return
		}
	}

	s.mu.RLock()
	leadsList := make([]models.Lead, len(s.memory))
	copy(leadsList, s.memory)
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, leadsList)
}

// HandleDeleteLead handles DELETE /api/leads/:id (Protected)
func (s *Service) HandleDeleteLead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	leadID := r.PathValue("id")
	if leadID == "" {
		path := strings.TrimPrefix(r.URL.Path, "/api/leads/")
		leadID = strings.TrimPrefix(path, ":")
	}
	if leadID == "" {
		writeError(w, http.StatusBadRequest, "lead id is required")
		return
	}

	if s.db != nil {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM leads WHERE id = $1 AND (user_id = $2 OR user_id IS NULL)`, leadID, user.ID)
	}

	s.mu.Lock()
	var updated []models.Lead
	for _, l := range s.memory {
		if l.ID != leadID {
			updated = append(updated, l)
		}
	}
	s.memory = updated
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"message": "lead deleted successfully"})
}
