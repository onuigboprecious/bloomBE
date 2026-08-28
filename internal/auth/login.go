package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// hashPassword hashes a password using bcrypt.
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// checkPassword checks a password against a hashed password using bcrypt (with legacy fallback).
func checkPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// handleLogin validates user credentials and establishes an authenticated session.
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if req.Email == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, found := findUserByEmail(req.Email)
	if !found || !checkPassword(user.PasswordHash, req.Password) {
		writeJSONError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	session, err := createSessionAndSetCookie(w, user.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	pub := user.ToPublic()
	_ = writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":     session.Token,
		"id":        pub.ID,
		"name":      pub.Name,
		"email":     pub.Email,
		"username":  pub.Username,
		"createdAt": pub.CreatedAt,
		"user":      pub,
	})
}

