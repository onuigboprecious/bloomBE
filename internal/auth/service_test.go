package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
)

func TestAuthService(t *testing.T) {
	authSvc := auth.New(nil, "development")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/signup", authSvc.HandleSignup)
	mux.HandleFunc("POST /api/login", authSvc.HandleLogin)
	mux.HandleFunc("POST /api/logout", authSvc.HandleLogout)
	mux.HandleFunc("GET /api/me", authSvc.HandleMe)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for unauthenticated /api/me, got %d", rec.Code)
	}
}
