package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Username string `json:"username"`
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
	req.Username = strings.TrimSpace(strings.ToLower(req.Username))

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

	if req.Username == "" {
		parts := strings.Split(req.Email, "@")
		reg := regexp.MustCompile("[^a-zA-Z0-9_-]")
		req.Username = reg.ReplaceAllString(parts[0], "")
	}

	idBytes := make([]byte, 16)
	_, _ = rand.Read(idBytes)
	userID := "usr_" + hex.EncodeToString(idBytes)[:8]

	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	newUser := &User{
		ID:           userID,
		Email:        req.Email,
		Name:         req.Name,
		Username:     req.Username,
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

	// Asynchronously dispatch Welcome Email via Resend
	go sendResendWelcomeEmail(newUser.Email, newUser.Name, newUser.Username)

	_ = writeJSON(w, http.StatusCreated, newUser.ToPublic())
}

// sendResendWelcomeEmail sends a welcome email to newly registered users via Resend.
func sendResendWelcomeEmail(toEmail, userName, username string) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		log.Println("auth: notice - RESEND_API_KEY env var not set. Skipping welcome email.")
		return
	}

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://capstone-project.name.ng"
	}
	profileURL := fmt.Sprintf("%s/@%s", frontendOrigin, username)

	payload := map[string]interface{}{
		"from":    "Bloom <onboarding@resend.dev>",
		"to":      []string{toEmail},
		"subject": "Welcome to Bloom! 🎉 Your Digital Card is Ready",
		"html": fmt.Sprintf(`
			<div style="font-family: Arial, sans-serif; max-width: 600px; margin: 0 auto; padding: 20px; border: 1px solid #eee; border-radius: 10px;">
				<h2 style="color: #00BCFF;">Welcome to Bloom, %s! 🎉</h2>
				<p>Your account and digital business card profile have been created successfully.</p>
				<p style="margin: 25px 0;">
					<a href="%s" style="background-color: #00BCFF; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold; display: inline-block;">View My Digital Card</a>
				</p>
				<p style="color: #666; font-size: 13px;">Your profile link:<br/><a href="%s">%s</a></p>
				<p style="color: #888; font-size: 12px; margin-top: 30px;">Thank you for joining Bloom! If you need any help, reply directly to this email.</p>
			</div>
		`, userName, profileURL, profileURL, profileURL),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("auth: failed to marshal welcome email: %v", err)
		return
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Printf("auth: failed to create welcome email request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("auth: failed to send welcome email: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("auth: Resend welcome email sent to %s (Status: %d)", toEmail, resp.StatusCode)
}
