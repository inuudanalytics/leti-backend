CREATE TABLE platform_commissions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    job_id UUID REFERENCES jobs(id) ON DELETE CASCADE,
    booking_id UUID REFERENCES artisan_bookings(id) ON DELETE CASCADE,
    artisan_id UUID NOT NULL REFERENCES users(id),
    amount DECIMAL(15,2) NOT NULL CHECK (amount > 0),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'waived')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    paid_at TIMESTAMP,

    CONSTRAINT chk_commission_source CHECK (
        (job_id IS NOT NULL AND booking_id IS NULL)
        OR
        (booking_id IS NOT NULL AND job_id IS NULL)
    )
);

CREATE INDEX idx_platform_commissions_artisan_id ON platform_commissions(artisan_id);
CREATE INDEX idx_platform_commissions_status ON platform_commissions(status);

CREATE UNIQUE INDEX IF NOT EXISTS uq_platform_commission_booking
    ON platform_commissions(booking_id)
    WHERE booking_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_platform_commission_job
    ON platform_commissions(job_id)
    WHERE job_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_platform_commissions_booking_id
    ON platform_commissions(booking_id)
    WHERE booking_id IS NOT NULL;