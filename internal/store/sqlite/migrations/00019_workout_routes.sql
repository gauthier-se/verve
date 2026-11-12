-- +goose Up
-- The route of each workout (Storage and aggregates FR-5), at most one, encoded by
-- internal/workouts/route (ADR 0014) with its bounding box, total distance and simplified polyline.
-- user_id is that of the workout: it scopes reads without a join, and lets account deletion remove
-- the routes, the largest rows, in batches of their own before the workouts.
CREATE TABLE workout_routes (
    workout_id  INTEGER PRIMARY KEY REFERENCES workouts (id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL,
    encoded     BLOB    NOT NULL CHECK (length(encoded) > 0),
    min_lat     REAL    NOT NULL CHECK (min_lat >= -90), -- degrees
    min_lon     REAL    NOT NULL CHECK (min_lon >= -180), -- degrees
    max_lat     REAL    NOT NULL CHECK (max_lat >= min_lat AND max_lat <= 90), -- degrees
    max_lon     REAL    NOT NULL CHECK (max_lon >= min_lon AND max_lon <= 180), -- degrees
    distance    REAL    NOT NULL CHECK (distance >= 0), -- m
    point_count INTEGER NOT NULL CHECK (point_count >= 1),
    polyline    TEXT    NOT NULL -- Google encoded polyline
) STRICT;

CREATE INDEX workout_routes_user ON workout_routes (user_id);
