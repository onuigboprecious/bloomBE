package cards_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
	"github.com/onuigboprecious/infarbloom/backend/internal/cards"
)

func TestHMACSignature(t *testing.T) {
	cardUid := "BLM-88A92K-NFC"
	sig := cards.SignCardUID(cardUid)

	if len(sig) != 8 {
		t.Fatalf("expected 8-character signature hex, got %s", sig)
	}

	if !cards.VerifyCardSignature(cardUid, sig) {
		t.Fatalf("signature verification failed for valid signature %s", sig)
	}

	if cards.VerifyCardSignature(cardUid, "invalid_sig") {
		t.Fatalf("expected signature verification to fail for fake signature")
	}
}

func TestBatchProvisioningAndClaiming(t *testing.T) {
	authSvc := auth.New(nil, "development")
	cardsSvc := cards.NewService(nil)
	handler := cards.NewHandler(cardsSvc, authSvc)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/signup", authSvc.HandleSignup)
	mux.HandleFunc("POST /api/admin/cards/provision", handler.HandleBatchProvision)
	mux.HandleFunc("POST /api/cards/claim", handler.HandleClaimCard)

	server := httptest.NewServer(mux)
	defer server.Close()
	client := server.Client()

	// 1. Batch provision 2 cards
	provReq := cards.ProvisionBatchRequest{
		CardUids:   []string{"BLM-TEST-001", "BLM-TEST-002"},
		FinishName: "Stealth Matte Black",
	}
	provBody, _ := json.Marshal(provReq)
	resp, err := client.Post(server.URL+"/api/admin/cards/provision", "application/json", bytes.NewBuffer(provBody))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on batch provision, got status %d, err: %v", resp.StatusCode, err)
	}

	var provRes cards.ProvisionBatchResponse
	_ = json.NewDecoder(resp.Body).Decode(&provRes)
	if provRes.Count != 2 || len(provRes.Cards) != 2 {
		t.Fatalf("unexpected provision response: %+v", provRes)
	}

	if provRes.Cards[0].Signature == "" || provRes.Cards[0].SignedURL == "" {
		t.Fatalf("expected signature and signedUrl in card response, got %+v", provRes.Cards[0])
	}

	// 2. Signup user to get session cookie
	signupBody, _ := json.Marshal(map[string]string{
		"email":    "cardclaimer@example.com",
		"password": "Password123!",
		"name":     "Card Claimer",
	})
	resp, err = client.Post(server.URL+"/api/auth/signup", "application/json", bytes.NewBuffer(signupBody))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup failed: %v, status: %d", err, resp.StatusCode)
	}
	cookies := resp.Cookies()

	// 3. Claim provisioned card "BLM-TEST-001"
	claimPayload, _ := json.Marshal(map[string]string{
		"cardUid": "BLM-TEST-001",
	})
	claimReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/cards/claim", bytes.NewBuffer(claimPayload))
	claimReq.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		claimReq.AddCookie(c)
	}

	resp, err = client.Do(claimReq)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on card claim, got status %d", resp.StatusCode)
	}

	var claimedCard cards.NFCCard
	_ = json.NewDecoder(resp.Body).Decode(&claimedCard)
	if claimedCard.Status != "claimed" || claimedCard.CardUid != "BLM-TEST-001" {
		t.Fatalf("unexpected claimed card payload: %+v", claimedCard)
	}
}
