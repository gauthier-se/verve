-- +goose Up
-- Finds the jobs of a user, which account deletion cancels and then removes in batches.
CREATE INDEX jobs_user_id ON jobs (user_id) WHERE user_id IS NOT NULL;
