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

	session, err := createSessionAndSetCookie(w, newUser.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Asynchronously dispatch Welcome Email via Resend
	go sendResendWelcomeEmail(newUser.Email, newUser.Name, newUser.Username)

	pub := newUser.ToPublic()
	_ = writeJSON(w, http.StatusCreated, map[string]interface{}{
		"token":     session.Token,
		"id":        pub.ID,
		"name":      pub.Name,
		"email":     pub.Email,
		"username":  pub.Username,
		"createdAt": pub.CreatedAt,
		"user":      pub,
	})
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
	frontendOrigin = strings.TrimSuffix(frontendOrigin, "/")
	profileURL := fmt.Sprintf("%s/@%s", frontendOrigin, username)
	dashboardURL := fmt.Sprintf("%s/dashboard", frontendOrigin)

	fromEmail := os.Getenv("FROM_EMAIL")
	if fromEmail == "" {
		fromEmail = "Bloom <onboarding@resend.dev>"
	}

	subject := fmt.Sprintf("Welcome to Bloom, %s! 🎉 Your Digital Card is Ready", userName)
	htmlContent := buildWelcomeEmailHTML(userName, username, profileURL, dashboardURL)

	payload := map[string]interface{}{
		"from":    fromEmail,
		"to":      []string{toEmail},
		"subject": subject,
		"html":    htmlContent,
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

// buildWelcomeEmailHTML constructs a responsive, modern HTML welcome email body with card linking & tapping instructions.
func buildWelcomeEmailHTML(userName, username, profileURL, dashboardURL string) string {
	currentYear := time.Now().Year()

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Welcome to Bloom</title>
</head>
<body style="margin: 0; padding: 0; background-color: #F1F5F9; font-family: 'Plus Jakarta Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; -webkit-font-smoothing: antialiased; color: #1E293B;">
  <table role="presentation" width="100%%" border="0" cellspacing="0" cellpadding="0" style="background-color: #F1F5F9; padding: 40px 10px;">
    <tr>
      <td align="center">
        <table role="presentation" width="100%%" border="0" cellspacing="0" cellpadding="0" style="max-width: 600px; background-color: #ffffff; border-radius: 20px; overflow: hidden; border: 1px solid #E2E8F0; box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.05);">
          
          <!-- Header Banner -->
          <tr>
            <td style="background-color: #0F172A; padding: 36px 40px; text-align: center; background-image: linear-gradient(135deg, #0F172A 0%%, #1E293B 100%%);">
              <div style="font-size: 32px; font-weight: 900; color: #ffffff; letter-spacing: -0.5px; margin-bottom: 6px;">
                bloom<span style="color: #00BCFF;">.</span>
              </div>
              <div style="color: #94A3B8; font-size: 13px; font-weight: 500; letter-spacing: 0.5px; text-transform: uppercase;">
                Digital Business Cards & Instant NFC Sharing
              </div>
            </td>
          </tr>

          <!-- Main Content -->
          <tr>
            <td style="padding: 40px 36px;">
              <h1 style="color: #0F172A; font-size: 24px; font-weight: 800; margin-top: 0; margin-bottom: 12px; line-height: 1.3;">
                Welcome to Bloom, %s! 👋
              </h1>
              <p style="color: #475569; font-size: 15px; line-height: 1.6; margin-top: 0; margin-bottom: 28px;">
                Your account is live and your digital card profile is ready. Here is how to link your physical card and let people view your profile with a simple tap!
              </p>

              <!-- Profile Link Card Box -->
              <table role="presentation" width="100%%" border="0" cellspacing="0" cellpadding="0" style="background-color: #F8FAFC; border: 1px solid #E2E8F0; border-radius: 14px; margin-bottom: 28px;">
                <tr>
                  <td style="padding: 24px;">
                    <div style="font-size: 11px; font-weight: 700; color: #0284C7; text-transform: uppercase; letter-spacing: 0.8px; margin-bottom: 6px;">
                      Your Unique Profile Link & Handle
                    </div>
                    <div style="font-size: 18px; font-weight: 800; color: #0F172A; margin-bottom: 8px;">
                      @%s
                    </div>
                    <div style="font-size: 13px; color: #64748B; word-break: break-all;">
                      <a href="%s" style="color: #00BCFF; text-decoration: none; font-weight: 600;">%s</a>
                    </div>
                  </td>
                </tr>
              </table>

              <!-- Action Button -->
              <table role="presentation" width="100%%" border="0" cellspacing="0" cellpadding="0" style="margin-bottom: 32px;">
                <tr>
                  <td align="center">
                    <a href="%s" style="display: inline-block; background-color: #00BCFF; color: #ffffff; font-size: 15px; font-weight: 700; text-decoration: none; padding: 15px 36px; border-radius: 12px; box-shadow: 0 4px 14px rgba(0, 188, 255, 0.35);">
                      Open Dashboard & Setup Profile →
                    </a>
                  </td>
                </tr>
              </table>

              <hr style="border: none; border-top: 1px solid #E2E8F0; margin: 32px 0;" />

              <!-- SECTION 1: How Card Linking Works -->
              <div style="font-size: 17px; font-weight: 800; color: #0F172A; margin-bottom: 14px;">
                💳 Step 1: How to Link Your Physical Bloom Card
              </div>

              <table role="presentation" width="100%%" border="0" cellspacing="0" cellpadding="0" style="margin-bottom: 24px;">
                <tr>
                  <td width="36" valign="top" style="padding-bottom: 16px;">
                    <div style="width: 26px; height: 26px; background-color: #E0F2FE; color: #0284C7; border-radius: 50%%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">1</div>
                  </td>
                  <td valign="top" style="padding-bottom: 16px; padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Log into your Dashboard</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">Click <strong>Activate New Card</strong> at the top right of your screen.</div>
                  </td>
                </tr>
                <tr>
                  <td width="36" valign="top" style="padding-bottom: 16px;">
                    <div style="width: 26px; height: 26px; background-color: #E0F2FE; color: #0284C7; border-radius: 50%%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">2</div>
                  </td>
                  <td valign="top" style="padding-bottom: 16px; padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Tap Card or Enter Code</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">Tap your physical Bloom NFC card on your phone, or manually enter your Card UID (e.g. <code>BLM-9921-NFC</code>).</div>
                  </td>
                </tr>
                <tr>
                  <td width="36" valign="top">
                    <div style="width: 26px; height: 26px; background-color: #E0F2FE; color: #0284C7; border-radius: 50%%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">3</div>
                  </td>
                  <td valign="top" style="padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Instant Card Binding</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">Your physical card is now securely encrypted and bound to your profile!</div>
                  </td>
                </tr>
              </table>

              <!-- SECTION 2: How Card Tapping Works -->
              <div style="font-size: 17px; font-weight: 800; color: #0F172A; margin-bottom: 14px; margin-top: 28px;">
                📱 Step 2: How People See Your Profile when Tapping
              </div>

              <table role="presentation" width="100%%" border="0" cellspacing="0" cellpadding="0">
                <tr>
                  <td width="36" valign="top" style="padding-bottom: 16px;">
                    <div style="width: 26px; height: 26px; background-color: #F0FDF4; color: #16A34A; border-radius: 50%%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">1</div>
                  </td>
                  <td valign="top" style="padding-bottom: 16px; padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Tap Card Against Phone</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">Hold your physical Bloom Card against the top back of any iPhone or center back of an Android phone.</div>
                  </td>
                </tr>
                <tr>
                  <td width="36" valign="top" style="padding-bottom: 16px;">
                    <div style="width: 26px; height: 26px; background-color: #F0FDF4; color: #16A34A; border-radius: 50%%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">2</div>
                  </td>
                  <td valign="top" style="padding-bottom: 16px; padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Instant Notification Opens</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">A native pop-up notification opens on their smartphone automatically—<strong>no app installation required!</strong></div>
                  </td>
                </tr>
                <tr>
                  <td width="36" valign="top">
                    <div style="width: 26px; height: 26px; background-color: #F0FDF4; color: #16A34A; border-radius: 50%%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">3</div>
                  </td>
                  <td valign="top" style="padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Save Contact & Share Back</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">They can save your contact (.vcf) directly to their phone, view your socials, or send their details back to your dashboard.</div>
                  </td>
                </tr>
              </table>

            </td>
          </tr>

          <!-- Footer -->
          <tr>
            <td style="background-color: #F8FAFC; border-top: 1px solid #E2E8F0; padding: 28px 40px; text-align: center;">
              <p style="font-size: 12px; color: #94A3B8; margin: 0 0 12px 0; line-height: 1.5;">
                Need help getting started? Reply directly to this email or visit your <a href="%s" style="color: #00BCFF; text-decoration: none; font-weight: 600;">Dashboard</a>.
              </p>
              <p style="font-size: 12px; color: #CBD5E1; margin: 0;">
                © %d Enlazar Technologies Ltd. All rights reserved.
              </p>
            </td>
          </tr>

        </table>
      </td>
    </tr>
  </table>
</body>
</html>`, userName, username, profileURL, profileURL, profileURL, dashboardURL, currentYear)
}
