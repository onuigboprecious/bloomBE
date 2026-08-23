package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/onuigboprecious/infarbloom/backend/internal/models"
)

type Service struct {
	db *sql.DB
	mu sync.RWMutex
}

func New(db *sql.DB) *Service {
	return &Service{db: db}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// HandleWaitlist handles POST /api/waitlist
func (s *Service) HandleWaitlist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.WaitlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	if req.Name == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "name and email are required")
		return
	}

	if req.PreferredFinish == "" {
		req.PreferredFinish = "Stealth Matte Black"
	}

	if s.db != nil {
		_, err := s.db.ExecContext(r.Context(), `INSERT INTO waitlist (name, email, phone, preferred_finish) VALUES ($1, $2, $3, $4)`, req.Name, req.Email, req.Phone, req.PreferredFinish)
		if err != nil {
			log.Printf("waitlist: warning DB insert error: %v", err)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status":  "success",
		"message": "Successfully joined the Bloom VIP waitlist",
		"data":    req,
	})
}

// HandleOrders handles POST /api/orders
func (s *Service) HandleOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	orderID := fmt.Sprintf("ord-%d", time.Now().UnixNano())

	if s.db != nil {
		_, err := s.db.ExecContext(r.Context(), `INSERT INTO orders (id, finish_id, finish_name, quantity, amount, delivery_address) VALUES ($1, $2, $3, $4, $5, $6)`, orderID, req.FinishID, req.FinishName, req.Quantity, req.Amount, req.DeliveryAddress)
		if err != nil {
			log.Printf("orders: warning DB insert error: %v", err)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status":  "success",
		"message": "Order created successfully",
		"order": map[string]interface{}{
			"id":         orderID,
			"finishId":   req.FinishID,
			"finishName": req.FinishName,
			"quantity":   req.Quantity,
			"amount":     req.Amount,
			"status":     "confirmed",
		},
	})
}
