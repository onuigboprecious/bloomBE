package auth

import "net/http"

// handleMe returns the profile information of the currently authenticated user.
func handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := userFromContext(r.Context())
	if !ok {
		// Fallback to checking active session cookie directly
		var found bool
		user, found = currentUser(r)
		if !found {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}

	_ = writeJSON(w, http.StatusOK, user.ToPublic())
}
