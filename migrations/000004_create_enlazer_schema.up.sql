-- Migration 000004: Create Enlazer full schema (Social Handles, Custom Bio Links, Tap Analytics)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Social Handles table
CREATE TABLE IF NOT EXISTS social_handles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform VARCHAR(50) NOT NULL,
    handle VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_social_handles_user_id ON social_handles(user_id);
CREATE INDEX IF NOT EXISTS idx_social_handles_platform ON social_handles(platform);

-- Custom Bio Links table
CREATE TABLE IF NOT EXISTS custom_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label VARCHAR(255) NOT NULL,
    url TEXT NOT NULL,
    link_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_custom_links_user_id ON custom_links(user_id);

-- Enlazer Detailed Tap Analytics table
CREATE TABLE IF NOT EXISTS tap_analytics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_uid VARCHAR(255),
    device_os VARCHAR(50) DEFAULT 'Desktop',
    location VARCHAR(255) DEFAULT 'Lagos, Nigeria',
    ip_address VARCHAR(64) DEFAULT '',
    user_agent TEXT DEFAULT '',
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tap_analytics_user_id ON tap_analytics(user_id);
CREATE INDEX IF NOT EXISTS idx_tap_analytics_card_uid ON tap_analytics(card_uid);

-- NFC Cards additions
ALTER TABLE nfc_cards ADD COLUMN IF NOT EXISTS chip_type VARCHAR(100) DEFAULT 'NXP NTAG216';
ALTER TABLE nfc_cards ADD COLUMN IF NOT EXISTS activated_at TIMESTAMPTZ;
