package auth

import "net/http"

// handleLogout terminates the current user session and clears authentication cookies.
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	destroySession(r)
	clearSessionCookie(w)

	_ = writeJSON(w, http.StatusOK, map[string]string{
		"message": "Logged out successfully",
	})
}
