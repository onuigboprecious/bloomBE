package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName     = "auth_session"
	sessionDuration       = 24 * time.Hour
	maxInactivityDuration = 30 * time.Minute
)

var (
	mu         sync.RWMutex
	users      = make(map[string]*User)          // userID -> User
	byEmail    = make(map[string]string)         // email -> userID
	sessions   = make(map[string]*Session)       // token -> Session
	resetStore = make(map[string]*PasswordReset) // token -> PasswordReset
)

func init() {
	seedDemoUsers()
}

func seedDemoUsers() {
	demoUsers := []*User{
		{
			ID:           "usr_9921",
			Email:        "kelvin@enlazer.cloud",
			Name:         "Kelvin Uchechukwu",
			Username:     "kelvin",
			PasswordHash: "$2a$10$wN3d0Dq9yL0q/KzZ3U2/nO5g9z.Gv9Z6p.y3J.3X.7S7.6v5J5.", // demo bcrypt hash
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	mu.Lock()
	defer mu.Unlock()
	for _, u := range demoUsers {
		users[u.ID] = u
		byEmail[u.Email] = u.ID
	}
}

// generateSessionToken generates a cryptographically secure random session token string.
func generateSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// createSessionAndSetCookie creates a new active session and sets the auth cookie on the response.
func createSessionAndSetCookie(w http.ResponseWriter, userID string) (*Session, error) {
	token, err := generateSessionToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	session := &Session{
		ID:             token,
		UserID:         userID,
		Token:          token,
		ExpiresAt:      now.Add(sessionDuration),
		LastActivityAt: now,
		CreatedAt:      now,
	}

	// Persist session to PostgreSQL if DB is connected
	if activeService != nil && activeService.db != nil {
		query := `INSERT INTO sessions (id, user_id, token, expires_at, last_activity_at, created_at) VALUES ($1, $2, $3, $4, $5, $6)`
		if _, err := activeService.db.Exec(query, session.ID, session.UserID, session.Token, session.ExpiresAt, session.LastActivityAt, session.CreatedAt); err != nil {
			return nil, err
		}
	}

	// Always update in-memory cache
	mu.Lock()
	sessions[token] = session
	mu.Unlock()

	isSecure := true
	sameSite := http.SameSiteNoneMode

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: sameSite,
		Secure:   isSecure,
	})

	return session, nil
}

// currentUser returns the User associated with the active request session cookie or Authorization Bearer header.
func currentUser(r *http.Request) (*User, bool) {
	var token string
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		token = cookie.Value
	}

	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else if strings.HasPrefix(authHeader, "bearer ") {
			token = strings.TrimPrefix(authHeader, "bearer ")
		}
	}

	if token == "" {
		token = r.Header.Get("X-Session-Token")
	}

	if token == "" {
		return nil, false
	}
	token = strings.TrimSpace(token)

	// If DB is available, query DB for active session (enforcing maxInactivityDuration)
	if activeService != nil && activeService.db != nil {
		now := time.Now()
		cutoff := now.Add(-maxInactivityDuration)
		query := `
		SELECT u.id, u.email, u.name, COALESCE(u.username, ''), COALESCE(u.password_hash, ''), COALESCE(u.google_id, ''), COALESCE(u.avatar_url, ''), u.is_pro, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > $2 AND s.last_activity_at > $3
		`
		var u User
		err := activeService.db.QueryRow(query, token, now, cutoff).Scan(
			&u.ID, &u.Email, &u.Name, &u.Username, &u.PasswordHash, &u.GoogleID, &u.AvatarURL, &u.IsPro, &u.CreatedAt, &u.UpdatedAt,
		)
		if err == nil {
			go func(t string) {
				_, _ = activeService.db.Exec(`UPDATE sessions SET last_activity_at = NOW() WHERE token = $1`, t)
			}(token)
			return &u, true
		}
		go func(t string) {
			_, _ = activeService.db.Exec(`DELETE FROM sessions WHERE token = $1 AND (expires_at <= NOW() OR last_activity_at <= NOW() - INTERVAL '30 minutes')`, t)
		}(token)
		return nil, false
	}

	// Fallback to in-memory store only if DB is not connected
	now := time.Now()
	mu.Lock()
	session, exists := sessions[token]
	if !exists {
		mu.Unlock()
		return nil, false
	}

	if now.After(session.ExpiresAt) {
		delete(sessions, token)
		mu.Unlock()
		return nil, false
	}

	// Check sliding window inactivity
	if session.LastActivityAt.IsZero() {
		session.LastActivityAt = session.CreatedAt
	}
	if now.Sub(session.LastActivityAt) > maxInactivityDuration {
		delete(sessions, token)
		mu.Unlock()
		return nil, false
	}

	// Update last activity timestamp on valid request
	session.LastActivityAt = now
	user, userExists := users[session.UserID]
	mu.Unlock()

	if !userExists {
		return nil, false
	}

	return user, true
}


// clearSessionCookie removes the session cookie from the client browser.
func clearSessionCookie(w http.ResponseWriter) {
	isSecure := false
	sameSite := http.SameSiteLaxMode
	if activeService != nil && activeService.env == "production" {
		isSecure = true
		sameSite = http.SameSiteNoneMode
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: sameSite,
		Secure:   isSecure,
	})
}

func destroySession(r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return
	}

	token := cookie.Value

	if activeService != nil && activeService.db != nil {
		query := `DELETE FROM sessions WHERE token = $1`
		_, _ = activeService.db.Exec(query, token)
	}

	mu.Lock()
	delete(sessions, token)
	mu.Unlock()
}

func findUserByEmail(email string) (*User, bool) {
	// Query DB first if connected
	if activeService != nil && activeService.db != nil {
		query := `SELECT id, email, name, COALESCE(username, ''), COALESCE(password_hash, ''), COALESCE(google_id, ''), COALESCE(avatar_url, ''), is_pro, created_at, updated_at FROM users WHERE LOWER(email) = LOWER($1)`
		var u User
		err := activeService.db.QueryRow(query, strings.TrimSpace(email)).Scan(
			&u.ID, &u.Email, &u.Name, &u.Username, &u.PasswordHash, &u.GoogleID, &u.AvatarURL, &u.IsPro, &u.CreatedAt, &u.UpdatedAt,
		)
		if err == nil {
			return &u, true
		}
		return nil, false
	}

	// Fallback to in-memory store only if DB is not connected
	mu.RLock()
	defer mu.RUnlock()
	userID, ok := byEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return nil, false
	}
	u, ok := users[userID]
	return u, ok
}

func findUserByGoogleID(googleID string) (*User, bool) {
	if googleID == "" {
		return nil, false
	}
	// Query DB first if connected
	if activeService != nil && activeService.db != nil {
		query := `SELECT id, email, name, COALESCE(username, ''), COALESCE(password_hash, ''), COALESCE(google_id, ''), COALESCE(avatar_url, ''), is_pro, created_at, updated_at FROM users WHERE google_id = $1`
		var u User
		err := activeService.db.QueryRow(query, googleID).Scan(
			&u.ID, &u.Email, &u.Name, &u.Username, &u.PasswordHash, &u.GoogleID, &u.AvatarURL, &u.IsPro, &u.CreatedAt, &u.UpdatedAt,
		)
		if err == nil {
			return &u, true
		}
		return nil, false
	}

	// Fallback to in-memory store only if DB is not connected
	mu.RLock()
	defer mu.RUnlock()
	for _, u := range users {
		if u.GoogleID == googleID {
			return u, true
		}
	}
	return nil, false
}

func findUserByUsername(username string) (*User, bool) {
	if username == "" {
		return nil, false
	}
	if activeService != nil && activeService.db != nil {
		query := `SELECT id, email, name, COALESCE(username, ''), COALESCE(password_hash, ''), COALESCE(google_id, ''), COALESCE(avatar_url, ''), is_pro, created_at, updated_at FROM users WHERE LOWER(username) = LOWER($1)`
		var u User
		err := activeService.db.QueryRow(query, strings.TrimSpace(username)).Scan(
			&u.ID, &u.Email, &u.Name, &u.Username, &u.PasswordHash, &u.GoogleID, &u.AvatarURL, &u.IsPro, &u.CreatedAt, &u.UpdatedAt,
		)
		if err == nil {
			return &u, true
		}
		return nil, false
	}

	// Fallback to in-memory store only if DB is not connected
	mu.RLock()
	defer mu.RUnlock()
	for _, u := range users {
		if strings.EqualFold(u.Username, username) {
			return u, true
		}
	}
	return nil, false
}

func saveUser(u *User) error {
	// Persist to DB if connected
	if activeService != nil && activeService.db != nil {
		var usernameNull, passNull, googleNull, avatarNull sql.NullString
		if u.Username != "" {
			usernameNull = sql.NullString{String: u.Username, Valid: true}
		}
		if u.PasswordHash != "" {
			passNull = sql.NullString{String: u.PasswordHash, Valid: true}
		}
		if u.GoogleID != "" {
			googleNull = sql.NullString{String: u.GoogleID, Valid: true}
		}
		if u.AvatarURL != "" {
			avatarNull = sql.NullString{String: u.AvatarURL, Valid: true}
		}

		// Try updating existing user by ID or Email first
		updateQuery := `
		UPDATE users SET
			email = $2,
			name = $3,
			username = COALESCE($4, username),
			password_hash = COALESCE($5, password_hash),
			google_id = COALESCE($6, google_id),
			avatar_url = COALESCE($7, avatar_url),
			is_pro = $8,
			updated_at = $9
		WHERE id = $1 OR LOWER(email) = LOWER($2)
		`
		res, err := activeService.db.Exec(updateQuery, u.ID, u.Email, u.Name, usernameNull, passNull, googleNull, avatarNull, u.IsPro, u.UpdatedAt)
		if err == nil {
			if rows, _ := res.RowsAffected(); rows > 0 {
				var actualID string
				if errScan := activeService.db.QueryRow(`SELECT id FROM users WHERE LOWER(email) = LOWER($1)`, u.Email).Scan(&actualID); errScan == nil {
					u.ID = actualID
				}
				mu.Lock()
				users[u.ID] = u
				byEmail[strings.ToLower(u.Email)] = u.ID
				mu.Unlock()
				return nil
			}
		}

		// Otherwise insert new user
		insertQuery := `
		INSERT INTO users (id, email, name, username, password_hash, google_id, avatar_url, is_pro, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			google_id = COALESCE(EXCLUDED.google_id, users.google_id),
			avatar_url = COALESCE(EXCLUDED.avatar_url, users.avatar_url),
			updated_at = EXCLUDED.updated_at
		`
		_, err = activeService.db.Exec(insertQuery, u.ID, u.Email, u.Name, usernameNull, passNull, googleNull, avatarNull, u.IsPro, u.CreatedAt, u.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to insert user into database: %w", err)
		}
	}

	// Update in-memory cache
	mu.Lock()
	defer mu.Unlock()
	users[u.ID] = u
	byEmail[strings.ToLower(u.Email)] = u.ID
	return nil
}
