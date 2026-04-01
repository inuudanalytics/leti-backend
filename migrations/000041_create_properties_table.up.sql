CREATE TYPE property_type AS ENUM (
    'apartment',
    'studio',
    '1_bedroom',
    '2_bedroom',
    '3_bedroom',
    '4_bedroom',
    '5_bedroom_plus',
    'duplex',
    'penthouse',
    'villa',
    'bungalow'
);
 
CREATE TYPE listing_status AS ENUM (
    'active',
    'inactive',
    'pending_review',     
    'suspended'           
);
 
CREATE TYPE order_status AS ENUM (
    'pending',            
    'confirmed',         
    'cancelled',         
    'checked_in',         
    'checked_out',      
    'completed',         
    'disputed'
);
 
CREATE TYPE order_dispute_status AS ENUM (
    'open',
    'investigating',
    'resolved',
    'closed'
);
 
 
CREATE TABLE properties (
    id                  UUID            PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_id            UUID            NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 
    name                VARCHAR(200)    NOT NULL,
    description         TEXT,
    property_type       property_type   NOT NULL,
    status              listing_status  NOT NULL DEFAULT 'active',
 
    price_per_night     NUMERIC(12, 2)  NOT NULL CHECK (price_per_night > 0),
    caution_fee         NUMERIC(12, 2)  NOT NULL DEFAULT 0.00 CHECK (caution_fee >= 0),
 
    images              JSONB           NOT NULL DEFAULT '[]'::jsonb,
 
    amenities           JSONB           NOT NULL DEFAULT '[]'::jsonb,
 
    house_rules         JSONB           NOT NULL DEFAULT '[]'::jsonb,
 
    max_adults          SMALLINT        NOT NULL DEFAULT 1 CHECK (max_adults >= 1),
    max_children        SMALLINT        NOT NULL DEFAULT 0 CHECK (max_children >= 0),
 
    state               VARCHAR(100)    NOT NULL,
    city                VARCHAR(100)    NOT NULL,
    street              VARCHAR(255)    NOT NULL,
    location            GEOMETRY(Point, 4326),
 
    avg_rating          NUMERIC(3, 2)   NOT NULL DEFAULT 0.00,
    review_count        INT             NOT NULL DEFAULT 0,
 
    deleted_at          TIMESTAMPTZ     DEFAULT NULL,
 
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
 
    CONSTRAINT property_images_check CHECK (
        jsonb_typeof(images) = 'array'
        AND jsonb_array_length(images) <= 5
    )
);
 
CREATE INDEX idx_properties_owner_id     ON properties(owner_id);
CREATE INDEX idx_properties_status       ON properties(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_properties_type         ON properties(property_type) WHERE deleted_at IS NULL;
CREATE INDEX idx_properties_state_city   ON properties(state, city) WHERE deleted_at IS NULL;
CREATE INDEX idx_properties_price        ON properties(price_per_night) WHERE deleted_at IS NULL;
CREATE INDEX idx_properties_rating       ON properties(avg_rating DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_properties_location     ON properties USING GIST(location);
CREATE INDEX idx_properties_deleted_at   ON properties(deleted_at) WHERE deleted_at IS NULL;
 
CREATE INDEX idx_properties_amenities    ON properties USING GIN(amenities);
 
CREATE TRIGGER trg_properties_updated_at
    BEFORE UPDATE ON properties
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();