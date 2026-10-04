package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		frontendOrigin = "https://enlazer.cloud"
	}
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", frontendOrigin, token)

	// Asynchronously dispatch email via Resend API
	go sendResendResetEmail(user.Email, user.Name, user.Username, resetURL)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "success",
		"message":    "If this email is registered, a password reset link has been sent.",
		"resetToken": token,
		"resetUrl":   resetURL,
	})
}

// sendResendResetEmail sends a password reset email via Resend template or HTML fallback.
func sendResendResetEmail(toEmail, fullName, username, resetURL string) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		log.Println("auth: notice - RESEND_API_KEY env var not set. Skipping live Resend email dispatch.")
		return
	}

	toEmail = strings.TrimSpace(strings.ToLower(toEmail))
	if toEmail == "" {
		return
	}

	fullName = strings.TrimSpace(fullName)
	leadName := fullName
	if parts := strings.Fields(fullName); len(parts) > 0 {
		leadName = parts[0]
	}
	if leadName == "" {
		leadName = username
	}
	if leadName == "" {
		leadName = "there"
	}
	if fullName == "" {
		fullName = leadName
	}

	subject := "Reset Your Enlazer Password"
	preheader := fmt.Sprintf("Hi %s, use this link to reset your Enlazer password.", leadName)

	templateVars := map[string]interface{}{
		"subject":        subject,
		"preheader":      preheader,
		"FULL_NAME":      fullName,
		"full_name":      fullName,
		"lead_Name":      leadName,
		"lead_name":      leadName,
		"first_name":     leadName,
		"FIRST_NAME":     leadName,
		"name":           fullName,
		"NAME":           fullName,
		"email":          toEmail,
		"EMAIL":          toEmail,
		"HANDLE":         username,
		"handle":         username,
		"username":       username,
		"USERNAME":       username,
		"RESET_URL":      resetURL,
		"reset_url":      resetURL,
		"resetUrl":       resetURL,
		"RESET_LINK":     resetURL,
		"reset_link":     resetURL,
		"resetLink":      resetURL,
		"year":           fmt.Sprintf("%d", time.Now().Year()),
	}

	fromEmail := getResendFromEmail()
	templateID := strings.TrimSpace(os.Getenv("RESEND_FORGOT_PASSWORD_TEMPLATE_ID"))
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("RESEND_PASSWORD_RESET_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("RESEND_RESET_PASSWORD_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("FORGOT_PASSWORD_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("PASSWORD_RESET_TEMPLATE_ID"))
	}

	var payload map[string]interface{}
	if templateID != "" {
		payload = map[string]interface{}{
			"from":    fromEmail,
			"to":      []string{toEmail},
			"subject": subject,
			"template": map[string]interface{}{
				"id":        templateID,
				"variables": templateVars,
			},
		}
	} else {
		log.Println("auth: notice - RESEND_FORGOT_PASSWORD_TEMPLATE_ID is not configured in .env. Falling back to local backend HTML email.")
		stringVars := make(map[string]string)
		for k, v := range templateVars {
			stringVars[k] = fmt.Sprintf("%v", v)
		}
		htmlContent := renderPasswordResetTemplate(stringVars)
		payload = map[string]interface{}{
			"from":    fromEmail,
			"to":      []string{toEmail},
			"subject": subject,
			"html":    htmlContent,
		}
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

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		log.Printf("auth: ERROR - Resend password reset email rejected for %s (Status: %d, Response: %s)", toEmail, resp.StatusCode, string(respBody))
		return
	}

	if templateID != "" {
		log.Printf("auth: Resend password reset email sent using template %s to %s (Status: %d)", templateID, toEmail, resp.StatusCode)
	} else {
		log.Printf("auth: Resend password reset email sent successfully via HTML fallback to %s (Status: %d)", toEmail, resp.StatusCode)
	}
}

// renderPasswordResetTemplate fills in all {{{variable}}} placeholders in fallback HTML.
func renderPasswordResetTemplate(vars map[string]string) string {
	tpl := passwordResetTemplateHTML
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{{{"+k+"}}}", v)
	}
	tpl = strings.ReplaceAll(tpl, "{{{year}}}", fmt.Sprintf("%d", time.Now().Year()))
	return tpl
}

const passwordResetTemplateHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{{subject}}}</title>
</head>
<body style="margin: 0; padding: 0; background-color: #F1F5F9; font-family: 'Plus Jakarta Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; -webkit-font-smoothing: antialiased; color: #1E293B;">
  <div style="display: none; max-height: 0px; overflow: hidden; mso-hide: all;">
    {{{preheader}}}
  </div>
  <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="background-color: #F1F5F9; padding: 40px 10px;">
    <tr>
      <td align="center">
        <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="max-width: 600px; background-color: #ffffff; border-radius: 20px; overflow: hidden; border: 1px solid #E2E8F0; box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.05);">
          <tr>
            <td style="background-color: #0F172A; padding: 36px 40px; text-align: center; background-image: linear-gradient(135deg, #0F172A 0%, #1E293B 100%);">
              <div style="font-size: 32px; font-weight: 900; color: #ffffff; letter-spacing: -0.5px; margin-bottom: 6px;">
                enlazer<span style="color: #00BCFF;">.</span>
              </div>
              <div style="color: #94A3B8; font-size: 13px; font-weight: 500; letter-spacing: 0.5px; text-transform: uppercase;">
                Account Security & Recovery
              </div>
            </td>
          </tr>
          <tr>
            <td style="padding: 40px 36px;">
              <h1 style="color: #0F172A; font-size: 24px; font-weight: 800; margin-top: 0; margin-bottom: 12px; line-height: 1.3;">
                Reset Your Password
              </h1>
              <p style="color: #475569; font-size: 15px; line-height: 1.6; margin-top: 0; margin-bottom: 24px;">
                Hi {{{lead_Name}}}, we received a request to reset the password for your Enlazer account (<strong>{{{email}}}</strong>). Click the button below to choose a new password.
              </p>
              <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="margin-bottom: 28px;">
                <tr>
                  <td align="center">
                    <a href="{{{RESET_URL}}}" style="display: inline-block; background-color: #00BCFF; color: #ffffff; font-size: 15px; font-weight: 700; text-decoration: none; padding: 14px 36px; border-radius: 12px; box-shadow: 0 4px 14px rgba(0, 188, 255, 0.35);">
                      Reset Password →
                    </a>
                  </td>
                </tr>
              </table>
              <div style="background-color: #F8FAFC; border: 1px solid #E2E8F0; border-radius: 12px; padding: 16px 20px; margin-bottom: 24px;">
                <p style="font-size: 13px; color: #64748B; margin: 0 0 8px 0;">Or copy and paste this link into your browser:</p>
                <a href="{{{RESET_URL}}}" style="color: #00BCFF; font-size: 13px; text-decoration: none; word-break: break-all; font-weight: 600;">{{{RESET_URL}}}</a>
              </div>
              <p style="font-size: 13px; color: #94A3B8; line-height: 1.5; margin: 0;">
                ⚠️ This reset link expires in <strong>1 hour</strong>. If you did not request a password reset, you can safely ignore this email — your account remains secure.
              </p>
            </td>
          </tr>
          <tr>
            <td style="background-color: #F8FAFC; border-top: 1px solid #E2E8F0; padding: 24px 40px; text-align: center;">
              <p style="font-size: 12px; color: #CBD5E1; margin: 0;">
                © {{{year}}} Enlazer - IaaS. All rights reserved.
              </p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`


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
