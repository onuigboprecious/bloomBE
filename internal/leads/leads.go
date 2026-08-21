package leads

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type Lead struct {
	ID        string    `json:"id"`
	CardUID   string    `json:"cardUid"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	Role      string    `json:"role,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Method    string    `json:"method"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateLeadPayload struct {
	CardUID string `json:"cardUid"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Role    string `json:"role"`
	Notes   string `json:"notes"`
	Method  string `json:"method"`
}

type Service struct {
	db     *sql.DB
	mu     sync.RWMutex
	memory []Lead
}

func New(db *sql.DB) *Service {
	svc := &Service{
		db: db,
		memory: []Lead{
			{
				ID:        "lead-1",
				CardUID:   "BLM-9921-NFC",
				Name:      "Amaka Adebayo",
				Phone:     "+234 802 345 6789",
				Email:     "amaka@paystack.com",
				Role:      "VP of Growth @ Paystack",
				Notes:     "Interested in NFC wristbands for our annual fintech summit.",
				Method:    "Share Back Form",
				CreatedAt: time.Now().Add(-2 * time.Minute),
			},
			{
				ID:        "lead-2",
				CardUID:   "BLM-9921-NFC",
				Name:      "Tunde Bakare",
				Phone:     "+234 809 111 2233",
				Email:     "tbakare@kudacapital.com",
				Role:      "Managing Partner @ Kuda Capital",
				Notes:     "Let's discuss corporate orders for our executive team.",
				Method:    "NFC Tap",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
			{
				ID:        "lead-3",
				CardUID:   "BLM-9921-NFC",
				Name:      "Zainab Bello",
				Phone:     "+234 813 444 5566",
				Email:     "zainab.bello@flutterwave.com",
				Role:      "Head of Product @ Flutterwave",
				Notes:     "Met at Lagos Tech Week.",
				Method:    "QR Scan",
				CreatedAt: time.Now().Add(-3 * time.Hour),
			},
		},
	}

	if db != nil {
		if err := svc.initSchema(); err != nil {
			log.Printf("leads: warning - schema init error: %v", err)
		}
	}

	return svc
}

func (s *Service) initSchema() error {
	if s.db == nil {
		return nil
	}

	schema := `
	CREATE TABLE IF NOT EXISTS leads (
		id VARCHAR(64) PRIMARY KEY,
		card_uid VARCHAR(64) NOT NULL DEFAULT 'BLM-9921-NFC',
		name VARCHAR(255) NOT NULL,
		phone VARCHAR(64) NOT NULL,
		email VARCHAR(255) NOT NULL,
		role VARCHAR(255),
		notes TEXT,
		method VARCHAR(64) NOT NULL DEFAULT 'Share Back Form',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_leads_card_uid ON leads(card_uid);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Service) HandleCreateLead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req CreateLeadPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	if req.Name == "" || req.Email == "" || req.Phone == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Name, Phone, and Email are required fields"})
		return
	}

	if req.Method == "" {
		req.Method = "Share Back Form"
	}
	if req.CardUID == "" {
		req.CardUID = "BLM-9921-NFC"
	}

	leadID := fmt.Sprintf("lead-%d", time.Now().UnixNano())
	newLead := Lead{
		ID:        leadID,
		CardUID:   req.CardUID,
		Name:      req.Name,
		Phone:     req.Phone,
		Email:     req.Email,
		Role:      req.Role,
		Notes:     req.Notes,
		Method:    req.Method,
		CreatedAt: time.Now(),
	}

	if s.db != nil {
		_, err := s.db.Exec(
			`INSERT INTO leads (id, card_uid, name, phone, email, role, notes, method, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			newLead.ID, newLead.CardUID, newLead.Name, newLead.Phone, newLead.Email, newLead.Role, newLead.Notes, newLead.Method, newLead.CreatedAt,
		)
		if err != nil {
			log.Printf("leads: failed to insert lead into DB: %v", err)
		}
	}

	s.mu.Lock()
	s.memory = append([]Lead{newLead}, s.memory...)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "success",
		"message": "Lead captured successfully",
		"lead":    newLead,
	})
}

func (s *Service) HandleGetLeads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	leadsList := make([]Lead, len(s.memory))
	copy(leadsList, s.memory)
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "success",
		"leads":  leadsList,
	})
}
