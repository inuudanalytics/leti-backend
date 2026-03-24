CREATE TABLE artisan_service_options (
    id                UUID           PRIMARY KEY DEFAULT uuid_generate_v4(),
    service_id        UUID           NOT NULL REFERENCES artisan_services(id) ON DELETE CASCADE,
    variation_type_id UUID            NOT NULL REFERENCES service_variation_types(id) ON DELETE RESTRICT,
    label             VARCHAR(100)   NOT NULL,     
    price_modifier    NUMERIC(12, 2) NOT NULL DEFAULT 0
                                     CHECK (price_modifier >= 0),
    created_at        TIMESTAMP      NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_service_option_label UNIQUE (service_id, variation_type_id, label)
);

CREATE INDEX idx_service_options_service_id
    ON artisan_service_options(service_id);

CREATE INDEX idx_service_options_variation_type_id
    ON artisan_service_options(variation_type_id);