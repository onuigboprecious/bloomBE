package auth

import (
	"database/sql"
	"log"
	"net/http"
)

// Service manages authentication workflows and database interactions.
type Service struct {
	db  *sql.DB
	env string
}

var activeService *Service

// New constructs a new auth Service instance and initializes database tables if db is provided.
func New(db *sql.DB, env string) *Service {
	svc := &Service{
		db:  db,
		env: env,
	}
	activeService = svc

	if db != nil {
		if err := svc.initSchema(); err != nil {
			log.Printf("auth: warning - database schema migration error: %v", err)
		}
	}

	return svc
}

// initSchema creates users and sessions tables if they do not exist.
func (s *Service) initSchema() error {
	if s.db == nil {
		return nil
	}

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id VARCHAR(64) PRIMARY KEY,
		email VARCHAR(255) UNIQUE NOT NULL,
		name VARCHAR(255) NOT NULL,
		password_hash VARCHAR(255),
		google_id VARCHAR(128),
		avatar_url VARCHAR(512),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	ALTER TABLE users ADD COLUMN IF NOT EXISTS google_id VARCHAR(128);
	ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_url VARCHAR(512);
	ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

	CREATE TABLE IF NOT EXISTS sessions (
		id VARCHAR(64) PRIMARY KEY,
		user_id VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token VARCHAR(128) UNIQUE NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_id ON users(google_id) WHERE google_id IS NOT NULL;
	CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
	`

	_, err := s.db.Exec(schema)
	return err
}

// HandleSignup delegates to handleSignup.
func (s *Service) HandleSignup(w http.ResponseWriter, r *http.Request) {
	handleSignup(w, r)
}

// HandleLogin delegates to handleLogin.
func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	handleLogin(w, r)
}

// HandleLogout delegates to handleLogout.
func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	handleLogout(w, r)
}

// HandleMe delegates to handleMe.
func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request) {
	handleMe(w, r)
}

// RequireAuth wraps an http.HandlerFunc with authentication verification middleware.
func (s *Service) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(next)
}
