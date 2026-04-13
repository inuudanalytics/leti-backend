CREATE TABLE IF NOT EXISTS calls (
    id                  UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),

    provider            VARCHAR(20)  NOT NULL DEFAULT 'stream',
    provider_call_type  VARCHAR(30)  NOT NULL DEFAULT 'audio_room',
    provider_call_id    TEXT         NOT NULL UNIQUE,

    context_type        VARCHAR(20)  NOT NULL
        CHECK (context_type IN ('booking', 'order')),
    context_id          UUID         NOT NULL,   -- artisan_bookings.id or orders.id
    
    caller_id           UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    callee_id           UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    audio_only          BOOLEAN      NOT NULL DEFAULT TRUE,

    state               VARCHAR(20)  NOT NULL DEFAULT 'ringing'
        CHECK (state IN ('ringing','accepted','ended','rejected','missed','failed')),
    end_reason          VARCHAR(30),

    started_at          TIMESTAMPTZ,
    ended_at            TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    duration_seconds    INT          NOT NULL DEFAULT 0,

    client_call_id      TEXT         UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_calls_context
    ON calls(context_type, context_id)
    WHERE state IN ('ringing', 'accepted');

CREATE INDEX IF NOT EXISTS idx_calls_caller_id   ON calls(caller_id);
CREATE INDEX IF NOT EXISTS idx_calls_callee_id   ON calls(callee_id);

CREATE INDEX IF NOT EXISTS idx_calls_ringing_created
    ON calls(created_at)
    WHERE state = 'ringing';

CREATE OR REPLACE FUNCTION compute_call_duration()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.ended_at IS NOT NULL AND NEW.started_at IS NOT NULL THEN
        NEW.duration_seconds :=
            GREATEST(0, EXTRACT(EPOCH FROM (NEW.ended_at - NEW.started_at))::INT);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_compute_call_duration ON calls;
CREATE TRIGGER trg_compute_call_duration
    BEFORE UPDATE ON calls
    FOR EACH ROW
    EXECUTE FUNCTION compute_call_duration();