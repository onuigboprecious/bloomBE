package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

// InitDB connects to PostgreSQL using DATABASE_URL and initializes all Bloom database tables.
func InitDB(databaseURL string) (*sql.DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is empty")
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres database: %w", err)
	}

	log.Println("PostgreSQL connection established successfully")

	if err := migrateSchema(db); err != nil {
		log.Printf("Warning: schema migration error: %v", err)
	}

	return db, nil
}

func migrateSchema(db *sql.DB) error {
	schema := `
	CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

	CREATE TABLE IF NOT EXISTS users (
		id VARCHAR(64) PRIMARY KEY,
		email VARCHAR(255) UNIQUE NOT NULL,
		name VARCHAR(255) NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		username VARCHAR(64) UNIQUE,
		is_pro BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

	ALTER TABLE users ADD COLUMN IF NOT EXISTS username VARCHAR(64) UNIQUE;
	ALTER TABLE users ADD COLUMN IF NOT EXISTS is_pro BOOLEAN NOT NULL DEFAULT FALSE;

	CREATE TABLE IF NOT EXISTS profiles (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		card_id VARCHAR(255) UNIQUE,
		card_uid VARCHAR(255) UNIQUE,
		user_id VARCHAR(64) REFERENCES users(id) ON DELETE CASCADE,
		display_name VARCHAR(255) NOT NULL DEFAULT '',
		title VARCHAR(255) NOT NULL DEFAULT '',
		company VARCHAR(255) NOT NULL DEFAULT '',
		bio TEXT NOT NULL DEFAULT '',
		avatar TEXT NOT NULL DEFAULT '',
		email VARCHAR(255) NOT NULL DEFAULT '',
		phone VARCHAR(50) NOT NULL DEFAULT '',
		website TEXT NOT NULL DEFAULT '',
		location VARCHAR(255) NOT NULL DEFAULT '',
		theme VARCHAR(50) NOT NULL DEFAULT 'dark-luxe',
		layout VARCHAR(50) NOT NULL DEFAULT 'stack',
		status VARCHAR(20) NOT NULL DEFAULT 'active',
		socials_json JSONB DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_profiles_card_uid ON profiles(card_uid);
	CREATE INDEX IF NOT EXISTS idx_profiles_user_id ON profiles(user_id);

	CREATE TABLE IF NOT EXISTS socials (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		profile_id UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
		platform VARCHAR(50) NOT NULL,
		handle VARCHAR(255) NOT NULL DEFAULT '',
		url TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS nfc_cards (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		card_uid VARCHAR(255) UNIQUE NOT NULL,
		user_id VARCHAR(64) REFERENCES users(id) ON DELETE SET NULL,
		finish_name VARCHAR(100) DEFAULT 'Stealth Matte Black',
		status VARCHAR(50) DEFAULT 'claimed',
		taps_count INT DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS leads (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id VARCHAR(64) REFERENCES users(id) ON DELETE CASCADE,
		card_uid VARCHAR(255) NOT NULL DEFAULT '',
		name VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL DEFAULT '',
		phone VARCHAR(50) NOT NULL DEFAULT '',
		role VARCHAR(255) NOT NULL DEFAULT '',
		method VARCHAR(50) NOT NULL DEFAULT 'NFC Tap',
		notes TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS taps (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		card_uid VARCHAR(255) NOT NULL,
		user_id VARCHAR(64) REFERENCES users(id) ON DELETE CASCADE,
		method VARCHAR(50) NOT NULL DEFAULT 'NFC Tap',
		tapped_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS waitlist (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL,
		phone VARCHAR(50) NOT NULL DEFAULT '',
		preferred_finish VARCHAR(100) NOT NULL DEFAULT 'Stealth Matte Black',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS orders (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id VARCHAR(64) REFERENCES users(id) ON DELETE SET NULL,
		finish_id VARCHAR(100) NOT NULL DEFAULT '',
		finish_name VARCHAR(255) NOT NULL DEFAULT '',
		quantity INT NOT NULL DEFAULT 1,
		amount INT NOT NULL DEFAULT 0,
		delivery_address TEXT NOT NULL DEFAULT '',
		status VARCHAR(50) NOT NULL DEFAULT 'pending',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS password_resets (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		email VARCHAR(255) NOT NULL,
		token VARCHAR(128) UNIQUE NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		used BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_password_resets_token ON password_resets(token);
	CREATE INDEX IF NOT EXISTS idx_password_resets_email ON password_resets(email);
	`

	_, err := db.Exec(schema)
	return err
}
