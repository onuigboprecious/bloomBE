package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
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
			Email:        "precious@bloomlabs.africa",
			Name:         "Precious Onuigbo",
			Username:     "precious",
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
		query := `INSERT INTO sessions (id, user_id, token, expires_at, created_at) VALUES ($1, $2, $3, $4, $5)`
		if _, err := activeService.db.Exec(query, session.ID, session.UserID, session.Token, session.ExpiresAt, session.CreatedAt); err != nil {
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

	// If DB is available, query DB for active session
	if activeService != nil && activeService.db != nil {
		query := `
		SELECT u.id, u.email, u.name, COALESCE(u.username, ''), COALESCE(u.password_hash, ''), COALESCE(u.google_id, ''), COALESCE(u.avatar_url, ''), u.is_pro, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > $2
		`
		var u User
		err := activeService.db.QueryRow(query, token, time.Now()).Scan(
			&u.ID, &u.Email, &u.Name, &u.Username, &u.PasswordHash, &u.GoogleID, &u.AvatarURL, &u.IsPro, &u.CreatedAt, &u.UpdatedAt,
		)
		if err == nil {
			return &u, true
		}
	}

	// Fallback to in-memory store
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
		query := `SELECT id, email, name, COALESCE(username, ''), COALESCE(password_hash, ''), COALESCE(google_id, ''), COALESCE(avatar_url, ''), is_pro, created_at, updated_at FROM users WHERE email = $1`
		var u User
		err := activeService.db.QueryRow(query, email).Scan(
			&u.ID, &u.Email, &u.Name, &u.Username, &u.PasswordHash, &u.GoogleID, &u.AvatarURL, &u.IsPro, &u.CreatedAt, &u.UpdatedAt,
		)
		if err == nil {
			return &u, true
		}
	}

	// Fallback to in-memory store
	mu.RLock()
	defer mu.RUnlock()
	userID, ok := byEmail[email]
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
	}

	// Fallback to in-memory store
	mu.RLock()
	defer mu.RUnlock()
	for _, u := range users {
		if u.GoogleID == googleID {
			return u, true
		}
	}
	return nil, false
}

func saveUser(u *User) error {
	// Persist to DB if connected
	if activeService != nil && activeService.db != nil {
		query := `
		INSERT INTO users (id, email, name, username, password_hash, google_id, avatar_url, is_pro, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			google_id = EXCLUDED.google_id,
			avatar_url = EXCLUDED.avatar_url,
			updated_at = EXCLUDED.updated_at
		`
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
		_, err := activeService.db.Exec(query, u.ID, u.Email, u.Name, usernameNull, passNull, googleNull, avatarNull, u.IsPro, u.CreatedAt, u.UpdatedAt)
		if err != nil {
			return errors.New("user with this email or username already exists")
		}
	}

	// Update in-memory cache
	mu.Lock()
	defer mu.Unlock()
	users[u.ID] = u
	byEmail[u.Email] = u.ID
	return nil
}
