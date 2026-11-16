-- +goose Up
-- The period and the routes of the report of an import (Ingestion pipeline FR-3): the first start
-- and the last end of the records read that are not invalid, both NULL when there is none, and the
-- number of routes stored with a workout. They are saved with the counters, in the transaction of
-- each batch.
ALTER TABLE imports ADD COLUMN period_start INTEGER; -- Unix milliseconds, UTC
ALTER TABLE imports ADD COLUMN period_end INTEGER; -- Unix milliseconds, UTC
ALTER TABLE imports ADD COLUMN routes INTEGER NOT NULL DEFAULT 0 CHECK (routes >= 0);
