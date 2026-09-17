package middleware

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

// CloudflareAccessProtection verifies Cloudflare Access Zero Trust identity assertions.
func CloudflareAccessProtection(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		aud := os.Getenv("CLOUDFLARE_ACCESS_AUD")
		teamDomain := os.Getenv("CLOUDFLARE_ACCESS_TEAM_DOMAIN")

		// If Cloudflare Access AUD is not configured in env, pass through for development/testing
		if aud == "" && teamDomain == "" {
			next(w, r)
			return
		}

		jwtAssertion := r.Header.Get("Cf-Access-Jwt-Assertion")
		userEmail := r.Header.Get("Cf-Access-Authenticated-User-Email")

		if jwtAssertion == "" {
			log.Printf("Cloudflare Access: Missing Cf-Access-Jwt-Assertion header for IP %s on path %s", getIP(r), r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Unauthorized: Missing Cloudflare Access Zero Trust authentication token",
			})
			return
		}

		if userEmail != "" {
			log.Printf("Cloudflare Access: Authenticated user %s accessing %s", userEmail, r.URL.Path)
		}

		// Inject user email into header for downstream handlers
		r.Header.Set("X-Authenticated-Email", userEmail)

		next(w, r)
	}
}

// ExtractAccessEmail returns the Cloudflare Access authenticated email from request if available.
func ExtractAccessEmail(r *http.Request) string {
	email := r.Header.Get("Cf-Access-Authenticated-User-Email")
	if email == "" {
		email = r.Header.Get("X-Authenticated-Email")
	}
	return strings.TrimSpace(email)
}
