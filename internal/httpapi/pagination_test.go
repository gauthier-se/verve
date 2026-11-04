package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// Given a cursor, then its encoding is URL-safe and decodes to the same cursor.
func TestPagination_CursorRoundTrips(t *testing.T) {
	t.Parallel()

	for _, c := range []store.Cursor{
		{At: time.Date(2026, 11, 4, 8, 30, 15, 123_000_000, time.UTC), ID: 42},
		{At: time.Date(1969, 7, 20, 20, 17, 40, 0, time.UTC), ID: 1},
		{At: time.UnixMilli(math.MaxInt64).UTC(), ID: math.MaxInt64},
	} {
		encoded := encodeCursor(c)
		require.Equal(t, encoded, url.QueryEscape(encoded))

		decoded, err := decodeCursor(encoded)
		require.NoError(t, err, encoded)
		require.Equal(t, c, decoded)
	}
}

// rawCursor returns text encoded as a cursor is, so that the test can build malformed ones.
func rawCursor(text string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(text))
}

// Given a value that encodeCursor never returns, then decodeCursor fails: not base64url, no
// separator, too many parts, parts that are not integers, an id below 1, or another spelling of a
// valid cursor.
func TestPagination_MalformedCursorFailsToDecode(t *testing.T) {
	t.Parallel()

	valid := encodeCursor(store.Cursor{At: time.UnixMilli(1_762_243_200_000).UTC(), ID: 42})

	for _, cursor := range []string{
		"",
		"not a cursor!",
		rawCursor("1762243200000"),
		rawCursor("1762243200000.42.1"),
		rawCursor("now.42"),
		rawCursor("1762243200000.last"),
		rawCursor("1762243200000.0"),
		rawCursor("1762243200000.-42"),
		rawCursor("1762243200000.042"),
		rawCursor("+1762243200000.42"),
		valid + "=",
		base64.URLEncoding.EncodeToString([]byte("1762243200000.42")),
	} {
		_, err := decodeCursor(cursor)
		require.Error(t, err, cursor)
	}
}

// testListAt is the time of the items of the test-only list operation.
var testListAt = time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)

// testListItem is an item of the test-only list operation.
type testListItem struct {
	ID int64 `json:"id"`
}

// testListOutput is the response of the test-only list operation.
type testListOutput struct {
	Body Page[testListItem]
}

// testListAPI returns an API with a test-only list operation GET /api/v1/test/items, which pages
// through count items at testListAt, newest first, the newest having the id count.
func testListAPI(t *testing.T, count int64) humatest.TestAPI {
	t.Helper()

	api := NewAPI(http.NewServeMux(), APIDeps{})
	huma.Register(api, huma.Operation{
		OperationID: "test-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/test/items",
	}, func(_ context.Context, in *PageParams) (*testListOutput, error) {
		// A fake of the store: the items after the cursor, at most the limit.
		list := func(page store.PageRequest) ([]store.Cursor, error) {
			var rows []store.Cursor

			for id := count; id >= 1 && len(rows) < page.Limit; id-- {
				if page.After == (store.Cursor{}) || id < page.After.ID {
					rows = append(rows, store.Cursor{At: testListAt, ID: id})
				}
			}

			return rows, nil
		}

		rows, next, err := listPage(*in, list, func(c store.Cursor) store.Cursor { return c })
		if err != nil {
			return nil, err
		}

		items := make([]testListItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, testListItem{ID: row.ID})
		}

		return &testListOutput{Body: Page[testListItem]{Items: items, NextCursor: next}}, nil
	})

	return humatest.Wrap(t, api)
}

// testPage is a page of the test-only list operation, as the tests read it.
type testPage struct {
	Items      []testListItem `json:"items"`
	NextCursor *string        `json:"nextCursor"`
}

// getTestPage gets target from api, checks that it answers 200, and returns the page.
func getTestPage(t *testing.T, api humatest.TestAPI, target string) testPage {
	t.Helper()

	rec := api.Get(target)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var page testPage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))

	return page
}

// Given a list request without limit, then the page has 50 items. Given a limit, then the page has
// at most that many items, and at most 200 for a larger limit.
func TestPagination_LimitDefaultsTo50AndIsCappedAt200(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		query string
		want  int
	}{
		{query: "", want: 50},
		{query: "?limit=1", want: 1},
		{query: "?limit=120", want: 120},
		{query: "?limit=200", want: 200},
		{query: "?limit=201", want: 200},
		{query: "?limit=5000", want: 200},
	} {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()

			page := getTestPage(t, testListAPI(t, 300), "/api/v1/test/items"+tt.query)

			require.Len(t, page.Items, tt.want)
			require.Equal(t, int64(300), page.Items[0].ID)
			require.NotNil(t, page.NextCursor)
		})
	}
}

// Given more items than the limit, then following nextCursor returns each item once, in order, and
// the last page has no nextCursor.
func TestPagination_FollowingNextCursorReturnsEachItemOnce(t *testing.T) {
	t.Parallel()

	api := testListAPI(t, 7)

	var ids []int64

	page := getTestPage(t, api, "/api/v1/test/items?limit=3")
	ids = append(ids, itemIDs(page)...)

	for page.NextCursor != nil {
		page = getTestPage(t, api, "/api/v1/test/items?limit=3&cursor="+url.QueryEscape(*page.NextCursor))
		ids = append(ids, itemIDs(page)...)
	}

	require.Equal(t, []int64{7, 6, 5, 4, 3, 2, 1}, ids)
}

// itemIDs returns the ids of the items of page, in order.
func itemIDs(page testPage) []int64 {
	ids := make([]int64, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}

	return ids
}

// Given exactly as many items as the limit, then the page has no nextCursor. Given no item, then
// items is an empty array, never null.
func TestPagination_LastPageHasNoNextCursor(t *testing.T) {
	t.Parallel()

	rec := testListAPI(t, 2).Get("/api/v1/test/items?limit=2")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"items":[{"id":2},{"id":1}]}`, rec.Body.String())

	rec = testListAPI(t, 0).Get("/api/v1/test/items")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"items":[]}`, rec.Body.String())
}

// Given a malformed cursor, then 422 validation_failed, with the cursor as the invalid field.
func TestPagination_MalformedCursorReturnsValidationFailed(t *testing.T) {
	t.Parallel()

	rec := testListAPI(t, 3).Get("/api/v1/test/items?cursor=nope")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{
		"type": "about:blank",
		"title": "Unprocessable Entity",
		"status": 422,
		"detail": "validation failed",
		"code": "validation_failed",
		"errors": [{"message": "invalid cursor", "location": "query.cursor", "value": "nope"}]
	}`, rec.Body.String())
}

// Given a limit below 1, then 422 validation_failed, with the limit as the invalid field.
func TestPagination_LimitBelowOneReturnsValidationFailed(t *testing.T) {
	t.Parallel()

	rec := testListAPI(t, 3).Get("/api/v1/test/items?limit=0")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.JSONEq(t, `{
		"type": "about:blank",
		"title": "Unprocessable Entity",
		"status": 422,
		"detail": "validation failed",
		"code": "validation_failed",
		"errors": [{"message": "expected number >= 1", "location": "query.limit", "value": 0}]
	}`, rec.Body.String())
}

// Given a malformed cursor and a limit below 1, then the 422 lists both invalid fields.
func TestPagination_MalformedCursorIsReportedWithTheOtherInvalidFields(t *testing.T) {
	t.Parallel()

	rec := testListAPI(t, 3).Get("/api/v1/test/items?limit=0&cursor=nope")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.JSONEq(t, `{
		"type": "about:blank",
		"title": "Unprocessable Entity",
		"status": 422,
		"detail": "validation failed",
		"code": "validation_failed",
		"errors": [
			{"message": "expected number >= 1", "location": "query.limit", "value": 0},
			{"message": "invalid cursor", "location": "query.cursor", "value": "nope"}
		]
	}`, rec.Body.String())
}
