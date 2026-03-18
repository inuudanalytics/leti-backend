
CREATE TYPE admin_role AS ENUM ('super_admin', 'admin', 'support');

CREATE TABLE admins (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    full_name           VARCHAR(100) NOT NULL,
    email               VARCHAR(255) NOT NULL UNIQUE,
    password            VARCHAR(255) NOT NULL,
    role                admin_role NOT NULL DEFAULT 'super_admin',
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at       TIMESTAMP,
    password_changed_at TIMESTAMP,
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          UUID REFERENCES admins(id) ON DELETE SET NULL
);

CREATE INDEX idx_admins_email ON admins(email);
CREATE INDEX idx_admins_role  ON admins(role);