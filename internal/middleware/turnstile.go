package middleware

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

type TurnstileResponse struct {
	Success     bool     `json:"success"`
	ErrorCodes  []string `json:"error-codes,omitempty"`
	ChallengeTS string   `json:"challenge_ts,omitempty"`
	Hostname    string   `json:"hostname,omitempty"`
}

// VerifyTurnstileToken checks a Cloudflare Turnstile token against siteverify API.
func VerifyTurnstileToken(token string, remoteIP string) bool {
	secretKey := os.Getenv("CLOUDFLARE_TURNSTILE_SECRET_KEY")
	if secretKey == "" {
		// If Turnstile secret key is not set in environment, allow in development/testing mode
		log.Println("Notice: CLOUDFLARE_TURNSTILE_SECRET_KEY is not set. Bypassing Turnstile verification.")
		return true
	}

	if token == "" {
		return false
	}

	form := url.Values{}
	form.Set("secret", secretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", form)
	if err != nil {
		log.Printf("Turnstile siteverify error: %v", err)
		return false
	}
	defer resp.Body.Close()

	var result TurnstileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Turnstile json decode error: %v", err)
		return false
	}

	return result.Success
}

// TurnstileProtection middleware enforces Cloudflare Turnstile verification.
func TurnstileProtection(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Secret key check - skip if not configured
		secretKey := os.Getenv("CLOUDFLARE_TURNSTILE_SECRET_KEY")
		if secretKey == "" {
			next(w, r)
			return
		}

		token := r.Header.Get("X-Turnstile-Token")
		if token == "" {
			token = r.Header.Get("cf-turnstile-response")
		}

		ip := getIP(r)
		if !VerifyTurnstileToken(token, ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Cloudflare Turnstile verification failed. Please refresh and try again.",
			})
			return
		}

		next(w, r)
	}
}
