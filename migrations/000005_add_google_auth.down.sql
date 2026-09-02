-- Rollback google_id and avatar_url columns from users table
DROP INDEX IF EXISTS idx_users_google_id;
ALTER TABLE users DROP COLUMN IF EXISTS avatar_url;
ALTER TABLE users DROP COLUMN IF EXISTS google_id;
