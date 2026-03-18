CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    first_name VARCHAR(100) NOT NULL,
    last_name VARCHAR(100) NOT NULL,
    username VARCHAR(50) UNIQUE,
    email VARCHAR(255) UNIQUE,
    phone_number VARCHAR(20) UNIQUE,
    avatar JSONB,
    password VARCHAR(255),
    password_changed_at TIMESTAMP,
    user_created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    password_reset_token VARCHAR(255),
    password_token_expires TIMESTAMP,
    otp VARCHAR(10),
    otp_expires TIMESTAMP,
    is_online BOOLEAN DEFAULT NULL,
    phone_verified BOOLEAN NOT NULL DEFAULT FALSE,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at TIMESTAMPTZ DEFAULT NULL,
    recovery_email VARCHAR(255),
    recovery_email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(50) NOT NULL DEFAULT 'approved' CHECK (status IN ('rejected','pending','suspended','approved','probation')),
    active_role VARCHAR(50) NOT NULL CHECK (active_role IN ('client','artisan','owner')),
    auth_provider VARCHAR(50) DEFAULT 'local',
    google_sub VARCHAR(255) UNIQUE,
    apple_sub VARCHAR(255) UNIQUE
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_deleted_at ON users(deleted_at) WHERE deleted_at IS NULL;
CREATE INDEX idx_users_phone_number ON users(phone_number);
CREATE INDEX idx_users_status ON users(status);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_active_role ON users(active_role);
CREATE INDEX idx_users_artisan_online ON users(is_online) WHERE active_role = 'artisan';
CREATE INDEX idx_users_recovery_email ON users(recovery_email) WHERE recovery_email IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_google_sub ON users(google_sub) WHERE google_sub IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_apple_sub ON users(apple_sub) WHERE apple_sub IS NOT NULL;

