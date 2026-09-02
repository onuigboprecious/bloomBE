package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHandleGoogleAuth_EmptyToken(t *testing.T) {
	mux := http.NewServeMux()
	registerAuthRoutes(mux)

	body, _ := json.Marshal(map[string]string{
		"token": "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/google", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 Bad Request, got %d", w.Code)
	}
}

func TestHandleGoogleAuth_InvalidToken(t *testing.T) {
	mux := http.NewServeMux()
	registerAuthRoutes(mux)

	body, _ := json.Marshal(map[string]string{
		"token": "invalid_mock_token_12345",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/google", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized, got %d", w.Code)
	}
}

func TestHandleGoogleOAuthLogin_Redirect(t *testing.T) {
	mux := http.NewServeMux()
	registerAuthRoutes(mux)

	// Case 1: Unconfigured client ID
	_ = os.Unsetenv("GOOGLE_CLIENT_ID")
	req := httptest.NewRequest(http.MethodGet, "/api/auth/google/login", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 when GOOGLE_CLIENT_ID is missing, got %d", w.Code)
	}

	// Case 2: Configured client ID
	_ = os.Setenv("GOOGLE_CLIENT_ID", "test-client-id.apps.googleusercontent.com")
	defer os.Unsetenv("GOOGLE_CLIENT_ID")

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected status 307 Temporary Redirect, got %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location == "" || !bytes.Contains([]byte(location), []byte("accounts.google.com")) {
		t.Errorf("expected redirect location to point to accounts.google.com, got %s", location)
	}
}

func TestProcessGoogleUser_NewAndExisting(t *testing.T) {
	w := httptest.NewRecorder()
	info := &GoogleTokenInfo{
		Sub:     "google_sub_998877",
		Email:   "googleuser@example.com",
		Name:    "Google Test User",
		Picture: "https://lh3.googleusercontent.com/a/mock_photo.jpg",
	}

	user, session, err := processGoogleUser(w, info)
	if err != nil {
		t.Fatalf("processGoogleUser failed: %v", err)
	}

	if user.Email != "googleuser@example.com" {
		t.Errorf("expected email googleuser@example.com, got %s", user.Email)
	}

	if user.GoogleID != "google_sub_998877" {
		t.Errorf("expected google_id google_sub_998877, got %s", user.GoogleID)
	}

	if session.Token == "" {
		t.Errorf("expected non-empty session token")
	}

	// Process existing user again
	w2 := httptest.NewRecorder()
	user2, session2, err := processGoogleUser(w2, info)
	if err != nil {
		t.Fatalf("processGoogleUser second call failed: %v", err)
	}

	if user2.ID != user.ID {
		t.Errorf("expected same user ID %s for existing user, got %s", user.ID, user2.ID)
	}

	if session2.Token == "" {
		t.Errorf("expected non-empty session token on re-auth")
	}
}
