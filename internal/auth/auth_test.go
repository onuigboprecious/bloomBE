package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthFlow(t *testing.T) {
	mux := http.NewServeMux()
	registerAuthRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()

	// 1. Signup
	signupBody, _ := json.Marshal(signupRequest{
		Email:    "newuser@example.com",
		Password: "password123",
		Name:     "Test User",
	})
	resp, err := client.Post(server.URL+"/api/auth/signup", "application/json", bytes.NewBuffer(signupBody))
	if err != nil {
		t.Fatalf("signup request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.StatusCode)
	}

	// Extract session cookie from signup response
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie on signup")
	}

	// 2. Fetch /api/auth/me with session cookie
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/auth/me", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("me request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on /me, got %d", resp.StatusCode)
	}

	var userResp UserPublic
	if err := json.NewDecoder(resp.Body).Decode(&userResp); err != nil {
		t.Fatalf("failed to decode me response: %v", err)
	}
	if userResp.Email != "newuser@example.com" {
		t.Errorf("expected email newuser@example.com, got %s", userResp.Email)
	}

	// 3. Logout
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on logout, got %d", resp.StatusCode)
	}

	// 4. Login
	loginBody, _ := json.Marshal(loginRequest{
		Email:    "newuser@example.com",
		Password: "password123",
	})
	resp, err = client.Post(server.URL+"/api/auth/login", "application/json", bytes.NewBuffer(loginBody))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on login, got %d", resp.StatusCode)
	}
}
