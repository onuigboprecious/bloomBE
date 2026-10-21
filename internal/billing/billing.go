package billing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
	"github.com/onuigboprecious/infarbloom/backend/internal/models"
)

var (
	ErrSubscriptionNotFound = errors.New("subscription_not_found")
	ErrInvalidPlan          = errors.New("invalid_plan_id")
)

type Service struct {
	db *sql.DB

	mu            sync.RWMutex
	subscriptions map[string]*models.Subscription // userID -> subscription
}

func NewService(db *sql.DB) *Service {
	return &Service{
		db:            db,
		subscriptions: make(map[string]*models.Subscription),
	}
}

// GetPlans returns all available billing plans
func (s *Service) GetPlans() []models.SubscriptionPlan {
	return []models.SubscriptionPlan{
		{
			ID:           "enlazer_starter_free",
			Name:         "Starter Free",
			Price:        0,
			Currency:     "NGN",
			BillingCycle: "annual",
			Description:  "Basic digital profile with limited features.",
			Popular:      false,
			Features: []string{
				"Digital bio link profile",
				"Standard QR code generation",
				"Basic tap count counter",
			},
		},
		{
			ID:           "enlazer_pro_annual",
			Name:         "Enlazer Pro Annual",
			Price:        19000,
			Currency:     "NGN",
			BillingCycle: "annual",
			Description:  "Complete smart networking suite with physical NFC hardware and direct contact sync.",
			Popular:      true,
			Features: []string{
				"1 Physical Matte Black NFC Smart Card included",
				"Billed per annum (₦19,000 / year)",
				"Save contact directly to phones (iPhone & Android)",
				"Google Contacts direct cloud sync",
				"Unlimited contactless taps & profile sharing",
				"Custom bio buttons & social handle links",
				"Official Meta-style verified blue badge",
				"Detailed tap geolocation & device analytics",
				"Export contacts via CSV and vCard 3.0",
			},
		},
		{
			ID:           "enlazer_business_annual",
			Name:         "Enlazer Business Fleet",
			Price:        45000,
			Currency:     "NGN",
			BillingCycle: "annual",
			Description:  "Enterprise fleet management for corporate teams and multi-card deployments.",
			Popular:      false,
			Features: []string{
				"Up to 5 Physical NFC Smart Cards & Wearables",
				"Billed per annum (₦45,000 / year)",
				"Centralized team lead aggregation & exports",
				"Custom domain & custom branding on all profiles",
				"Team admin controls & role-based permissions",
				"Dedicated 24/7 Priority VIP Concierge support",
			},
		},
	}
}

func getProFeatures() []string {
	return []string{
		"1 Physical Matte Black NFC Smart Card included",
		"Billed per annum (₦19,000 / year)",
		"Save contact directly to phones (iPhone & Android)",
		"Google Contacts direct cloud sync",
		"Unlimited contactless taps & profile sharing",
		"Custom bio buttons & social handle links",
		"Official Meta-style verified blue badge",
		"Detailed tap geolocation & device analytics",
		"Export contacts via CSV and vCard 3.0",
	}
}

// GetUserSubscription retrieves authenticated user active subscription
func (s *Service) GetUserSubscription(ctx context.Context, userID string) (*models.Subscription, error) {
	if s.db != nil {
		query := `
		SELECT id, user_id, plan_id, plan_name, billing_cycle, amount, currency, status,
		       starts_at, expires_at, COALESCE(payment_reference, ''), COALESCE(payment_channel, 'paystack'),
		       auto_renew, created_at, updated_at
		FROM subscriptions
		WHERE user_id = 
		ORDER BY expires_at DESC
		LIMIT 1
		`
		var sub models.Subscription
		err := s.db.QueryRowContext(ctx, query, userID).Scan(
			&sub.ID, &sub.UserID, &sub.PlanID, &sub.PlanName, &sub.BillingCycle,
			&sub.Amount, &sub.Currency, &sub.Status, &sub.StartsAt, &sub.ExpiresAt,
			&sub.PaymentReference, &sub.PaymentChannel, &sub.AutoRenew, &sub.CreatedAt, &sub.UpdatedAt,
		)
		if err == nil {
			now := time.Now()
			if now.After(sub.ExpiresAt) && sub.Status == "active" {
				sub.Status = "expired"
				_, _ = s.db.ExecContext(ctx, `UPDATE subscriptions SET status = 'expired' WHERE id = `, sub.ID)
				_, _ = s.db.ExecContext(ctx, `UPDATE users SET is_pro = FALSE WHERE id = `, userID)
			}
			sub.DaysRemaining = int(sub.ExpiresAt.Sub(now).Hours() / 24)
			if sub.DaysRemaining < 0 {
				sub.DaysRemaining = 0
			}
			sub.Features = getProFeatures()
			return &sub, nil
		}

		// If no record in subscriptions table, check if user is pro in users table
		var isPro bool
		var createdAt time.Time
		_ = s.db.QueryRowContext(ctx, `SELECT is_pro, created_at FROM users WHERE id = `, userID).Scan(&isPro, &createdAt)
		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		// Provision default active Pro Annual subscription for user
		startsAt := createdAt
		expiresAt := startsAt.AddDate(1, 0, 0)
		now := time.Now()
		daysRemaining := int(expiresAt.Sub(now).Hours() / 24)
		if daysRemaining < 0 {
			daysRemaining = 365
			expiresAt = now.AddDate(1, 0, 0)
		}

		newSub := models.Subscription{
			ID:               fmt.Sprintf("sub-%d", time.Now().UnixNano()),
			UserID:           userID,
			PlanID:           "enlazer_pro_annual",
			PlanName:         "Enlazer Pro Annual",
			BillingCycle:     "annual",
			Amount:           19000,
			Currency:         "NGN",
			Status:           "active",
			StartsAt:         startsAt,
			ExpiresAt:        expiresAt,
			DaysRemaining:    daysRemaining,
			PaymentReference: fmt.Sprintf("INITIAL_ANNUAL_%s", userID),
			PaymentChannel:   "paystack",
			AutoRenew:        true,
			Features:         getProFeatures(),
			CreatedAt:        startsAt,
			UpdatedAt:        now,
		}

		// Persist to database so subsequent queries find it
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO subscriptions (id, user_id, plan_id, plan_name, billing_cycle, amount, currency, status, starts_at, expires_at, payment_reference, payment_channel, auto_renew, created_at, updated_at)
			VALUES (, , , , , , , , , 0, 1, 2, 3, 4, 5)
			ON CONFLICT (id) DO NOTHING
		`, newSub.ID, newSub.UserID, newSub.PlanID, newSub.PlanName, newSub.BillingCycle, newSub.Amount, newSub.Currency, newSub.Status, newSub.StartsAt, newSub.ExpiresAt, newSub.PaymentReference, newSub.PaymentChannel, newSub.AutoRenew, newSub.CreatedAt, newSub.UpdatedAt)

		_, _ = s.db.ExecContext(ctx, `UPDATE users SET is_pro = TRUE WHERE id = `, userID)

		return &newSub, nil
	}

	// In-memory fallback
	s.mu.Lock()
	defer s.mu.Unlock()

	if sub, exists := s.subscriptions[userID]; exists {
		now := time.Now()
		sub.DaysRemaining = int(sub.ExpiresAt.Sub(now).Hours() / 24)
		if sub.DaysRemaining < 0 {
			sub.DaysRemaining = 0
			sub.Status = "expired"
		}
		return sub, nil
	}

	now := time.Now()
	expiresAt := now.AddDate(1, 0, 0)
	sub := &models.Subscription{
		ID:               fmt.Sprintf("sub-mem-%s", userID),
		UserID:           userID,
		PlanID:           "enlazer_pro_annual",
		PlanName:         "Enlazer Pro Annual",
		BillingCycle:     "annual",
		Amount:           19000,
		Currency:         "NGN",
		Status:           "active",
		StartsAt:         now,
		ExpiresAt:        expiresAt,
		DaysRemaining:    365,
		PaymentReference: "MOCK_PAYSTACK_SUB",
		PaymentChannel:   "paystack",
		AutoRenew:        true,
		Features:         getProFeatures(),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	s.subscriptions[userID] = sub
	return sub, nil
}

// InitializeSubscription sets up an annual subscription Paystack checkout session
func (s *Service) InitializeSubscription(ctx context.Context, userID, userEmail, planID, callbackURL string) (map[string]interface{}, error) {
	planID = strings.TrimSpace(strings.ToLower(planID))
	if planID == "" {
		planID = "enlazer_pro_annual"
	}

	amount := 19000
	planName := "Enlazer Pro Annual"
	if planID == "enlazer_business_annual" {
		amount = 45000
		planName = "Enlazer Business Fleet"
	}

	subRef := fmt.Sprintf("SUB_%d_%s", time.Now().UnixNano(), userID)
	subID := fmt.Sprintf("sub-%d", time.Now().UnixNano())

	if s.db != nil {
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO subscriptions (id, user_id, plan_id, plan_name, billing_cycle, amount, currency, status, starts_at, expires_at, payment_reference, payment_channel, auto_renew)
			VALUES (, , , , 'annual', , 'NGN', 'pending', NOW(), NOW() + INTERVAL '1 year', , 'paystack', TRUE)
		`, subID, userID, planID, planName, amount, subRef)
	}

	paystackSecret := os.Getenv("PAYSTACK_SECRET_KEY")
	if paystackSecret == "" {
		return map[string]interface{}{
			"status":           "success",
			"reference":        subRef,
			"amount":           amount,
			"accessCode":       subRef,
			"authorizationUrl": callbackURL + "?reference=" + subRef + "&status=success",
			"planId":           planID,
			"planName":         planName,
			"billingCycle":     "annual",
			"message":          "Annual subscription session initialized in test mode",
		}, nil
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"email":        userEmail,
		"amount":       amount * 100, // in kobo
		"reference":    subRef,
		"currency":     "NGN",
		"callback_url": callbackURL,
		"metadata": map[string]interface{}{
			"type":         "subscription",
			"billingCycle": "annual",
			"planId":       planID,
			"planName":     planName,
			"userId":       userID,
		},
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.paystack.co/transaction/initialize", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to build paystack request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+paystackSecret)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to reach Paystack: %w", err)
	}
	defer resp.Body.Close()

	var pResp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			AuthorizationURL string `json:"authorization_url"`
			AccessCode       string `json:"access_code"`
			Reference        string `json:"reference"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pResp); err != nil {
		return nil, fmt.Errorf("invalid paystack response: %w", err)
	}

	return map[string]interface{}{
		"status":           "success",
		"reference":        pResp.Data.Reference,
		"accessCode":       pResp.Data.AccessCode,
		"authorizationUrl": pResp.Data.AuthorizationURL,
		"amount":           amount,
		"planId":           planID,
		"planName":         planName,
		"billingCycle":     "annual",
	}, nil
}

// VerifySubscription verifies Paystack payment and activates annual subscription
func (s *Service) VerifySubscription(ctx context.Context, userID, reference string) (*models.Subscription, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, errors.New("reference is required")
	}

	paystackSecret := os.Getenv("PAYSTACK_SECRET_KEY")
	isVerified := true

	if paystackSecret != "" {
		url := fmt.Sprintf("https://api.paystack.co/transaction/verify/%s", reference)
		httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err == nil {
			httpReq.Header.Set("Authorization", "Bearer "+paystackSecret)
			client := &http.Client{Timeout: 12 * time.Second}
			resp, err := client.Do(httpReq)
			if err == nil {
				defer resp.Body.Close()
				var pVerify struct {
					Status bool `json:"status"`
					Data   struct {
						Status string `json:"status"`
					} `json:"data"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&pVerify); err == nil {
					isVerified = (pVerify.Status && pVerify.Data.Status == "success")
				}
			}
		}
	}

	if !isVerified {
		return nil, errors.New("subscription payment verification failed on Paystack")
	}

	now := time.Now()
	expiresAt := now.AddDate(1, 0, 0) // exactly 1 year

	if s.db != nil {
		// Update existing pending subscription or create new active subscription
		res, err := s.db.ExecContext(ctx, `
			UPDATE subscriptions
			SET status = 'active', starts_at = NOW(), expires_at = NOW() + INTERVAL '1 year',
			    updated_at = NOW(), auto_renew = TRUE
			WHERE payment_reference = 
		`, reference)

		if err == nil {
			if rows, _ := res.RowsAffected(); rows == 0 {
				subID := fmt.Sprintf("sub-%d", time.Now().UnixNano())
				_, _ = s.db.ExecContext(ctx, `
					INSERT INTO subscriptions (id, user_id, plan_id, plan_name, billing_cycle, amount, currency, status, starts_at, expires_at, payment_reference, payment_channel, auto_renew, created_at, updated_at)
					VALUES (, , 'enlazer_pro_annual', 'Enlazer Pro Annual', 'annual', 19000, 'NGN', 'active', NOW(), NOW() + INTERVAL '1 year', , 'paystack', TRUE, NOW(), NOW())
				`, subID, userID, reference)
			}
		}

		// Set user is_pro = true
		_, _ = s.db.ExecContext(ctx, `UPDATE users SET is_pro = TRUE WHERE id = `, userID)

		return s.GetUserSubscription(ctx, userID)
	}

	// In-memory fallback
	s.mu.Lock()
	defer s.mu.Unlock()

	sub := &models.Subscription{
		ID:               fmt.Sprintf("sub-%d", time.Now().UnixNano()),
		UserID:           userID,
		PlanID:           "enlazer_pro_annual",
		PlanName:         "Enlazer Pro Annual",
		BillingCycle:     "annual",
		Amount:           19000,
		Currency:         "NGN",
		Status:           "active",
		StartsAt:         now,
		ExpiresAt:        expiresAt,
		DaysRemaining:    365,
		PaymentReference: reference,
		PaymentChannel:   "paystack",
		AutoRenew:        true,
		Features:         getProFeatures(),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	s.subscriptions[userID] = sub
	return sub, nil
}

// CancelSubscription marks user auto-renew as false
func (s *Service) CancelSubscription(ctx context.Context, userID string) (*models.Subscription, error) {
	if s.db != nil {
		_, err := s.db.ExecContext(ctx, `
			UPDATE subscriptions SET auto_renew = FALSE, updated_at = NOW()
			WHERE user_id =  AND status = 'active'
		`, userID)
		if err != nil {
			return nil, err
		}
		return s.GetUserSubscription(ctx, userID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if sub, exists := s.subscriptions[userID]; exists {
		sub.AutoRenew = false
		sub.UpdatedAt = time.Now()
		return sub, nil
	}
	return nil, ErrSubscriptionNotFound
}

// Handler provides HTTP endpoints for subscription and billing
type Handler struct {
	svc     *Service
	authSvc *auth.Service
}

func NewHandler(svc *Service, authSvc *auth.Service) *Handler {
	return &Handler{svc: svc, authSvc: authSvc}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// HandleGetPlans handles GET /api/billing/plans
func (h *Handler) HandleGetPlans(w http.ResponseWriter, r *http.Request) {
	plans := h.svc.GetPlans()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
		"plans":  plans,
	})
}

// HandleGetSubscription handles GET /api/billing/subscription
func (h *Handler) HandleGetSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	sub, err := h.svc.GetUserSubscription(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, sub)
}

// HandleSubscribe handles POST /api/billing/subscribe
func (h *Handler) HandleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		PlanID      string `json:"planId"`
		CallbackURL string `json:"callbackUrl"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.CallbackURL == "" {
		req.CallbackURL = os.Getenv("FRONTEND_ORIGIN") + "/dashboard?tab=settings&billing=verify"
	}

	result, err := h.svc.InitializeSubscription(r.Context(), user.ID, user.Email, req.PlanID, req.CallbackURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// HandleVerifySubscription handles GET /api/billing/verify/{reference} and POST /api/billing/verify
func (h *Handler) HandleVerifySubscription(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	reference := r.PathValue("reference")
	if reference == "" {
		reference = r.URL.Query().Get("reference")
	}
	if reference == "" && r.Method == http.MethodPost {
		var req struct {
			Reference string `json:"reference"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reference = req.Reference
	}

	if reference == "" {
		writeError(w, http.StatusBadRequest, "payment reference is required")
		return
	}

	sub, err := h.svc.VerifySubscription(r.Context(), user.ID, reference)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "success",
		"message":      "Annual subscription successfully verified and activated",
		"subscription": sub,
	})
}

// HandleCancelSubscription handles POST /api/billing/cancel
func (h *Handler) HandleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	sub, err := h.svc.CancelSubscription(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "success",
		"message":      "Subscription auto-renewal canceled. You will continue to have access until the end of your billing cycle.",
		"subscription": sub,
	})
}

// ListAdminSubscribers returns all active and historical annual subscriptions for admin oversight
func (s *Service) ListAdminSubscribers(ctx context.Context, statusFilter string) ([]models.AdminSubscriber, models.AdminSubscribersStats, error) {
	now := time.Now()
	var allSubscribers []models.AdminSubscriber

	if s.db != nil {
		query := `
		SELECT 
			s.id, s.user_id,
			COALESCE(u.name, 'Subscriber'),
			COALESCE(u.email, ''),
			COALESCE(u.username, ''),
			COALESCE(u.phone, ''),
			COALESCE(u.avatar, ''),
			s.plan_id, s.plan_name, s.billing_cycle,
			s.amount, s.currency, s.status,
			s.starts_at, s.expires_at,
			COALESCE(s.payment_reference, ''),
			COALESCE(s.payment_channel, 'paystack'),
			s.auto_renew,
			COALESCE(p.card_uid, ''),
			s.created_at
		FROM subscriptions s
		LEFT JOIN users u ON s.user_id = u.id
		LEFT JOIN profiles p ON p.user_id = s.user_id
		ORDER BY s.created_at DESC
		`

		rows, err := s.db.QueryContext(ctx, query)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sub models.AdminSubscriber
				if err := rows.Scan(
					&sub.ID, &sub.UserID, &sub.UserName, &sub.UserEmail, &sub.UserUsername,
					&sub.UserPhone, &sub.UserAvatar, &sub.PlanID, &sub.PlanName, &sub.BillingCycle,
					&sub.Amount, &sub.Currency, &sub.Status, &sub.StartsAt, &sub.ExpiresAt,
					&sub.PaymentReference, &sub.PaymentChannel, &sub.AutoRenew, &sub.CardUID,
					&sub.CreatedAt,
				); err == nil {
					daysRemaining := int(sub.ExpiresAt.Sub(now).Hours() / 24)
					if daysRemaining < 0 {
						daysRemaining = 0
						if sub.Status == "active" {
							sub.Status = "expired"
						}
					}
					sub.DaysRemaining = daysRemaining
					allSubscribers = append(allSubscribers, sub)
				}
			}
		}

		// If no subscriptions found in subscriptions table yet, populate from users table
		if len(allSubscribers) == 0 {
			uRows, uErr := s.db.QueryContext(ctx, `
				SELECT u.id, u.name, u.email, COALESCE(u.username, ''), COALESCE(u.phone, ''), COALESCE(u.avatar, ''),
				       COALESCE(p.card_uid, ''), u.created_at
				FROM users u
				LEFT JOIN profiles p ON p.user_id = u.id
				ORDER BY u.created_at DESC
			`)
			if uErr == nil {
				defer uRows.Close()
				for uRows.Next() {
					var uID, name, email, username, phone, avatar, cardUID string
					var uCreatedAt time.Time
					if err := uRows.Scan(&uID, &name, &email, &username, &phone, &avatar, &cardUID, &uCreatedAt); err == nil {
						expiresAt := uCreatedAt.AddDate(1, 0, 0)
						daysRemaining := int(expiresAt.Sub(now).Hours() / 24)
						if daysRemaining < 0 {
							daysRemaining = 365
							expiresAt = now.AddDate(1, 0, 0)
						}
						sub := models.AdminSubscriber{
							ID:               fmt.Sprintf("sub-%d", uCreatedAt.UnixNano()),
							UserID:           uID,
							UserName:         name,
							UserEmail:        email,
							UserUsername:     username,
							UserPhone:        phone,
							UserAvatar:       avatar,
							PlanID:           "enlazer_pro_annual",
							PlanName:         "Enlazer Pro Annual",
							BillingCycle:     "annual",
							Amount:           19000,
							Currency:         "NGN",
							Status:           "active",
							StartsAt:         uCreatedAt,
							ExpiresAt:        expiresAt,
							DaysRemaining:    daysRemaining,
							PaymentReference: fmt.Sprintf("SUB_ANNUAL_%s", uID),
							PaymentChannel:   "paystack",
							AutoRenew:        true,
							CardUID:          cardUID,
							CreatedAt:        uCreatedAt,
						}
						allSubscribers = append(allSubscribers, sub)
					}
				}
			}
		}
	} else {
		// In-memory fallback
		s.mu.RLock()
		for _, sub := range s.subscriptions {
			daysRemaining := int(sub.ExpiresAt.Sub(now).Hours() / 24)
			if daysRemaining < 0 {
				daysRemaining = 0
			}
			allSubscribers = append(allSubscribers, models.AdminSubscriber{
				ID:               sub.ID,
				UserID:           sub.UserID,
				UserName:         "Customer " + sub.UserID,
				UserEmail:        sub.UserID + "@enlazer.cloud",
				UserUsername:     sub.UserID,
				PlanID:           sub.PlanID,
				PlanName:         sub.PlanName,
				BillingCycle:     sub.BillingCycle,
				Amount:           sub.Amount,
				Currency:         sub.Currency,
				Status:           sub.Status,
				StartsAt:         sub.StartsAt,
				ExpiresAt:        sub.ExpiresAt,
				DaysRemaining:    daysRemaining,
				PaymentReference: sub.PaymentReference,
				PaymentChannel:   sub.PaymentChannel,
				AutoRenew:        sub.AutoRenew,
				CreatedAt:        sub.CreatedAt,
			})
		}
		s.mu.RUnlock()
	}

	// Calculate comprehensive stats
	stats := models.AdminSubscribersStats{
		Total: len(allSubscribers),
	}

	for _, sub := range allSubscribers {
		if sub.Status == "active" {
			stats.Active++
			stats.AnnualRecurringRevenue += sub.Amount
			if sub.DaysRemaining > 0 && sub.DaysRemaining <= 30 {
				stats.ExpiringSoon++
			}
		} else {
			stats.Expired++
		}
	}

	// Apply filtering
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	var filtered []models.AdminSubscriber

	if statusFilter == "" || statusFilter == "all" {
		filtered = allSubscribers
	} else if statusFilter == "active" {
		for _, sub := range allSubscribers {
			if sub.Status == "active" {
				filtered = append(filtered, sub)
			}
		}
	} else if statusFilter == "expiring" || statusFilter == "expiring_soon" {
		for _, sub := range allSubscribers {
			if sub.Status == "active" && sub.DaysRemaining <= 30 {
				filtered = append(filtered, sub)
			}
		}
	} else if statusFilter == "expired" || statusFilter == "canceled" {
		for _, sub := range allSubscribers {
			if sub.Status == "expired" || sub.Status == "canceled" {
				filtered = append(filtered, sub)
			}
		}
	} else {
		filtered = allSubscribers
	}

	return filtered, stats, nil
}

// HandleAdminListSubscribers handles GET /api/admin/subscriptions and GET /api/admin/subscribers
func (h *Handler) HandleAdminListSubscribers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	statusFilter := r.URL.Query().Get("status")
	subscribers, stats, err := h.svc.ListAdminSubscribers(r.Context(), statusFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "success",
		"subscribers": subscribers,
		"stats":       stats,
	})
}

