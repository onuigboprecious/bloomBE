package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookieName = "auth_session"
	sessionDuration   = 24 * time.Hour
)

var (
	mu       sync.RWMutex
	users    = make(map[string]*User)    // userID -> User
	byEmail  = make(map[string]string)   // email -> userID
	sessions = make(map[string]*Session) // token -> Session
)

func init() {
	seedDemoUsers()
}

func seedDemoUsers() {
	demoUsers := []*User{
		{
			ID:           "user-demo-1",
			Email:        "test@example.com",
			Name:         "Test User",
			PasswordHash: "$2a$10$wN3d0Dq9yL0q/KzZ3U2/nO5g9z.Gv9Z6p.y3J.3X.7S7.6v5J5.", // demo bcrypt hash
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			ID:           "user-demo-2",
			Email:        "precious@bloomlabs.africa",
			Name:         "Precious Onuigbo",
			PasswordHash: "$2a$10$wN3d0Dq9yL0q/KzZ3U2/nO5g9z.Gv9Z6p.y3J.3X.7S7.6v5J5.",
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

	session := &Session{
		ID:        token,
		UserID:    userID,
		Token:     token,
		ExpiresAt: time.Now().Add(sessionDuration),
		CreatedAt: time.Now(),
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

	isSecure := false
	sameSite := http.SameSiteLaxMode
	if activeService != nil && activeService.env == "production" {
		isSecure = true
		sameSite = http.SameSiteNoneMode
	}

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

// currentUser returns the User associated with the active request session cookie, if valid.
func currentUser(r *http.Request) (*User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}

	token := cookie.Value

	// If DB is available, query DB for active session
	if activeService != nil && activeService.db != nil {
		query := `
		SELECT u.id, u.email, u.name, u.password_hash, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > $2
		`
		var u User
		err := activeService.db.QueryRow(query, token, time.Now()).Scan(
			&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
		)
		if err == nil {
			return &u, true
		}
	}

	// Fallback to in-memory store
	mu.RLock()
	session, exists := sessions[token]
	if !exists {
		mu.RUnlock()
		return nil, false
	}

	if time.Now().After(session.ExpiresAt) {
		mu.RUnlock()
		return nil, false
	}

	user, userExists := users[session.UserID]
	mu.RUnlock()

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
		query := `SELECT id, email, name, password_hash, created_at, updated_at FROM users WHERE email = $1`
		var u User
		err := activeService.db.QueryRow(query, email).Scan(
			&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
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

func saveUser(u *User) error {
	// Persist to DB if connected
	if activeService != nil && activeService.db != nil {
		query := `INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`
		_, err := activeService.db.Exec(query, u.ID, u.Email, u.Name, u.PasswordHash, u.CreatedAt, u.UpdatedAt)
		if err != nil {
			return errors.New("user with this email already exists")
		}
	}

	// Update in-memory cache
	mu.Lock()
	defer mu.Unlock()
	if _, exists := byEmail[u.Email]; exists && (activeService == nil || activeService.db == nil) {
		return errors.New("user already exists")
	}
	users[u.ID] = u
	byEmail[u.Email] = u.ID
	return nil
}
