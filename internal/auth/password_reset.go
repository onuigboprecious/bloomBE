package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type PasswordReset struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Used      bool      `json:"used"`
	CreatedAt time.Time `json:"created_at"`
}

type forgotPasswordPayload struct {
	Email string `json:"email"`
}

type resetPasswordPayload struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

// generateResetToken creates a 32-byte hex token string.
func generateResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HandleForgotPassword handles POST /api/auth/forgot-password
func (s *Service) HandleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req forgotPasswordPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		writeJSONError(w, http.StatusBadRequest, "email is required")
		return
	}

	user, found := findUserByEmail(email)
	if !found {
		// Return generic success to prevent account enumeration attacks
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "success",
			"message": "If this email is registered, a password reset link has been sent.",
		})
		return
	}

	token, err := generateResetToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate reset token")
		return
	}

	expiresAt := time.Now().Add(1 * time.Hour)

	if s.db != nil {
		query := `INSERT INTO password_resets (email, token, expires_at, used, created_at) VALUES ($1, $2, $3, false, NOW())`
		if _, err := s.db.ExecContext(r.Context(), query, user.Email, token, expiresAt); err != nil {
			log.Printf("auth: warning password reset token DB error: %v", err)
		}
	}

	mu.Lock()
	resetStore[token] = &PasswordReset{
		Email:     user.Email,
		Token:     token,
		ExpiresAt: expiresAt,
		Used:      false,
		CreatedAt: time.Now(),
	}
	mu.Unlock()

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://www.enlazer.com.ng"
	}
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", frontendOrigin, token)

	// Asynchronously dispatch email via Resend API
	go sendResendResetEmail(user.Email, resetURL)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "success",
		"message":    "If this email is registered, a password reset link has been sent.",
		"resetToken": token,
		"resetUrl":   resetURL,
	})
}

// sendResendResetEmail sends a password reset email via Resend API.
func sendResendResetEmail(toEmail, resetURL string) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		log.Println("auth: notice - RESEND_API_KEY env var not set. Skipping live Resend email dispatch.")
		return
	}

	payload := map[string]interface{}{
		"from":    "Enlazer <onboarding@resend.dev>",
		"to":      []string{toEmail},
		"subject": "Reset Your Enlazer Password",
		"html": fmt.Sprintf(`
			<div style="font-family: sans-serif; max-width: 500px; margin: 0 auto; padding: 20px;">
				<h2 style="color: #0F172A;">Reset Your Password</h2>
				<p>We received a request to reset your password for your Enlazer account.</p>
				<p style="margin: 25px 0;">
					<a href="%s" style="background-color: #00BCFF; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold; display: inline-block;">Reset Password</a>
				</p>
				<p style="color: #666; font-size: 13px;">Or copy and paste this URL into your browser:<br/><a href="%s">%s</a></p>
				<p style="color: #888; font-size: 12px; margin-top: 30px;">This link will expire in 1 hour. If you did not request a password reset, please ignore this email.</p>
			</div>
		`, resetURL, resetURL, resetURL),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("auth: failed to marshal Resend email: %v", err)
		return
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Printf("auth: failed to create Resend request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("auth: failed to send Resend email: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("auth: Resend email sent to %s (Status: %d)", toEmail, resp.StatusCode)
}

// HandleResetPassword handles POST /api/auth/reset-password
func (s *Service) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req resetPasswordPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" || req.NewPassword == "" {
		writeJSONError(w, http.StatusBadRequest, "token and newPassword are required")
		return
	}

	if len(req.NewPassword) < 6 {
		writeJSONError(w, http.StatusBadRequest, "new password must be at least 6 characters")
		return
	}

	email, err := s.validateAndConsumeResetToken(r.Context(), req.Token)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	newHash, err := hashPassword(req.NewPassword)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to hash new password")
		return
	}

	if s.db != nil {
		_, err := s.db.ExecContext(r.Context(), `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE LOWER(email) = $2`, newHash, email)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to update password: "+err.Error())
			return
		}
		// Invalidate active sessions
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE LOWER(email) = $1)`, email)
	}

	mu.Lock()
	if uID, ok := byEmail[email]; ok {
		if u, exists := users[uID]; exists {
			u.PasswordHash = newHash
		}
	}
	mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Password reset successfully. You can now log in with your new password.",
	})
}

func (s *Service) validateAndConsumeResetToken(ctx context.Context, token string) (string, error) {
	if s.db != nil {
		var email string
		var expiresAt time.Time
		var used bool
		query := `SELECT email, expires_at, used FROM password_resets WHERE token = $1`
		err := s.db.QueryRowContext(ctx, query, token).Scan(&email, &expiresAt, &used)
		if err != nil {
			return "", errors.New("invalid or non-existent reset token")
		}
		if used {
			return "", errors.New("this reset token has already been used")
		}
		if time.Now().After(expiresAt) {
			return "", errors.New("this reset token has expired")
		}

		_, _ = s.db.ExecContext(ctx, `UPDATE password_resets SET used = true WHERE token = $1`, token)
		return email, nil
	}

	mu.Lock()
	defer mu.Unlock()

	pr, ok := resetStore[token]
	if !ok {
		return "", errors.New("invalid or non-existent reset token")
	}
	if pr.Used {
		return "", errors.New("this reset token has already been used")
	}
	if time.Now().After(pr.ExpiresAt) {
		return "", errors.New("this reset token has expired")
	}

	pr.Used = true
	return pr.Email, nil
}
