package store

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

// getResendFromEmail resolves sender email address from env or defaults to fallback.
func getResendFromEmail() string {
	if from := os.Getenv("RESEND_FROM"); from != "" {
		return from
	}
	if from := os.Getenv("FROM_EMAIL"); from != "" {
		return from
	}
	return "Enlazer <onboarding@enlazer.cloud>"
}

// formatPrice formats an integer price into formatted Naira string (e.g., 19000 -> ₦19,000.00).
func formatPrice(amount int) string {
	if amount <= 0 {
		return "₦19,000.00"
	}
	// If amount is passed in kobo (e.g. 1900000), adjust if necessary, or treat as whole Naira
	s := fmt.Sprintf("%d", amount)
	n := len(s)
	if n <= 3 {
		return "₦" + s + ".00"
	}
	var res []string
	rem := n % 3
	if rem > 0 {
		res = append(res, s[:rem])
	}
	for i := rem; i < n; i += 3 {
		res = append(res, s[i:i+3])
	}
	return "₦" + strings.Join(res, ",") + ".00"
}

// SendOrderShippedEmail sends a shipment notification email to the customer via Resend template.
// Optional optExtras: [0] = username/handle, [1] = avatarURL
func SendOrderShippedEmail(toEmail, shippingName, orderID, finishName string, quantity, amount int, deliveryAddress, city, trackingNumber string, optExtras ...string) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		log.Println("store: notice - RESEND_API_KEY env var not set. Skipping order shipped email.")
		return
	}

	toEmail = strings.TrimSpace(strings.ToLower(toEmail))
	if toEmail == "" {
		log.Println("store: notice - recipient email is empty. Skipping order shipped email.")
		return
	}

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://enlazer.cloud"
	}
	frontendOrigin = strings.TrimSuffix(frontendOrigin, "/")

	shippingName = strings.TrimSpace(shippingName)
	leadName := shippingName
	if parts := strings.Fields(shippingName); len(parts) > 0 {
		leadName = parts[0]
	}
	if leadName == "" {
		leadName = "there"
	}
	if shippingName == "" {
		shippingName = "Customer"
	}

	var username, avatarURL string
	if len(optExtras) > 0 {
		username = strings.TrimSpace(optExtras[0])
	}
	if len(optExtras) > 1 {
		avatarURL = strings.TrimSpace(optExtras[1])
	}

	username = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(username)), "@")
	if username == "" {
		parts := strings.Split(toEmail, "@")
		if len(parts) > 0 {
			username = parts[0]
		}
	}
	if username == "" {
		username = leadName
	}
	if username == "" {
		username = "member"
	}

	if avatarURL == "" {
		avatarURL = "https://enlazer.cloud/default-avatar.png"
	}

	if finishName == "" {
		finishName = "Stealth Matte Black"
	}
	if quantity <= 0 {
		quantity = 1
	}
	if deliveryAddress == "" {
		deliveryAddress = "Standard Delivery Address"
	}
	if city == "" {
		city = "Nigeria"
	}

	fullAddress := deliveryAddress
	if !strings.Contains(strings.ToLower(deliveryAddress), strings.ToLower(city)) {
		fullAddress = fmt.Sprintf("%s, %s", deliveryAddress, city)
	}

	itemName := fmt.Sprintf("Enlazer Smart Card (%s)", finishName)
	formattedAmount := formatPrice(amount)
	profileURL := fmt.Sprintf("%s/@%s", frontendOrigin, username)
	dashboardURL := fmt.Sprintf("%s/dashboard", frontendOrigin)
	supportURL := fmt.Sprintf("%s/contact", frontendOrigin)

	subject := fmt.Sprintf("Your Enlazer NFC Card Has Shipped! (Order #%s)", orderID)
	preheader := fmt.Sprintf("Great news %s! Your %s has been dispatched and is on its way to %s.", leadName, finishName, city)

	templateVars := map[string]interface{}{
		"subject":          subject,
		"preheader":        preheader,
		"FULL_NAME":        shippingName,
		"full_name":        shippingName,
		"lead_Name":        leadName,
		"lead_name":        leadName,
		"first_name":       leadName,
		"FIRST_NAME":       leadName,
		"CUSTOMER_NAME":    shippingName,
		"customer_name":    shippingName,
		"name":             shippingName,
		"NAME":             shippingName,
		"HANDLE":           username,
		"handle":           username,
		"username":         username,
		"USERNAME":         username,
		"USER_NAME":        username,
		"user_name":        username,
		"AVATAR_URL":       avatarURL,
		"avatar_url":       avatarURL,
		"avatar":           avatarURL,
		"AVATAR":           avatarURL,
		"PROFILE_URL":      profileURL,
		"profile_url":      profileURL,
		"email":            toEmail,
		"EMAIL":            toEmail,
		"ORDER_ID":         orderID,
		"order_id":         orderID,
		"orderId":          orderID,
		"order_number":     orderID,
		"ORDER_NUMBER":     orderID,
		"ITEM_NAME":        itemName,
		"item_name":        itemName,
		"FINISH_NAME":      finishName,
		"finish_name":      finishName,
		"finishName":       finishName,
		"QUANTITY":         fmt.Sprintf("%d", quantity),
		"quantity":         quantity,
		"AMOUNT":           formattedAmount,
		"amount":           formattedAmount,
		"DELIVERY_ADDRESS": deliveryAddress,
		"delivery_address": deliveryAddress,
		"address":          deliveryAddress,
		"ADDRESS":          deliveryAddress,
		"CITY":             city,
		"city":             city,
		"DESTINATION":      fullAddress,
		"destination":      fullAddress,
		"TRACKING_NUMBER":  trackingNumber,
		"tracking_number":  trackingNumber,
		"trackingNumber":   trackingNumber,
		"CARD_URL":         dashboardURL,
		"card_url":         dashboardURL,
		"CARD_UID":         orderID,
		"card_uid":         orderID,
		"cardUid":          orderID,
		"CARD_ID":          orderID,
		"card_id":          orderID,
		"cardId":           orderID,
		"STATUS":           "Shipped",
		"status":           "Shipped",
		"DASHBOARD_URL":    dashboardURL,
		"dashboard_url":    dashboardURL,
		"SUPPORT_URL":      supportURL,
		"support_url":      supportURL,
		"CONTACT_URL":      supportURL,
		"contact_url":      supportURL,
		"year":             fmt.Sprintf("%d", time.Now().Year()),
		"YEAR":             fmt.Sprintf("%d", time.Now().Year()),
	}

	fromEmail := getResendFromEmail()
	templateID := strings.TrimSpace(os.Getenv("RESEND_ORDER_SHIPPED_TEMPLATE_ID"))
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("RESEND_SHIPPED_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("RESEND_CARD_SHIPPED_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("RESEND_SHIPPING_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("RESEND_SHIP_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("RESEND_ORDER_SHIPPING_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("ORDER_SHIPPED_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("SHIPPED_TEMPLATE_ID"))
	}
	if templateID == "" {
		templateID = strings.TrimSpace(os.Getenv("SHIP_TEMPLATE_ID"))
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
		log.Println("store: notice - RESEND_ORDER_SHIPPED_TEMPLATE_ID is not configured in .env. Falling back to local backend HTML email.")
		stringVars := make(map[string]string)
		for k, v := range templateVars {
			stringVars[k] = fmt.Sprintf("%v", v)
		}
		htmlContent := renderOrderShippedTemplate(stringVars)
		payload = map[string]interface{}{
			"from":    fromEmail,
			"to":      []string{toEmail},
			"subject": subject,
			"html":    htmlContent,
		}
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("store: failed to marshal order shipped email: %v", err)
		return
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Printf("store: failed to create order shipped email request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("store: failed to send order shipped email: %v", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		log.Printf("store: ERROR - Resend order shipped email rejected for %s (Status: %d, Response: %s)", toEmail, resp.StatusCode, string(respBody))
		return
	}

	if templateID != "" {
		log.Printf("store: Resend order shipped email sent using template %s to %s (Status: %d)", templateID, toEmail, resp.StatusCode)
	} else {
		log.Printf("store: Resend order shipped email sent successfully via HTML fallback to %s (Status: %d)", toEmail, resp.StatusCode)
	}
}

// renderOrderShippedTemplate fills in all {{{variable}}} placeholders in fallback HTML.
func renderOrderShippedTemplate(vars map[string]string) string {
	tpl := orderShippedTemplateHTML
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{{{"+k+"}}}", v)
	}
	tpl = strings.ReplaceAll(tpl, "{{{year}}}", fmt.Sprintf("%d", time.Now().Year()))
	return tpl
}

const orderShippedTemplateHTML = `<!DOCTYPE html>
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
                Order Dispatch Notification
              </div>
            </td>
          </tr>

          <!-- Main Content -->
          <tr>
            <td style="padding: 40px 36px;">
              <div style="display: inline-block; background-color: #E0F2FE; color: #0284C7; font-size: 12px; font-weight: 800; padding: 4px 14px; border-radius: 20px; margin-bottom: 16px; text-transform: uppercase; letter-spacing: 0.5px;">
                🚚 Order Shipped & In Transit
              </div>

              <h1 style="color: #0F172A; font-size: 24px; font-weight: 800; margin-top: 0; margin-bottom: 12px; line-height: 1.3;">
                Your Enlazer NFC Card is On Its Way, {{{lead_Name}}}!
              </h1>
              <p style="color: #475569; font-size: 15px; line-height: 1.6; margin-top: 0; margin-bottom: 24px;">
                Great news! We have packaged your physical smart card and dispatched it with our delivery team. It will arrive at your address shortly.
              </p>

              <!-- Order Summary Box -->
              <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="background-color: #0F172A; border-radius: 14px; margin-bottom: 28px; overflow: hidden;">
                <tr>
                  <td style="padding: 24px 28px; color: #ffffff;">
                    <table width="100%" border="0" cellspacing="0" cellpadding="0">
                      <tr>
                        <td valign="middle">
                          <div style="font-size: 11px; text-transform: uppercase; letter-spacing: 1px; color: #00BCFF; font-weight: 700; margin-bottom: 4px;">Order Number</div>
                          <div style="font-size: 18px; font-weight: 800; color: #ffffff; font-family: monospace;">#{{{ORDER_ID}}}</div>
                        </td>
                        <td align="right" valign="middle">
                          <div style="font-size: 12px; background-color: rgba(255, 255, 255, 0.12); padding: 4px 10px; border-radius: 6px; color: #E2E8F0; font-weight: 600;">
                            {{{QUANTITY}}} Unit
                          </div>
                        </td>
                      </tr>
                    </table>

                    <div style="height: 1px; background-color: rgba(255, 255, 255, 0.1); margin: 18px 0;"></div>

                    <table width="100%" border="0" cellspacing="0" cellpadding="0">
                      <tr>
                        <td style="padding-bottom: 10px;">
                          <div style="font-size: 12px; color: #94A3B8; margin-bottom: 2px;">Item:</div>
                          <div style="font-size: 14px; color: #ffffff; font-weight: 700;">{{{ITEM_NAME}}}</div>
                        </td>
                      </tr>
                      <tr>
                        <td style="padding-bottom: 10px;">
                          <div style="font-size: 12px; color: #94A3B8; margin-bottom: 2px;">Delivery Destination:</div>
                          <div style="font-size: 14px; color: #ffffff; font-weight: 600; line-height: 1.4;">{{{DESTINATION}}}</div>
                        </td>
                      </tr>
                      <tr>
                        <td>
                          <div style="font-size: 12px; color: #94A3B8; margin-bottom: 2px;">Payment:</div>
                          <div style="font-size: 14px; color: #34D399; font-weight: 800;">{{{AMOUNT}}} (Paid)</div>
                        </td>
                      </tr>
                    </table>
                  </td>
                </tr>
              </table>

              <!-- Action Button -->
              <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="margin-bottom: 32px;">
                <tr>
                  <td align="center">
                    <a href="{{{DASHBOARD_URL}}}" style="display: inline-block; background-color: #00BCFF; color: #ffffff; font-size: 15px; font-weight: 700; text-decoration: none; padding: 14px 34px; border-radius: 12px; box-shadow: 0 4px 14px rgba(0, 188, 255, 0.35);">
                      Open Dashboard & Prepare Profile →
                    </a>
                  </td>
                </tr>
              </table>

              <hr style="border: none; border-top: 1px solid #E2E8F0; margin: 32px 0;" />

              <!-- What to Do When Package Arrives -->
              <div style="font-size: 16px; font-weight: 800; color: #0F172A; margin-bottom: 14px;">
                📦 What to do when your package arrives:
              </div>

              <table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0">
                <tr>
                  <td width="36" valign="top" style="padding-bottom: 14px;">
                    <div style="width: 24px; height: 24px; background-color: #F0FDF4; color: #16A34A; border-radius: 50%; text-align: center; font-weight: 800; font-size: 12px; line-height: 24px;">1</div>
                  </td>
                  <td valign="top" style="padding-bottom: 14px; padding-left: 8px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Unbox Your Enlazer Card</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.4;">Unpack your premium smart card and tap it on your phone.</div>
                  </td>
                </tr>
                <tr>
                  <td width="36" valign="top" style="padding-bottom: 14px;">
                    <div style="width: 24px; height: 24px; background-color: #F0FDF4; color: #16A34A; border-radius: 50%; text-align: center; font-weight: 800; font-size: 12px; line-height: 24px;">2</div>
                  </td>
                  <td valign="top" style="padding-bottom: 14px; padding-left: 8px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Instant 5-Second Linking</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.4;">Follow the on-screen prompt to bind your physical card to your Enlazer handle.</div>
                  </td>
                </tr>
                <tr>
                  <td width="36" valign="top">
                    <div style="width: 24px; height: 24px; background-color: #F0FDF4; color: #16A34A; border-radius: 50%; text-align: center; font-weight: 800; font-size: 12px; line-height: 24px;">3</div>
                  </td>
                  <td valign="top" style="padding-left: 8px;">
                    <div style="font-size: 14px; font-weight: 700; color: #0F172A; margin-bottom: 2px;">Start Sharing Instantly</div>
                    <div style="font-size: 13px; color: #64748B; line-height: 1.4;">Tap any smartphone to share your contact, bio links, and socials seamlessly!</div>
                  </td>
                </tr>
              </table>

            </td>
          </tr>

          <!-- Footer -->
          <tr>
            <td style="background-color: #F8FAFC; border-top: 1px solid #E2E8F0; padding: 28px 40px; text-align: center;">
              <p style="font-size: 12px; color: #94A3B8; margin: 0 0 10px 0; line-height: 1.5;">
                Questions about your delivery? Reply to this email or reach out to <a href="mailto:support@enlazer.com.ng" style="color: #00BCFF; text-decoration: none; font-weight: 600;">support@enlazer.com.ng</a>
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
