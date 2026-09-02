-- Add google_id and avatar_url columns to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS google_id VARCHAR(128);
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_url VARCHAR(512);
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_id ON users(google_id) WHERE google_id IS NOT NULL;
