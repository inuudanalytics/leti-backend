CREATE TABLE owner_address (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address_type VARCHAR(20) NOT NULL CHECK (address_type IN ('home', 'work')),
    label VARCHAR(100),
    street VARCHAR(255) NOT NULL,
    city VARCHAR(100) NOT NULL,
    state VARCHAR(100) NOT NULL,
    country VARCHAR(100) NOT NULL DEFAULT 'Nigeria',
    location GEOMETRY(Point, 4326),
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT one_home_address_per_owner
        EXCLUDE USING btree (owner_id WITH =, address_type WITH =)
        WHERE (address_type = 'home')
);

CREATE INDEX idx_owner_address_owner_id ON owner_address(owner_id);
CREATE INDEX idx_owner_address_location ON owner_address USING GIST(location);