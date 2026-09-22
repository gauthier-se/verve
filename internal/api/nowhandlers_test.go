package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

func getNow(t *testing.T, srv *Server, cookie *http.Cookie) (*http.Response, nowView) {
	t.Helper()
	res, body := do(t, srv, "/v1/now", cookie)
	var view nowView
	if raw, ok := body["now"]; ok {
		if err := json.Unmarshal(raw, &view); err != nil {
			t.Fatalf("decode now: %v", err)
		}
	}
	return res, view
}

func TestNowRequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res, _ := do(t, srv, "/v1/now")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", res.StatusCode)
	}
}

// An Account with no data answers a 200 and an empty Freshness: nothing is late,
// because nothing has arrived.
func TestNowOfAnEmptyAccount(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, view := getNow(t, srv, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if view.Freshness.LastDay != "" || view.Freshness.Sources == nil || len(view.Freshness.Sources) != 0 {
		t.Fatalf("empty Account: %+v", view.Freshness)
	}
}

// The age is counted by the server's clock, not the caller's.
func TestNowCarriesTheAccountsFreshness(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	at := time.Now().UTC().AddDate(0, 0, -3).Format(time.RFC3339)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 6500, OriginalUnit: "count", StartAt: at, EndAt: at, Source: "Apple Watch", ContentKey: "s1"},
	})

	_, view := getNow(t, srv, cookie)
	f := view.Freshness
	if f.AgeDays != 3 || len(f.Sources) != 1 || f.Sources[0].Source != "Apple Watch" {
		t.Fatalf("freshness: %+v", f)
	}
}

// A Pin is a card on Now, in the same call as the Freshness.
func TestNowCarriesAPinnedMetricAtItsLatestValue(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc, err := models.Accounts.GetByEmail(context.Background(), testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	at := time.Now().UTC().AddDate(0, 0, -40).Format(time.RFC3339)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 6500, OriginalUnit: "count", StartAt: at, EndAt: at, Source: "Apple Watch", ContentKey: "s1"},
	})
	if _, err := models.Pins.Add(context.Background(), acc.ID, "steps"); err != nil {
		t.Fatalf("pin: %v", err)
	}

	_, view := getNow(t, srv, cookie)
	if len(view.Cards) != 1 {
		t.Fatalf("cards: %+v", view.Cards)
	}
	c := view.Cards[0]
	if c.Metric != "steps" || c.Latest == nil || c.Latest.Value != 6500 || c.Latest.AgeDays != 40 {
		t.Fatalf("card: %+v %+v", c, c.Latest)
	}
}
