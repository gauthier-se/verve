package httpapi

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/store"
)

// maxLimit is the largest number of items of a page. The default of 50 is the tag of
// PageParams.Limit.
const maxLimit = 200

// PageParams are the query parameters of a list operation, embedded in its input. Lists are sorted
// newest first and paginated by an opaque cursor on the time and the id of their items, so that
// following the cursors returns each item once, even while new items arrive.
type PageParams struct {
	Cursor string `query:"cursor" doc:"The nextCursor of the previous page, left out for the first page."`
	Limit  int    `query:"limit" default:"50" minimum:"1" doc:"The maximum number of items of the page, at most 200: a larger limit counts as 200."`

	// after is the position that Cursor encodes, set by Resolve.
	after store.Cursor
}

// Resolve decodes the cursor, so that Huma reports a malformed one with the other invalid fields.
// An input that embeds PageParams and has its own Resolve must call this one.
func (p *PageParams) Resolve(huma.Context) []error {
	return resolveCursor(p.Cursor, &p.after)
}

// resolveCursor decodes cursor, the query parameter cursor, into after for a Huma resolver, and
// returns the error of a malformed one. An empty cursor leaves after zero.
func resolveCursor(cursor string, after *store.Cursor) []error {
	if cursor == "" {
		return nil
	}

	decoded, err := decodeCursor(cursor)
	if err != nil {
		return []error{&huma.ErrorDetail{Message: "invalid cursor", Location: "query.cursor", Value: cursor}}
	}

	*after = decoded

	return nil
}

// Page is a page of a list, the body of the response of a list operation.
type Page[T any] struct {
	Items []T `json:"items" nullable:"false" doc:"The items of the page, newest first."`
	// NextCursor is empty on the last page.
	NextCursor string `json:"nextCursor,omitempty" doc:"The cursor of the next page, left out on the last page."`
}

// listPage returns the rows of the page that p asks for, and the cursor of the next page, or ""
// when there is none. list returns the rows of the store for a page request, and cursorOf the
// position of a row.
func listPage[T any](p PageParams, list func(store.PageRequest) ([]T, error), cursorOf func(T) store.Cursor) ([]T, string, error) {
	limit := min(p.Limit, maxLimit)

	// One row more than the limit tells whether a next page exists.
	rows, err := list(store.PageRequest{After: p.after, Limit: limit + 1})
	if err != nil {
		return nil, "", err
	}

	if len(rows) <= limit {
		return rows, "", nil
	}

	rows = rows[:limit]

	return rows, encodeCursor(cursorOf(rows[limit-1])), nil
}

// cursorSeparator separates the time and the id of a cursor.
const cursorSeparator = "."

// encodeCursor returns the opaque form of c: its time in Unix milliseconds and its id, in base64url.
func encodeCursor(c store.Cursor) string {
	text := strconv.FormatInt(c.At.UnixMilli(), 10) + cursorSeparator + strconv.FormatInt(c.ID, 10)

	return base64.RawURLEncoding.EncodeToString([]byte(text))
}

// errMalformedCursor is returned by decodeCursor for a value that encodeCursor does not return.
var errMalformedCursor = errors.New("malformed cursor")

// decodeCursor returns the cursor that encodeCursor encoded as s, or errMalformedCursor.
func decodeCursor(s string) (store.Cursor, error) {
	text, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return store.Cursor{}, errMalformedCursor
	}

	millis, id, found := strings.Cut(string(text), cursorSeparator)
	if !found {
		return store.Cursor{}, errMalformedCursor
	}

	at, atErr := strconv.ParseInt(millis, 10, 64)
	itemID, idErr := strconv.ParseInt(id, 10, 64)

	if atErr != nil || idErr != nil || itemID < 1 {
		return store.Cursor{}, errMalformedCursor
	}

	c := store.Cursor{At: time.UnixMilli(at).UTC(), ID: itemID}

	// Another spelling of a cursor, such as a leading zero or a plus sign, is not a cursor.
	if encodeCursor(c) != s {
		return store.Cursor{}, errMalformedCursor
	}

	return c, nil
}
