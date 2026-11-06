-- +goose Up
-- The preferences of each user, deleted with them. A user has no row until they set a preference,
-- and the zero value of each preference is NULL. M2 adds the other preferences as columns.
CREATE TABLE user_preferences (
    user_id  INTEGER NOT NULL PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    timezone TEXT -- IANA name, NULL until the user confirms one
) STRICT;
