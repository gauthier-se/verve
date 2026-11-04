package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
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
}

// Page is a page of a list, the body of the response of a list operation.
type Page[T any] struct {
	Items []T `json:"items" nullable:"false" doc:"The items of the page, newest first."`
	// NextCursor is empty on the last page.
	NextCursor string `json:"nextCursor,omitempty" doc:"The cursor of the next page, left out on the last page."`
}

// limit returns the number of items of the page that p asks for.
func (p PageParams) limit() int {
	return min(p.Limit, maxLimit)
}

// storePage returns the page of the store that p asks for, or a 422 problem when its cursor is
// malformed. The page has one item more than the limit, which tells cutPage whether a next page
// exists.
func (p PageParams) storePage() (store.Page, error) {
	page := store.Page{Limit: p.limit() + 1}

	if p.Cursor != "" {
		after, err := decodeCursor(p.Cursor)
		if err != nil {
			return store.Page{}, huma.NewError(http.StatusUnprocessableEntity, "validation failed",
				&huma.ErrorDetail{Message: "invalid cursor", Location: "query.cursor", Value: p.Cursor})
		}

		page.After = after
	}

	return page, nil
}

// cutPage returns the rows of the page that p asks for, out of the rows that the store returned for
// p.storePage(), and the cursor of the next page, or "" when there is none. cursorOf returns the
// position of a row.
func cutPage[T any](p PageParams, rows []T, cursorOf func(T) store.Cursor) ([]T, string) {
	limit := p.limit()
	if len(rows) <= limit {
		return rows, ""
	}

	rows = rows[:limit]

	return rows, encodeCursor(cursorOf(rows[limit-1]))
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
