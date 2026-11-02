# 0013. Case-insensitive logins through folded columns

- **Status:** Accepted
- **Date:** 2026-11-02

## Context

Usernames and emails are unique ignoring case, and a login is looked up ignoring case. SQLite's `COLLATE NOCASE` and `lower()` only fold ASCII. With them, `élodie` and `Élodie` are two accounts, and a phone that capitalizes the first letter of a form field turns the login `élodie` into one that fails. Verve is used by households, so names outside ASCII are not an edge case.

## Decision

- The store computes the case-folded form of the username and of the email, and keeps it next to the original, in `username_fold` and `email_fold`.
- Uniqueness and lookups use the folded columns. The original columns keep the case they were created with, for display.
- Folding is `strings.ToLower(strings.ToUpper(s))`, from the standard library. Going through the upper case first merges letters with two lower case forms, such as the final sigma.
- This behavior is part of the `UsersRepository` contract: `storetest.RunUsers` holds a storage implementation to it, in every alphabet.

## Consequences

- Two columns exist only for comparison. Whatever changes a username or an email later must update them too.
- Changing the folding needs a data migration that recomputes both columns.
- The Turkish `ı` and `İ` fold to `i`, so `ılker` conflicts with `ilker`: a false conflict, on the safe side.
- Folding handles case, not Unicode normalization: `é` as one code point and `é` as `e` followed by a combining accent stay different. Common keyboards produce the first form, so this is left out for now.

## Alternatives considered

- **`COLLATE NOCASE`**: no extra column, but it does not meet the requirement outside ASCII.
- **A collation or function registered on the driver**: the schema would depend on code that only Verve registers, and the `sqlite3` shell could no longer write to the table.
- **ASCII usernames only**: the simplest rule, but it refuses people's names, and it says nothing about emails.
- **`golang.org/x/text/cases`**: full Unicode case folding, at the cost of a dependency, for a difference that only touches rare letters such as `ß`.
