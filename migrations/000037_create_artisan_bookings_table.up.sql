CREATE TYPE artisan_booking_status AS ENUM (
    'pending',      -- client requested, waiting for artisan
    'confirmed',    -- artisan accepted
    'declined',     -- artisan rejected
    'cancelled',    -- cancelled by either party
    'completed',    -- service delivered
    'awaiting_client_confirmation',
    'disputed'
);
 
CREATE TABLE artisan_bookings (
    id                  UUID                    PRIMARY KEY DEFAULT uuid_generate_v4(),
    client_id           UUID                    NOT NULL REFERENCES users(id),
    artisan_id          UUID                    NOT NULL REFERENCES users(id),
    category_id         UUID                     NOT NULL REFERENCES job_categories(id),
    service_id          UUID                    REFERENCES artisan_services(id) ON DELETE RESTRICT,
    service_option_id   UUID                    REFERENCES artisan_service_options(id) ON DELETE RESTRICT,
    booking_date        DATE                    NOT NULL,
    start_time          TIME                    NOT NULL,
    end_time            TIME                    NOT NULL,
    total_price         NUMERIC(12, 2)          NOT NULL CHECK (total_price >= 0),
    address             TEXT,
    location            GEOMETRY(Point, 4326),
    note                TEXT,
    status              artisan_booking_status  NOT NULL DEFAULT 'pending',
    payment_method      VARCHAR(20)             CHECK (payment_method IN ('wallet', 'paystack')),
    payment_status      VARCHAR(20)             NOT NULL DEFAULT 'pending'
        CHECK (payment_status IN ('pending', 'paid', 'failed')),
    payment_reference   TEXT,
    confirmed_at        TIMESTAMPTZ,
    declined_at         TIMESTAMPTZ,
    cancelled_at        TIMESTAMPTZ,
    client_completed_at TIMESTAMPTZ DEFAULT NULL,
    artisan_completed_at TIMESTAMPTZ DEFAULT NULL,
    cancelled_by        UUID                    REFERENCES users(id),
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ             NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ             NOT NULL DEFAULT NOW(),
 
    CONSTRAINT chk_booking_window CHECK (end_time > start_time)
);
 
CREATE INDEX idx_artisan_bookings_artisan    ON artisan_bookings(artisan_id, status);
CREATE INDEX idx_artisan_bookings_client     ON artisan_bookings(client_id, status);
CREATE INDEX idx_artisan_bookings_date       ON artisan_bookings(artisan_id, booking_date);
CREATE INDEX idx_artisan_bookings_status     ON artisan_bookings(status);
 
CREATE TRIGGER trg_artisan_bookings_updated_at
    BEFORE UPDATE ON artisan_bookings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
 