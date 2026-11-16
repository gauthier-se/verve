-- +goose Up
-- The period and the routes of the report of an import (Ingestion pipeline FR-3): the first start
-- and the last end of the records read that are not invalid, both NULL when there is none, and the
-- number of routes stored with a workout. They are saved with the counters, in the transaction of
-- each batch.
ALTER TABLE imports ADD COLUMN period_start INTEGER; -- Unix milliseconds, UTC
ALTER TABLE imports ADD COLUMN period_end INTEGER; -- Unix milliseconds, UTC
ALTER TABLE imports ADD COLUMN routes INTEGER NOT NULL DEFAULT 0 CHECK (routes >= 0);
-- The uncompressed size of the export.xml of an XML import, stored once its job opens the export,
-- which its progress is computed against (XML import FR-7). NULL until then and for other origins.
ALTER TABLE imports ADD COLUMN export_size INTEGER CHECK (export_size IS NULL OR export_size > 0);
