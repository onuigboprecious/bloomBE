package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
)

func TestPasswordResetFlow(t *testing.T) {
	svc := auth.New(nil, "development")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/signup", svc.HandleSignup)
	mux.HandleFunc("POST /api/auth/login", svc.HandleLogin)
	mux.HandleFunc("POST /api/auth/forgot-password", svc.HandleForgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password", svc.HandleResetPassword)

	server := httptest.NewServer(mux)
	defer server.Close()
	client := server.Client()

	// 1. Signup user
	signupBody, _ := json.Marshal(map[string]string{
		"email":    "resetuser@example.com",
		"password": "OldPassword123!",
		"name":     "Reset User",
	})
	resp, err := client.Post(server.URL+"/api/auth/signup", "application/json", bytes.NewBuffer(signupBody))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup failed: %v, status: %d", err, resp.StatusCode)
	}

	// 2. Request Forgot Password
	forgotBody, _ := json.Marshal(map[string]string{
		"email": "resetuser@example.com",
	})
	resp, err = client.Post(server.URL+"/api/auth/forgot-password", "application/json", bytes.NewBuffer(forgotBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("forgot password failed: %v, status: %d", err, resp.StatusCode)
	}

	var forgotRes map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&forgotRes)
	token, ok := forgotRes["resetToken"].(string)
	if !ok || token == "" {
		t.Fatalf("expected resetToken in response, got %+v", forgotRes)
	}

	// 3. Reset Password using Token
	resetBody, _ := json.Marshal(map[string]string{
		"token":       token,
		"newPassword": "NewPassword456!",
	})
	resp, err = client.Post(server.URL+"/api/auth/reset-password", "application/json", bytes.NewBuffer(resetBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("reset password failed: %v, status: %d", err, resp.StatusCode)
	}

	// 4. Try Login with Old Password -> 401
	oldLogin, _ := json.Marshal(map[string]string{
		"email":    "resetuser@example.com",
		"password": "OldPassword123!",
	})
	resp, err = client.Post(server.URL+"/api/auth/login", "application/json", bytes.NewBuffer(oldLogin))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for old password, got %d", resp.StatusCode)
	}

	// 5. Login with New Password -> 200 OK
	newLogin, _ := json.Marshal(map[string]string{
		"email":    "resetuser@example.com",
		"password": "NewPassword456!",
	})
	resp, err = client.Post(server.URL+"/api/auth/login", "application/json", bytes.NewBuffer(newLogin))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for new password, got %d", resp.StatusCode)
	}

	// 6. Attempt token reuse -> 400 Bad Request
	resp, err = client.Post(server.URL+"/api/auth/reset-password", "application/json", bytes.NewBuffer(resetBody))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for reused token, got %d", resp.StatusCode)
	}
}
