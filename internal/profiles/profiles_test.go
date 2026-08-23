package profiles_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
	"github.com/onuigboprecious/infarbloom/backend/internal/models"
	"github.com/onuigboprecious/infarbloom/backend/internal/profiles"
)

func setupTestServer() (*httptest.Server, *auth.Service, *profiles.Handler) {
	authSvc := auth.New(nil, "development")
	svc := profiles.NewService(nil)
	handler := profiles.NewHandler(svc, authSvc)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/signup", authSvc.HandleSignup)
	mux.HandleFunc("POST /api/auth/login", authSvc.HandleLogin)

	mux.HandleFunc("GET /api/profile/{username}", handler.HandleGetProfile)
	mux.HandleFunc("PUT /api/profile/me", handler.HandleUpdateMyProfile)
	mux.HandleFunc("GET /api/profile/check-handle", handler.HandleCheckHandle)
	mux.HandleFunc("POST /api/cards/claim", handler.HandleClaimCard)
	mux.HandleFunc("GET /api/vcard/{username}", handler.HandleGetVCard)

	server := httptest.NewServer(mux)
	return server, authSvc, handler
}

func TestBloomProfileFlow(t *testing.T) {
	server, _, _ := setupTestServer()
	defer server.Close()

	client := server.Client()

	// 1. GET /api/profile/precious -> returns default profile 200 OK
	resp, err := client.Get(server.URL + "/api/profile/precious")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for precious profile, got %d, err: %v", resp.StatusCode, err)
	}

	var p models.BloomProfile
	_ = json.NewDecoder(resp.Body).Decode(&p)
	if p.Username != "precious" || p.Name == "" {
		t.Fatalf("unexpected profile payload: %+v", p)
	}

	// 2. GET /api/vcard/precious -> returns .vcf content
	resp, err = client.Get(server.URL + "/api/vcard/precious")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for vcard, got %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType == "" {
		t.Fatalf("expected Content-Type text/vcard, got %s", contentType)
	}

	// 3. GET /api/profile/check-handle?username=precious -> false (taken)
	resp, err = client.Get(server.URL + "/api/profile/check-handle?username=precious")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("check handle failed: %v", err)
	}

	var handleRes map[string]bool
	_ = json.NewDecoder(resp.Body).Decode(&handleRes)
	if handleRes["available"] != false {
		t.Fatalf("expected handle 'precious' to be unavailable, got %+v", handleRes)
	}

	// 4. PUT /api/profile/me without auth -> 401
	newTitle := "VP of Technology"
	updateBody, _ := json.Marshal(models.UpdateBloomProfileRequest{Title: &newTitle})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/profile/me", bytes.NewBuffer(updateBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for update without login, got %d", resp.StatusCode)
	}
}
