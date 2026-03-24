CREATE TABLE webhook_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider TEXT NOT NULL,
    event_type TEXT NOT NULL,    
    reference TEXT UNIQUE NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    retry_count INT DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 3,
    error TEXT,
    created_at TIMESTAMP DEFAULT now(),
    processed_at TIMESTAMP,
    locked_until TIMESTAMP
);

CREATE INDEX idx_webhook_events_status ON webhook_events(status);
CREATE INDEX idx_webhook_events_event_type ON webhook_events(event_type);
CREATE INDEX idx_webhook_events_pending ON webhook_events(status, locked_until) 
    WHERE status = 'pending';