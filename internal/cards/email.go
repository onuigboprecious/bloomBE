package cards

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// getResendFromEmail resolves the Resend sender address from RESEND_FROM, FROM_EMAIL, or defaults to fallback.
func getResendFromEmail() string {
	if from := os.Getenv("RESEND_FROM"); from != "" {
		return from
	}
	if from := os.Getenv("FROM_EMAIL"); from != "" {
		return from
	}
	return "Enlazer <onboarding@enlazer.cloud>"
}

// SendCardActivationEmail sends a confirmation email when a user links/claims their physical NFC card via Resend template.
func SendCardActivationEmail(toEmail, fullName, username, avatarURL, cardUid, finishName, signedURL string) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		log.Println("cards: notice - RESEND_API_KEY env var not set. Skipping card activation email.")
		return
	}

	toEmail = strings.TrimSpace(strings.ToLower(toEmail))
	if toEmail == "" {
		log.Println("cards: notice - recipient email is empty. Skipping card activation email.")
		return
	}

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://enlazer.cloud"
	}
	frontendOrigin = strings.TrimSuffix(frontendOrigin, "/")

	fullName = strings.TrimSpace(fullName)
	username = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(username)), "@")
	if username == "" {
		parts := strings.Split(toEmail, "@")
		username = parts[0]
	}

	// Calculate lead name (first name)
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

	if avatarURL == "" {
		avatarURL = "https://www.enlazer.cloud/email/avatar-default.png"
	}
	if finishName == "" {
		finishName = "Stealth Matte Black"
	}

	cardUid = strings.TrimSpace(cardUid)
	if cardUid == "" {
		cardUid = "ENZ-CARD"
	}

	sig := SignCardUID(cardUid)
	if signedURL == "" {
		signedURL = fmt.Sprintf("%s/card/%s?sig=%s", frontendOrigin, cardUid, sig)
	}

	profileURL := fmt.Sprintf("%s/@%s", frontendOrigin, username)
	dashboardURL := fmt.Sprintf("%s/dashboard", frontendOrigin)

	subject := fmt.Sprintf("Your Enlazer NFC Card is Active! (%s)", cardUid)
	preheader := fmt.Sprintf("Your %s card (%s) is linked and ready to tap with any smartphone.", finishName, cardUid)

	// Comprehensive variables to match both uppercase and lowercase template tags in Resend
	templateVars := map[string]interface{}{
		"subject":        subject,
		"preheader":      preheader,
		"FULL_NAME":      fullName,
		"full_name":      fullName,
		"lead_Name":      leadName,
		"lead_name":      leadName,
		"first_name":     leadName,
		"FIRST_NAME":     leadName,
		"HANDLE":         username,
		"handle":         username,
		"username":       username,
		"USERNAME":       username,
		"AVATAR_URL":     avatarURL,
		"avatar_url":     avatarURL,
		"CARD_UID":       cardUid,
		"card_uid":       cardUid,
		"cardUid":        cardUid,
		"FINISH_NAME":    finishName,
		"finish_name":    finishName,
		"finishName":     finishName,
		"CARD_URL":       signedURL,
		"card_url":       signedURL,
		"SIGNED_URL":     signedURL,
		"signed_url":     signedURL,
		"PROFILE_URL":    profileURL,
		"profile_url":    profileURL,
		"DASHBOARD_URL":  dashboardURL,
		"dashboard_url":  dashboardURL,
		"year":           fmt.Sprintf("%d", time.Now().Year()),
	}

	fromEmail := getResendFromEmail()
	templateID := os.Getenv("RESEND_CARD_ACTIVATION_TEMPLATE_ID")
	if templateID == "" {
		templateID = os.Getenv("RESEND_CARD_ACTIVATED_TEMPLATE_ID")
	}
	if templateID == "" {
		templateID = os.Getenv("RESEND_CARD_TEMPLATE_ID")
	}

	var payload map[string]interface{}
	if templateID != "" {
		// Send using published Resend Dashboard Template
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
		// Fallback: render HTML template locally and send HTML body
		stringVars := make(map[string]string)
		for k, v := range templateVars {
			stringVars[k] = fmt.Sprintf("%v", v)
		}
		htmlContent := renderCardActivationTemplate(stringVars)
		payload = map[string]interface{}{
			"from":    fromEmail,
			"to":      []string{toEmail},
			"subject": subject,
			"html":    htmlContent,
		}
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("cards: failed to marshal card activation email: %v", err)
		return
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Printf("cards: failed to create card activation email request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("cards: failed to send card activation email: %v", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		log.Printf("cards: ERROR - Resend card activation email rejected for %s (Status: %d, Response: %s)", toEmail, resp.StatusCode, string(respBody))
		return
	}

	if templateID != "" {
		log.Printf("cards: Resend card activation email sent using template %s to %s (Status: %d)", templateID, toEmail, resp.StatusCode)
	} else {
		log.Printf("cards: Resend card activation email sent successfully via HTML fallback to %s (Status: %d)", toEmail, resp.StatusCode)
	}
}

// renderCardActivationTemplate fills in all {{{variable}}} placeholders in fallback HTML.
func renderCardActivationTemplate(vars map[string]string) string {
	tpl := cardActivationTemplateHTML
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{{{"+k+"}}}", v)
	}
	tpl = strings.ReplaceAll(tpl, "{{{year}}}", fmt.Sprintf("%d", time.Now().Year()))
	return tpl
}

const cardActivationTemplateHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{{subject}}}</title>
</head>
<body style="margin: 0; padding: 0; background-color: #F1F5F9; font-family: 'Plus Jakarta Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; -webkit-font-smoothing: antialiased; color: #1E293B;">
  
  <!-- Preheader Preview Text -->
  <div style="display: none; max-height: 0px; overflow: hidden; mso-hide: all;">
    {{{preheader}}}
  </div>

  <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="background-color: #F1F5F9; padding: 40px 10px;">
    <tr>
      <td align="center">
        <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="max-width: 600px; background-color: #ffffff; border-radius: 20px; overflow: hidden; border: 1px solid #E2E8F0; box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.05);">
          
          <!-- Header Banner -->
          <tr>
            <td style="background-color: #0F172A; padding: 36px 40px; text-align: center; background-image: linear-gradient(135deg, #0F172A 0%, #1E293B 100%);">
              <div style="font-size: 32px; font-weight: 900; color: #ffffff; letter-spacing: -0.5px; margin-bottom: 6px;">
                enlazer<span style="color: #00BCFF;">.</span>
              </div>
              <div style="color: #94A3B8; font-size: 13px; font-weight: 500; letter-spacing: 0.5px; text-transform: uppercase;">
                Card Activation Confirmed
              </div>
            </td>
          </tr>

          <!-- Main Content -->
          <tr>
            <td style="padding: 40px 36px;">
              <div style="display: inline-block; background-color: #DCFCE7; color: #15803D; font-size: 12px; font-weight: 800; padding: 4px 12px; border-radius: 20px; margin-bottom: 16px; text-transform: uppercase; letter-spacing: 0.5px;">
                ✓ Card Linked & Activated
              </div>

              <h1 style="color: #0F172A; font-size: 24px; font-weight: 800; margin-top: 0; margin-bottom: 12px; line-height: 1.3;">
                Your Enlazer NFC Card is Ready, {{{lead_Name}}}!
              </h1>
              <p style="color: #475569; font-size: 15px; line-height: 1.6; margin-top: 0; margin-bottom: 24px;">
                Your physical smart card has been securely connected to your digital profile. Anyone who taps your card with their smartphone will instantly view your live profile and save your contact.
              </p>

              <!-- Hardware Card Spec Box -->
              <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="background-color: #0F172A; border-radius: 14px; margin-bottom: 28px; overflow: hidden;">
                <tr>
                  <td style="padding: 24px 28px; color: #ffffff;">
                    <table width="100%" border="0" cellspacing="0" cellpadding="0">
                      <tr>
                        <td valign="middle">
                          <div style="font-size: 11px; text-transform: uppercase; letter-spacing: 1px; color: #00BCFF; font-weight: 700; margin-bottom: 4px;">Hardware Identifier</div>
                          <div style="font-size: 18px; font-weight: 800; color: #ffffff; font-family: monospace; letter-spacing: 1px;">{{{CARD_UID}}}</div>
                        </td>
                        <td align="right" valign="middle">
                          <div style="font-size: 12px; background-color: rgba(255, 255, 255, 0.12); padding: 4px 10px; border-radius: 6px; color: #E2E8F0; font-weight: 600;">
                            {{{FINISH_NAME}}}
                          </div>
                        </td>
                      </tr>
                    </table>

                    <div style="height: 1px; background-color: rgba(255, 255, 255, 0.1); margin: 18px 0;"></div>

                    <table width="100%" border="0" cellspacing="0" cellpadding="0">
                      <tr>
                        <td>
                          <div style="font-size: 12px; color: #94A3B8; margin-bottom: 2px;">Linked Profile Handle:</div>
                          <div style="font-size: 14px; color: #ffffff; font-weight: 700;">@{{{HANDLE}}}</div>
                        </td>
                      </tr>
                    </table>
                  </td>
                </tr>
              </table>

              <!-- Action Buttons -->
              <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="margin-bottom: 32px;">
                <tr>
                  <td align="center">
                    <a href="{{{CARD_URL}}}" style="display: inline-block; background-color: #00BCFF; color: #ffffff; font-size: 15px; font-weight: 700; text-decoration: none; padding: 14px 32px; border-radius: 12px; box-shadow: 0 4px 14px rgba(0, 188, 255, 0.35); margin-right: 10px; margin-bottom: 8px;">
                      Test Your Card Tap →
                    </a>
                    <a href="{{{DASHBOARD_URL}}}" style="display: inline-block; background-color: #F1F5F9; color: #0F172A; font-size: 15px; font-weight: 700; text-decoration: none; padding: 14px 28px; border-radius: 12px; border: 1px solid #CBD5E1; margin-bottom: 8px;">
                      Manage in Dashboard
                    </a>
                  </td>
                </tr>
              </table>

              <hr style="border: none; border-top: 1px solid #E2E8F0; margin: 32px 0;" />

              <!-- Quick Tapping Guide -->
              <div style="font-size: 16px; font-weight: 800; color: #0F172A; margin-bottom: 14px;">
                📱 How to Tap Your Card in the Wild
              </div>

              <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0">
                <tr>
                  <td width="36" valign="top" style="padding-bottom: 16px;">
                    <div style="width: 26px; height: 26px; background-color: #E0F2FE; color: #0284C7; border-radius: 50%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">1</div>
                  </td>
                  <td valign="top" style="padding-bottom: 16px; padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">iPhone (XR and newer)</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">Tap the top edge of your Enlazer card against the <strong>top back</strong> of their iPhone. A banner pops up instantly.</div>
                  </td>
                </tr>
                <tr>
                  <td width="36" valign="top" style="padding-bottom: 16px;">
                    <div style="width: 26px; height: 26px; background-color: #E0F2FE; color: #0284C7; border-radius: 50%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">2</div>
                  </td>
                  <td valign="top" style="padding-bottom: 16px; padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Android Phones</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">Tap against the <strong>center back</strong> of the phone (ensure NFC is toggled on in quick settings).</div>
                  </td>
                </tr>
                <tr>
                  <td width="36" valign="top">
                    <div style="width: 26px; height: 26px; background-color: #E0F2FE; color: #0284C7; border-radius: 50%; text-align: center; font-weight: 800; font-size: 13px; line-height: 26px;">3</div>
                  </td>
                  <td valign="top" style="padding-left: 10px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Zero Apps Needed</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.5;">Your contacts do not need an app or Enlazer account to view your card, download your contact vCard, or exchange details.</div>
                  </td>
                </tr>
              </table>

            </td>
          </tr>

          <!-- Footer -->
          <tr>
            <td style="background-color: #F8FAFC; border-top: 1px solid #E2E8F0; padding: 28px 40px; text-align: center;">
              <p style="font-size: 12px; color: #94A3B8; margin: 0 0 10px 0; line-height: 1.5;">
                Need help or have questions about your card? Reply to this email or contact <a href="mailto:support@enlazer.com.ng" style="color: #00BCFF; text-decoration: none; font-weight: 600;">support@enlazer.com.ng</a>
              </p>
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
