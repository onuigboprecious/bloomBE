package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// handleSignup handles new user registration and logs them in.
func handleSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)

	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "name, email, and password are required")
		return
	}

	if len(req.Password) < 6 {
		writeJSONError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	if _, found := findUserByEmail(req.Email); found {
		writeJSONError(w, http.StatusConflict, "user with this email already exists")
		return
	}

	idBytes := make([]byte, 16)
	_, _ = rand.Read(idBytes)
	userID := hex.EncodeToString(idBytes)

	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	newUser := &User{
		ID:           userID,
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: hashedPassword,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := saveUser(newUser); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}

	if _, err := createSessionAndSetCookie(w, newUser.ID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	_ = writeJSON(w, http.StatusCreated, newUser.ToPublic())
}
