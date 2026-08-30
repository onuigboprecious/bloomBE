package cards

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
)

var (
	ErrCardNotFound       = errors.New("unregistered_card")
	ErrCardAlreadyClaimed = errors.New("card_already_claimed")
	ErrCardUidExists      = errors.New("card_uid_already_exists")
)

type NFCCard struct {
	ID         string    `json:"id"`
	CardUid    string    `json:"cardUid"`
	UserID     *string   `json:"userId,omitempty"`
	LinkedUser string    `json:"linkedUser,omitempty"`
	FinishName string    `json:"finishName"`
	Status     string    `json:"status"`
	TapsCount  int       `json:"tapsCount"`
	SignedURL  string    `json:"signedUrl,omitempty"`
	Signature  string    `json:"signature,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ProvisionBatchRequest struct {
	CardUids   []string `json:"cardUids"`
	FinishName string   `json:"finishName,omitempty"`
}

type ProvisionBatchResponse struct {
	Status string    `json:"status"`
	Count  int       `json:"count"`
	Cards  []NFCCard `json:"cards"`
}

type CreateCardPayload struct {
	CardUid    string `json:"cardUid"`
	FinishName string `json:"finishName"`
	Status     string `json:"status"`
}

type UpdateCardPayload struct {
	CardUid    *string `json:"cardUid,omitempty"`
	FinishName *string `json:"finishName,omitempty"`
	Status     *string `json:"status,omitempty"`
}

type Service struct {
	db *sql.DB

	// In-memory fallback
	mu    sync.RWMutex
	cards map[string]*NFCCard // cardUid -> NFCCard
}

func NewService(db *sql.DB) *Service {
	s := &Service{
		db:    db,
		cards: make(map[string]*NFCCard),
	}
	s.seedDefaultCards()
	return s
}

func (s *Service) seedDefaultCards() {
	s.mu.Lock()
	defer s.mu.Unlock()

	defaultUid := "BLM-9921-NFC"
	sig := SignCardUID(defaultUid)
	s.cards[defaultUid] = &NFCCard{
		ID:         "card-demo-1",
		CardUid:    defaultUid,
		FinishName: "Stealth Matte Black",
		Status:     "claimed",
		Signature:  sig,
		CreatedAt:  time.Now(),
	}
}

// CreateCard pushes a single unique tag to DB
func (s *Service) CreateCard(ctx context.Context, cardUid, finishName, status string) (*NFCCard, error) {
	cardUid = strings.TrimSpace(cardUid)
	if cardUid == "" {
		return nil, errors.New("cardUid cannot be empty")
	}

	if finishName == "" {
		finishName = "Stealth Matte Black"
	}
	if status == "" {
		status = "provisioned"
	}

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://www.enlazer.com.ng"
	}
	sig := SignCardUID(cardUid)
	signedURL := fmt.Sprintf("%s/card/%s?sig=%s", frontendOrigin, cardUid, sig)

	if s.db != nil {
		query := `
		INSERT INTO nfc_cards (card_uid, finish_name, status, taps_count, created_at)
		VALUES ($1, $2, $3, 0, NOW())
		RETURNING id, card_uid, user_id, finish_name, status, taps_count, created_at
		`
		var c NFCCard
		var userIDNull sql.NullString
		err := s.db.QueryRowContext(ctx, query, cardUid, finishName, status).Scan(
			&c.ID, &c.CardUid, &userIDNull, &c.FinishName, &c.Status, &c.TapsCount, &c.CreatedAt,
		)
		if err != nil {
			if strings.Contains(err.Error(), "unique constraint") || strings.Contains(err.Error(), "duplicate key") {
				return nil, ErrCardUidExists
			}
			return nil, err
		}
		if userIDNull.Valid {
			c.UserID = &userIDNull.String
		}
		c.Signature = sig
		c.SignedURL = signedURL
		return &c, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.cards[cardUid]; exists {
		return nil, ErrCardUidExists
	}

	c := &NFCCard{
		ID:         fmt.Sprintf("card-%d", time.Now().UnixNano()),
		CardUid:    cardUid,
		FinishName: finishName,
		Status:     status,
		Signature:  sig,
		SignedURL:  signedURL,
		CreatedAt:  time.Now(),
	}
	s.cards[cardUid] = c
	return c, nil
}

// ProvisionBatch pre-seeds a batch of card UIDs in database as 'provisioned' and returns signed URLs.
func (s *Service) ProvisionBatch(ctx context.Context, cardUids []string, finishName string) ([]NFCCard, error) {
	if len(cardUids) == 0 {
		return nil, errors.New("cardUids array cannot be empty")
	}

	if finishName == "" {
		finishName = "Stealth Matte Black"
	}

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://www.enlazer.com.ng"
	}

	var result []NFCCard

	if s.db != nil {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()

		for _, rawUid := range cardUids {
			cardUid := strings.TrimSpace(rawUid)
			if cardUid == "" {
				continue
			}

			sig := SignCardUID(cardUid)
			signedURL := fmt.Sprintf("%s/card/%s?sig=%s", frontendOrigin, cardUid, sig)

			query := `
			INSERT INTO nfc_cards (card_uid, finish_name, status, taps_count, created_at)
			VALUES ($1, $2, 'provisioned', 0, NOW())
			ON CONFLICT (card_uid) DO UPDATE SET finish_name = EXCLUDED.finish_name
			RETURNING id, card_uid, user_id, finish_name, status, taps_count, created_at
			`
			var c NFCCard
			var userID sql.NullString
			err := tx.QueryRowContext(ctx, query, cardUid, finishName).Scan(
				&c.ID, &c.CardUid, &userID, &c.FinishName, &c.Status, &c.TapsCount, &c.CreatedAt,
			)
			if err != nil {
				log.Printf("cards: warning provision error for %s: %v", cardUid, err)
				continue
			}
			if userID.Valid {
				c.UserID = &userID.String
			}
			c.Signature = sig
			c.SignedURL = signedURL
			result = append(result, c)
		}

		if err := tx.Commit(); err != nil {
			return nil, err
		}

		return result, nil
	}

	// Fallback in-memory
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, rawUid := range cardUids {
		cardUid := strings.TrimSpace(rawUid)
		if cardUid == "" {
			continue
		}
		sig := SignCardUID(cardUid)
		signedURL := fmt.Sprintf("%s/card/%s?sig=%s", frontendOrigin, cardUid, sig)

		c := &NFCCard{
			ID:         fmt.Sprintf("card-%d", time.Now().UnixNano()),
			CardUid:    cardUid,
			FinishName: finishName,
			Status:     "provisioned",
			Signature:  sig,
			SignedURL:  signedURL,
			CreatedAt:  time.Now(),
		}
		s.cards[cardUid] = c
		result = append(result, *c)
	}

	return result, nil
}

// ListAllCards returns list of all tags in database
func (s *Service) ListAllCards(ctx context.Context) ([]NFCCard, error) {
	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://www.enlazer.com.ng"
	}

	if s.db != nil {
		query := `
		SELECT c.id, c.card_uid, c.user_id, c.finish_name, c.status, c.taps_count, c.created_at, COALESCE(u.username, u.name, '')
		FROM nfc_cards c
		LEFT JOIN users u ON c.user_id = u.id
		ORDER BY c.created_at DESC
		`
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var list []NFCCard
		for rows.Next() {
			var c NFCCard
			var uNull sql.NullString
			var usernameNull sql.NullString
			if err := rows.Scan(&c.ID, &c.CardUid, &uNull, &c.FinishName, &c.Status, &c.TapsCount, &c.CreatedAt, &usernameNull); err == nil {
				if uNull.Valid {
					c.UserID = &uNull.String
				}
				if usernameNull.Valid && usernameNull.String != "" {
					c.LinkedUser = usernameNull.String
				}
				c.Signature = SignCardUID(c.CardUid)
				c.SignedURL = fmt.Sprintf("%s/card/%s?sig=%s", frontendOrigin, c.CardUid, c.Signature)
				list = append(list, c)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if list == nil {
			list = []NFCCard{}
		}
		return list, nil
	}


	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []NFCCard
	for _, c := range s.cards {
		res := *c
		res.Signature = SignCardUID(c.CardUid)
		res.SignedURL = fmt.Sprintf("%s/card/%s?sig=%s", frontendOrigin, c.CardUid, res.Signature)
		list = append(list, res)
	}
	return list, nil
}

// UpdateCard updates an existing NFC tag record
func (s *Service) UpdateCard(ctx context.Context, targetUid string, payload UpdateCardPayload) (*NFCCard, error) {
	targetUid = strings.TrimSpace(targetUid)

	if s.db != nil {
		var newUid, finishName, status string
		var userIDNull sql.NullString

		_ = s.db.QueryRowContext(ctx, `SELECT card_uid, finish_name, status, user_id FROM nfc_cards WHERE card_uid = $1`, targetUid).Scan(&newUid, &finishName, &status, &userIDNull)

		if payload.CardUid != nil && *payload.CardUid != "" {
			newUid = strings.TrimSpace(*payload.CardUid)
		}
		if payload.FinishName != nil && *payload.FinishName != "" {
			finishName = *payload.FinishName
		}
		if payload.Status != nil && *payload.Status != "" {
			status = *payload.Status
		}

		query := `
		UPDATE nfc_cards
		SET card_uid = $1, finish_name = $2, status = $3
		WHERE card_uid = $4
		RETURNING id, card_uid, user_id, finish_name, status, taps_count, created_at
		`
		var c NFCCard
		err := s.db.QueryRowContext(ctx, query, newUid, finishName, status, targetUid).Scan(
			&c.ID, &c.CardUid, &userIDNull, &c.FinishName, &c.Status, &c.TapsCount, &c.CreatedAt,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrCardNotFound
			}
			return nil, err
		}
		if userIDNull.Valid {
			c.UserID = &userIDNull.String
		}
		c.Signature = SignCardUID(c.CardUid)
		return &c, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	c, exists := s.cards[targetUid]
	if !exists {
		return nil, ErrCardNotFound
	}

	if payload.CardUid != nil && *payload.CardUid != "" {
		delete(s.cards, targetUid)
		c.CardUid = *payload.CardUid
		s.cards[c.CardUid] = c
	}
	if payload.FinishName != nil {
		c.FinishName = *payload.FinishName
	}
	if payload.Status != nil {
		c.Status = *payload.Status
	}

	res := *c
	return &res, nil
}

// DeleteCard deletes an NFC tag record from DB
func (s *Service) DeleteCard(ctx context.Context, cardUid string) error {
	cardUid = strings.TrimSpace(cardUid)

	if s.db != nil {
		res, err := s.db.ExecContext(ctx, `DELETE FROM nfc_cards WHERE card_uid = $1`, cardUid)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return ErrCardNotFound
		}
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.cards[cardUid]; !exists {
		return ErrCardNotFound
	}
	delete(s.cards, cardUid)
	return nil
}

// ClaimCard claims a provisioned card for authenticated user
func (s *Service) ClaimCard(ctx context.Context, userID, cardUid string) (*NFCCard, error) {
	cardUid = strings.TrimSpace(cardUid)
	if cardUid == "" {
		return nil, errors.New("cardUid is required")
	}

	if s.db != nil {
		var status string
		var existingUserID sql.NullString
		err := s.db.QueryRowContext(ctx, `SELECT status, user_id FROM nfc_cards WHERE card_uid = $1`, cardUid).Scan(&status, &existingUserID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Auto-provision if cardUid does not exist yet
				_, _ = s.db.ExecContext(ctx, `INSERT INTO nfc_cards (card_uid, user_id, status) VALUES ($1, $2, 'claimed')`, cardUid, userID)
			} else {
				return nil, err
			}
		} else {
			if status == "claimed" && existingUserID.Valid && existingUserID.String != userID {
				return nil, ErrCardAlreadyClaimed
			}
			_, err = s.db.ExecContext(ctx, `UPDATE nfc_cards SET user_id = $1, status = 'claimed' WHERE card_uid = $2`, userID, cardUid)
			if err != nil {
				return nil, err
			}
		}

		// Update profile table link
		res, err := s.db.ExecContext(ctx, `UPDATE profiles SET card_uid = $2 WHERE user_id = $1`, userID, cardUid)
		if err == nil {
			if rows, _ := res.RowsAffected(); rows == 0 {
				_, _ = s.db.ExecContext(ctx, `INSERT INTO profiles (user_id, card_uid) VALUES ($1, $2)`, userID, cardUid)
			}
		}

		var c NFCCard
		var uNull sql.NullString
		_ = s.db.QueryRowContext(ctx, `SELECT id, card_uid, user_id, finish_name, status, taps_count, created_at FROM nfc_cards WHERE card_uid = $1`, cardUid).Scan(
			&c.ID, &c.CardUid, &uNull, &c.FinishName, &c.Status, &c.TapsCount, &c.CreatedAt,
		)
		if uNull.Valid {
			c.UserID = &uNull.String
		}
		c.Signature = SignCardUID(cardUid)
		return &c, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	c, exists := s.cards[cardUid]
	if !exists {
		c = &NFCCard{
			ID:         fmt.Sprintf("card-%d", time.Now().UnixNano()),
			CardUid:    cardUid,
			FinishName: "Stealth Matte Black",
			CreatedAt:  time.Now(),
		}
		s.cards[cardUid] = c
	} else if c.Status == "claimed" && c.UserID != nil && *c.UserID != userID {
		return nil, ErrCardAlreadyClaimed
	}

	c.UserID = &userID
	c.Status = "claimed"
	c.Signature = SignCardUID(cardUid)
	res := *c
	return &res, nil
}

// GetCardStatus retrieves card details by cardUid
func (s *Service) GetCardStatus(ctx context.Context, cardUid string) (*NFCCard, error) {
	cardUid = strings.TrimSpace(cardUid)
	if s.db != nil {
		var c NFCCard
		var userIDNull sql.NullString
		query := `SELECT id, card_uid, user_id, finish_name, status, taps_count, created_at FROM nfc_cards WHERE card_uid = $1`
		err := s.db.QueryRowContext(ctx, query, cardUid).Scan(
			&c.ID, &c.CardUid, &userIDNull, &c.FinishName, &c.Status, &c.TapsCount, &c.CreatedAt,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrCardNotFound
			}
			return nil, err
		}
		if userIDNull.Valid {
			c.UserID = &userIDNull.String
		}
		c.Signature = SignCardUID(cardUid)
		return &c, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	c, exists := s.cards[cardUid]
	if !exists {
		return nil, ErrCardNotFound
	}
	res := *c
	return &res, nil
}

// Handler handles card HTTP endpoints
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

// HandleCreateCard handles POST /api/admin/cards (Single Tag Push)
func (h *Handler) HandleCreateCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req CreateCardPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	card, err := h.svc.CreateCard(r.Context(), req.CardUid, req.FinishName, req.Status)
	if err != nil {
		if errors.Is(err, ErrCardUidExists) {
			writeError(w, http.StatusConflict, "cardUid already exists in database and must be unique")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, card)
}

// HandleListCards handles GET /api/admin/cards (List All Tags)
func (h *Handler) HandleListCards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cardsList, err := h.svc.ListAllCards(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, cardsList)
}

// HandleUpdateCard handles PUT /api/admin/cards/{cardUid} (Update Tag)
func (h *Handler) HandleUpdateCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cardUid := r.PathValue("cardUid")
	if cardUid == "" {
		path := strings.TrimPrefix(r.URL.Path, "/api/admin/cards/")
		cardUid = strings.TrimPrefix(path, ":")
	}
	if cardUid == "" {
		writeError(w, http.StatusBadRequest, "cardUid parameter is required")
		return
	}

	var req UpdateCardPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	card, err := h.svc.UpdateCard(r.Context(), cardUid, req)
	if err != nil {
		if errors.Is(err, ErrCardNotFound) {
			writeError(w, http.StatusNotFound, "cardUid not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// HandleDeleteCard handles DELETE /api/admin/cards/{cardUid} (Delete Tag)
func (h *Handler) HandleDeleteCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cardUid := r.PathValue("cardUid")
	if cardUid == "" {
		path := strings.TrimPrefix(r.URL.Path, "/api/admin/cards/")
		cardUid = strings.TrimPrefix(path, ":")
	}
	if cardUid == "" {
		writeError(w, http.StatusBadRequest, "cardUid parameter is required")
		return
	}

	if err := h.svc.DeleteCard(r.Context(), cardUid); err != nil {
		if errors.Is(err, ErrCardNotFound) {
			writeError(w, http.StatusNotFound, "cardUid not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "card tag deleted successfully"})
}

// HandleBatchProvision handles POST /api/admin/cards/provision
func (h *Handler) HandleBatchProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ProvisionBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cardsList, err := h.svc.ProvisionBatch(r.Context(), req.CardUids, req.FinishName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, ProvisionBatchResponse{
		Status: "success",
		Count:  len(cardsList),
		Cards:  cardsList,
	})
}

// GetUserCards returns list of physical cards claimed by user
func (s *Service) GetUserCards(ctx context.Context, userID string) ([]NFCCard, error) {
	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://www.enlazer.com.ng"
	}

	if s.db != nil {
		rows, err := s.db.QueryContext(ctx, `SELECT id, card_uid, user_id, finish_name, status, taps_count, created_at FROM nfc_cards WHERE user_id = $1 ORDER BY created_at DESC`, userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var list []NFCCard
		for rows.Next() {
			var c NFCCard
			var uNull sql.NullString
			if err := rows.Scan(&c.ID, &c.CardUid, &uNull, &c.FinishName, &c.Status, &c.TapsCount, &c.CreatedAt); err == nil {
				if uNull.Valid {
					c.UserID = &uNull.String
				}
				c.Signature = SignCardUID(c.CardUid)
				c.SignedURL = fmt.Sprintf("%s/card/%s?sig=%s", frontendOrigin, c.CardUid, c.Signature)
				list = append(list, c)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if list == nil {
			list = []NFCCard{}
		}
		return list, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []NFCCard
	for _, c := range s.cards {
		if c.UserID != nil && *c.UserID == userID {
			res := *c
			res.Signature = SignCardUID(c.CardUid)
			res.SignedURL = fmt.Sprintf("%s/card/%s?sig=%s", frontendOrigin, c.CardUid, res.Signature)
			list = append(list, res)
		}
	}
	return list, nil
}

// HandleClaimCard handles POST /api/cards/claim
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

	var req struct {
		CardUid string `json:"cardUid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CardUid == "" {
		writeError(w, http.StatusBadRequest, "cardUid is required")
		return
	}

	card, err := h.svc.ClaimCard(r.Context(), user.ID, req.CardUid)
	if err != nil {
		if errors.Is(err, ErrCardAlreadyClaimed) {
			writeError(w, http.StatusConflict, "this card has already been claimed by another user")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// HandleActivateCard handles POST /api/cards/activate (Enlazer Specification)
func (h *Handler) HandleActivateCard(w http.ResponseWriter, r *http.Request) {
	h.HandleClaimCard(w, r)
}

// HandleGetMyCards handles GET /api/cards/me
func (h *Handler) HandleGetMyCards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := auth.CurrentUserFromContext(r)
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	list, err := h.svc.GetUserCards(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, list)
}
