CREATE TABLE platform_settings (
    key         VARCHAR(100) PRIMARY KEY,
    value       TEXT NOT NULL,
    description TEXT,
    updated_by  UUID REFERENCES admins(id) ON DELETE SET NULL,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO platform_settings (key, value, description) VALUES
    ('maintenance_mode',         'false',  'Put the platform in read-only maintenance mode'),
    ('allow_new_registrations',  'true',   'Allow new users to register'),
    ('platform_service_charge',  '5',   'Platform service charge percentage on usage by car owner'),
    ('platform_fee_percent',     '8',     'Platform commission percentage on each job'),
    ('min_withdrawal_amount',    '500',    'Minimum wallet withdrawal in NGN');