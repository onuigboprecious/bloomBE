-- Migration 000007: Create Subscriptions Schema
CREATE TABLE IF NOT EXISTS subscriptions (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id VARCHAR(100) NOT NULL DEFAULT 'enlazer_pro_annual',
    plan_name VARCHAR(255) NOT NULL DEFAULT 'Enlazer Pro Annual',
    billing_cycle VARCHAR(50) NOT NULL DEFAULT 'annual',
    amount INT NOT NULL DEFAULT 19000,
    currency VARCHAR(10) NOT NULL DEFAULT 'NGN',
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    starts_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '1 year'),
    payment_reference VARCHAR(255) DEFAULT '',
    payment_channel VARCHAR(50) DEFAULT 'paystack',
    auto_renew BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status);
