-- +goose Up
-- aggregation_key groups the bursts of an event type, such as the failed logins of one client IP,
-- into the count of one row. It is NULL for the events that are never aggregated, which the index
-- leaves out.
ALTER TABLE audit_events ADD COLUMN aggregation_key TEXT;

CREATE INDEX audit_events_type_aggregation_key_last_at ON audit_events (type, aggregation_key, last_at)
    WHERE aggregation_key IS NOT NULL;
