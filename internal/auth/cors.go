package auth

import (
	"net/http"

	"github.com/onuigboprecious/infarbloom/backend/internal/middleware"
)

// corsMiddleware wraps an http.Handler to configure CORS headers and preflight handling.
func corsMiddleware(next http.Handler) http.Handler {
	return middleware.CORS("http://localhost:5173", next)
}

