package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type changePasswordPayload struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type deleteAccountPayload struct {
	Confirmation string `json:"confirmation"`
	Password     string `json:"password"`
}

type sessionItem struct {
	ID         string `json:"id"`
	Device     string `json:"device"`
	OS         string `json:"os"`
	Location   string `json:"location"`
	LastActive string `json:"lastActive"`
	IsCurrent  bool   `json:"isCurrent"`
}

// HandleChangePassword updates the authenticated user's password.
func (s *Service) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := currentUser(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req changePasswordPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.NewPassword = strings.TrimSpace(req.NewPassword)
	if len(req.NewPassword) < 8 {
		writeJSONError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}

	// If user already has a password hash, verify current password
	if user.PasswordHash != "" {
		if !checkPassword(user.PasswordHash, req.CurrentPassword) {
			writeJSONError(w, http.StatusUnauthorized, "incorrect current password")
			return
		}
	}

	hashed, err := hashPassword(req.NewPassword)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	// Update PostgreSQL if connected
	if s.db != nil {
		query := `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`
		if _, err := s.db.Exec(query, hashed, user.ID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to update password")
			return
		}
	}

	// Update memory
	mu.Lock()
	if u, exists := users[user.ID]; exists {
		u.PasswordHash = hashed
		u.UpdatedAt = time.Now()
	}
	mu.Unlock()

	_ = writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "password updated successfully",
	})
}

// HandleLogoutAll revokes all active sessions for the current user.
func (s *Service) HandleLogoutAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := currentUser(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if s.db != nil {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE user_id = $1`, user.ID)
	}

	mu.Lock()
	for token, sess := range sessions {
		if sess.UserID == user.ID {
			delete(sessions, token)
		}
	}
	mu.Unlock()

	clearSessionCookie(w)

	_ = writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "all sessions revoked successfully",
	})
}

// HandleGetSessions returns active sessions for the authenticated user.
func (s *Service) HandleGetSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := currentUser(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var currentToken string
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		currentToken = cookie.Value
	}
	if currentToken == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			currentToken = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	var list []sessionItem

	if s.db != nil {
		query := `
		SELECT id, token, last_activity_at 
		FROM sessions 
		WHERE user_id = $1 AND expires_at > NOW() 
		ORDER BY last_activity_at DESC
		`
		rows, err := s.db.Query(query, user.ID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, tok string
				var lastAct time.Time
				if err := rows.Scan(&id, &tok, &lastAct); err == nil {
					isCur := tok == currentToken
					timeAgo := "Active now"
					if diff := time.Since(lastAct); diff > time.Minute {
						timeAgo = diff.Round(time.Minute).String() + " ago"
					}
					list = append(list, sessionItem{
						ID:         id,
						Device:     "Desktop / Mobile",
						OS:         "Active Web Session",
						Location:   "Nigeria",
						LastActive: timeAgo,
						IsCurrent:  isCur,
					})
				}
			}
		}
	}

	if len(list) == 0 {
		list = append(list, sessionItem{
			ID:         "current-session",
			Device:     "Desktop",
			OS:         "Current Web Session",
			Location:   "Nigeria",
			LastActive: "Active now",
			IsCurrent:  true,
		})
	}

	_ = writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": list,
	})
}

// HandleRevokeSession deletes a specific session.
func (s *Service) HandleRevokeSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := currentUser(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Extract session ID from URL path (e.g. /api/auth/sessions/{id})
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionID string
	if len(pathParts) > 0 {
		sessionID = pathParts[len(pathParts)-1]
	}

	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session id required")
		return
	}

	if s.db != nil {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE (id = $1 OR token = $1) AND user_id = $2`, sessionID, user.ID)
	}

	mu.Lock()
	for token, sess := range sessions {
		if (sess.ID == sessionID || token == sessionID) && sess.UserID == user.ID {
			delete(sessions, token)
		}
	}
	mu.Unlock()

	_ = writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "session revoked",
	})
}

// HandleDeleteAccount permanently deletes the user's account and associated data.
func (s *Service) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := currentUser(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req deleteAccountPayload
	_ = json.NewDecoder(r.Body).Decode(&req)

	if strings.ToUpper(strings.TrimSpace(req.Confirmation)) != "DELETE" {
		writeJSONError(w, http.StatusBadRequest, "confirmation must be DELETE")
		return
	}

	// If password provided and user has password, verify it
	if user.PasswordHash != "" && req.Password != "" {
		if !checkPassword(user.PasswordHash, req.Password) {
			writeJSONError(w, http.StatusUnauthorized, "incorrect password")
			return
		}
	}

	if s.db != nil {
		// 1. Unbind and deactivate any claimed cards
		_, _ = s.db.Exec(`UPDATE cards SET claimed_by = NULL, is_active = FALSE WHERE claimed_by = $1`, user.ID)
		// 2. Delete custom links & socials
		_, _ = s.db.Exec(`DELETE FROM custom_links WHERE user_id = $1`, user.ID)
		_, _ = s.db.Exec(`DELETE FROM socials WHERE user_id = $1`, user.ID)
		// 3. Delete leads
		_, _ = s.db.Exec(`DELETE FROM leads WHERE user_id = $1`, user.ID)
		// 4. Delete profiles
		_, _ = s.db.Exec(`DELETE FROM profiles WHERE user_id = $1 OR username = $2`, user.ID, user.Username)
		// 5. Delete account settings
		_, _ = s.db.Exec(`DELETE FROM account_settings WHERE user_id = $1`, user.ID)
		// 6. Delete sessions
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE user_id = $1`, user.ID)
		// 7. Delete user
		_, _ = s.db.Exec(`DELETE FROM users WHERE id = $1`, user.ID)
	}

	mu.Lock()
	delete(users, user.ID)
	delete(byEmail, user.Email)
	for token, sess := range sessions {
		if sess.UserID == user.ID {
			delete(sessions, token)
		}
	}
	mu.Unlock()

	clearSessionCookie(w)

	_ = writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "account permanently deleted",
	})
}
