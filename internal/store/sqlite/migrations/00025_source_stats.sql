-- +goose Up
-- The sample count of each source of a user, which the sources list reads (Deduplication and source
-- priority, Data and API notes), maintained in the transactions that maintain sample_stats. A row
-- whose count drops to zero is deleted. It references (user_id, id) of sources, so that it leaves
-- with a deleted source, as a merge deletes.
CREATE TABLE source_stats (
    user_id   INTEGER NOT NULL,
    source_id INTEGER NOT NULL,
    count     INTEGER NOT NULL CHECK (count > 0),
    PRIMARY KEY (user_id, source_id),
    FOREIGN KEY (user_id, source_id) REFERENCES sources (user_id, id) ON DELETE CASCADE
) STRICT, WITHOUT ROWID;

-- The index on samples (user_id, source_id, type_id) gives the samples of each source in order.
INSERT INTO source_stats (user_id, source_id, count)
SELECT user_id, source_id, count(*)
FROM samples
GROUP BY user_id, source_id;
