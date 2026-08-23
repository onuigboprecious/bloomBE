package auth

import (
	"context"
	"net/http"
)

type contextKey string

const userContextKey contextKey = "user"

// requireAuth is a middleware that enforces authentication via session cookies.
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

// userFromContext extracts the authenticated User struct from the request context.
func userFromContext(ctx context.Context) (*User, bool) {
	user, ok := ctx.Value(userContextKey).(*User)
	return user, ok
}

// CurrentUserFromContext returns the authenticated User from context or session cookie.
func CurrentUserFromContext(r *http.Request) (*User, bool) {
	if u, ok := userFromContext(r.Context()); ok {
		return u, true
	}
	return currentUser(r)
}

