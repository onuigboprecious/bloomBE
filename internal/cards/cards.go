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
)

type NFCCard struct {
	ID         string    `json:"id"`
	CardUid    string    `json:"cardUid"`
	UserID     *string   `json:"userId,omitempty"`
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
	Status      string    `json:"status"`
	Count       int       `json:"count"`
	Cards       []NFCCard `json:"cards"`
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
		frontendOrigin = "https://capstone-project.name.ng"
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
		_, _ = s.db.ExecContext(ctx, `INSERT INTO profiles (user_id, card_uid) VALUES ($1, $2) ON CONFLICT (user_id) DO UPDATE SET card_uid = $2`, userID, cardUid)

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
