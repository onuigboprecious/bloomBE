package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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
		_, err := s.db.ExecContext(
			r.Context(),
			`INSERT INTO orders (id, finish_id, finish_name, quantity, amount, delivery_address, shipping_name, phone, email, city, payment_ref, status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'confirmed')`,
			orderID, req.FinishID, req.FinishName, req.Quantity, req.Amount, req.DeliveryAddress, req.ShippingName, req.Phone, req.Email, req.City, req.PaymentRef,
		)
		if err != nil {
			log.Printf("orders: warning DB insert error: %v (falling back gracefully)", err)
			// Fallback Exec without optional columns if schema not fully migrated yet
			_, _ = s.db.ExecContext(r.Context(), `INSERT INTO orders (id, finish_id, finish_name, quantity, amount, delivery_address, status) VALUES ($1, $2, $3, $4, $5, $6, 'confirmed')`, orderID, req.FinishID, req.FinishName, req.Quantity, req.Amount, req.DeliveryAddress)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status":  "success",
		"message": "Order created successfully",
		"order": map[string]interface{}{
			"id":           orderID,
			"finishId":     req.FinishID,
			"finishName":   req.FinishName,
			"quantity":     req.Quantity,
			"amount":       req.Amount,
			"shippingName": req.ShippingName,
			"phone":        req.Phone,
			"email":        req.Email,
			"city":         req.City,
			"paymentRef":   req.PaymentRef,
			"status":       "confirmed",
			"createdAt":    time.Now().Format(time.RFC3339),
		},
	})
}

// HandleInitializePaystack handles POST /api/paystack/initialize (Server-to-Server Paystack transaction setup)
func (s *Service) HandleInitializePaystack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	if req.Email == "" || req.ShippingName == "" || req.DeliveryAddress == "" || req.City == "" {
		writeError(w, http.StatusBadRequest, "all customer details are required")
		return
	}

	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	// Server-side authoritative price calculation (prevents client price tampering)
	unitPrice := 40000
	idLower := strings.ToLower(req.FinishID)
	nameLower := strings.ToLower(req.FinishName)

	if strings.Contains(idLower, "wristband") || strings.Contains(nameLower, "wristband") {
		unitPrice = 28000
	} else if strings.Contains(idLower, "steel") || strings.Contains(nameLower, "steel") {
		unitPrice = 65000
	} else if strings.Contains(idLower, "rose") || strings.Contains(nameLower, "rose") {
		unitPrice = 55000
	} else if strings.Contains(idLower, "bamboo") || strings.Contains(nameLower, "bamboo") {
		unitPrice = 40000
	} else if strings.Contains(idLower, "matte") || strings.Contains(nameLower, "matte") {
		unitPrice = 35000
	}

	shippingFee := 5000
	if strings.Contains(strings.ToLower(req.City), "abuja") {
		shippingFee = 0
	}

	authoritativeTotal := (unitPrice * req.Quantity) + shippingFee
	orderRef := fmt.Sprintf("ENL_%d", time.Now().UnixNano())
	orderID := fmt.Sprintf("ord-%d", time.Now().UnixNano())

	// Persist pending order in DB
	if s.db != nil {
		_, _ = s.db.ExecContext(
			r.Context(),
			`INSERT INTO orders (id, finish_id, finish_name, quantity, amount, delivery_address, shipping_name, phone, email, city, payment_ref, status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'pending')`,
			orderID, req.FinishID, req.FinishName, req.Quantity, authoritativeTotal, req.DeliveryAddress, req.ShippingName, req.Phone, req.Email, req.City, orderRef,
		)
	}

	paystackSecret := os.Getenv("PAYSTACK_SECRET_KEY")
	if paystackSecret == "" {
		// Return transaction details for dev/sandbox mode
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":      "success",
			"reference":   orderRef,
			"amount":      authoritativeTotal,
			"accessCode":  orderRef,
			"message":     "Transaction initialized server-side",
		})
		return
	}

	paystackReqBody, _ := json.Marshal(map[string]interface{}{
		"email":     req.Email,
		"amount":    authoritativeTotal * 100, // Amount in kobo
		"reference": orderRef,
		"currency":  "NGN",
		"metadata": map[string]interface{}{
			"orderId":      orderID,
			"shippingName": req.ShippingName,
			"phone":        req.Phone,
			"city":         req.City,
		},
	})

	payReq, err := http.NewRequestWithContext(r.Context(), "POST", "https://api.paystack.co/transaction/initialize", bytes.NewBuffer(paystackReqBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build paystack request")
		return
	}
	payReq.Header.Set("Authorization", "Bearer "+paystackSecret)
	payReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(payReq)
	if err != nil || resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":    "success",
			"reference": orderRef,
			"amount":    authoritativeTotal,
		})
		return
	}
	defer resp.Body.Close()

	var paystackResp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			AuthorizationURL string `json:"authorization_url"`
			AccessCode       string `json:"access_code"`
			Reference        string `json:"reference"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&paystackResp)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "success",
		"reference":        paystackResp.Data.Reference,
		"accessCode":       paystackResp.Data.AccessCode,
		"authorizationUrl": paystackResp.Data.AuthorizationURL,
		"amount":           authoritativeTotal,
	})
}
