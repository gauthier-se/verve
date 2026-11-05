-- +goose Up
-- Lists the finished jobs of a status newest first, by the time they ended, and finds the finished
-- jobs that the purge removes.
CREATE INDEX jobs_status_finished_at ON jobs (status, finished_at, id);
