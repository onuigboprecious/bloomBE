package auth

import "net/http"

// registerAuthRoutes registers all authentication endpoints on the provided ServeMux.
func registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/signup", handleSignup)
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/auth/logout", handleLogout)
	mux.HandleFunc("/api/auth/me", requireAuth(handleMe))
	mux.HandleFunc("/api/auth/google", handleGoogleAuth)
	mux.HandleFunc("/api/auth/google/login", handleGoogleOAuthLogin)
	mux.HandleFunc("/api/auth/google/callback", handleGoogleOAuthCallback)
}

// RegisterAuthRoutes is the exported wrapper to register authentication routes.
func RegisterAuthRoutes(mux *http.ServeMux) {
	registerAuthRoutes(mux)
}

// RegisterRoutes registers auth routes wrapped with CORS middleware on the given ServeMux.
func RegisterRoutes(mux *http.ServeMux) http.Handler {
	registerAuthRoutes(mux)
	return corsMiddleware(mux)
}
