-- +goose Up
-- The inbox file of an import, which holds its payload until the import is done (Ingestion pipeline,
-- Data and API notes), so that cleanup.inbox tells the files that imports still need from the
-- others. NULL for an import without inbox file.
ALTER TABLE imports ADD COLUMN inbox_file TEXT CHECK (inbox_file IS NULL OR inbox_file <> '');
