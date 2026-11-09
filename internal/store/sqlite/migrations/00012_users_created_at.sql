-- +goose Up
-- The admin user list pages through the users newest first by creation.
CREATE INDEX users_created_at ON users (created_at, id);
