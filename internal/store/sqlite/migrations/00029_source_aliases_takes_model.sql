-- +goose Up
-- Whether an alias without model takes the model of a record that matches it (Deduplication and
-- source priority FR-2). An identity without model that takes a model leaves behind an alias of
-- its name without model that takes none: it keeps matching the records of that name without
-- model, so that a record keeps its source whatever the order of the records of an import.
ALTER TABLE source_aliases ADD COLUMN takes_model INTEGER NOT NULL DEFAULT 1 CHECK (takes_model IN (0, 1));
