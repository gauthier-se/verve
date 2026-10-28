-- +goose Up
-- Admin-managed settings. Secrets are encrypted with the instance secret key.
CREATE TABLE instance_settings (
    key        TEXT    NOT NULL PRIMARY KEY,
    value      TEXT    NOT NULL,
    updated_at INTEGER NOT NULL -- Unix milliseconds, UTC
) STRICT;
