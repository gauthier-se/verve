package now

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

func setup(t *testing.T) (Engine, data.Models, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := data.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	models := data.NewModels(db)
	acc := &data.Account{Email: "owner@example.com"}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	return Engine{Query: query.Engine{DB: db}, Models: models}, models, acc.ID
}

// seed writes Measurements, filling in the Account and a unique content key.
func seed(t *testing.T, models data.Models, acc int64, ms ...data.Measurement) {
	t.Helper()
	for i := range ms {
		ms[i].AccountID = acc
		if ms[i].OriginalUnit == "" {
			ms[i].OriginalUnit = "count"
		}
		if ms[i].EndAt == "" {
			ms[i].EndAt = ms[i].StartAt
		}
		ms[i].ContentKey = fmt.Sprintf("%s-%s-%s-%d", ms[i].Metric, ms[i].Source, ms[i].StartAt, i)
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), ms); err != nil {
		t.Fatalf("seed measurements: %v", err)
	}
}

func seedSleep(t *testing.T, models data.Models, acc int64, source, start, end string) {
	t.Helper()
	_, err := models.States.InsertStateBatch(context.Background(), []data.State{{
		AccountID: acc, Kind: "sleep", StateValue: "asleep_core",
		StartAt: start, EndAt: end, Source: source,
		ContentKey: "sleep-" + source + "-" + start,
	}})
	if err != nil {
		t.Fatalf("seed states: %v", err)
	}
}

func steps(source, at string) data.Measurement {
	return data.Measurement{Metric: "steps", Value: 1000, StartAt: at, Source: source}
}

func freshness(t *testing.T, e Engine, acc int64, now string) Freshness {
	t.Helper()
	at, err := time.Parse(time.RFC3339, now)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Freshness(context.Background(), acc, at)
	if err != nil {
		t.Fatalf("Freshness: %v", err)
	}
	return out
}

func source(t *testing.T, f Freshness, name string) SourceFresh {
	t.Helper()
	for _, s := range f.Sources {
		if s.Source == name {
			return s
		}
	}
	t.Fatalf("no Source %q in %+v", name, f.Sources)
	return SourceFresh{}
}

// An Account with nothing in it is a Freshness with nothing in it, never an error
// and never an age invented against a day that does not exist.
func TestFreshnessOfAnEmptyAccount(t *testing.T) {
	e, _, acc := setup(t)
	f := freshness(t, e, acc, "2026-09-22T10:00:00Z")
	if f.LastDay != "" || f.AgeDays != 0 || len(f.Sources) != 0 {
		t.Fatalf("empty Account: got %+v", f)
	}
}

// The age is counted against the UTC today, the day a Goal and a window are dated
// by, so 23:30 in New York on the 21st is already the 22nd.
func TestFreshnessAgeCountsFromTheUTCToday(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc, steps("Apple Watch", "2026-07-25T15:10:00Z"))

	f := freshness(t, e, acc, "2026-09-21T23:30:00-05:00")
	if f.LastDay != "2026-07-25" || f.AgeDays != 59 {
		t.Fatalf("got last %s aged %d, want 2026-07-25 aged 59", f.LastDay, f.AgeDays)
	}
}

// A typed value says nothing about whether a device still sends: one weight typed
// today must not make a silent Watch look fresh.
func TestFreshnessIgnoresTheManualSource(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		steps("Apple Watch", "2026-09-01T08:00:00Z"),
		data.Measurement{Metric: "body_mass", Value: 75, OriginalUnit: "kg", StartAt: "2026-09-22T07:00:00Z", Source: "Manual"},
	)

	f := freshness(t, e, acc, "2026-09-22T10:00:00Z")
	if f.LastDay != "2026-09-01" {
		t.Fatalf("last day %s, want 2026-09-01: Manual moved it", f.LastDay)
	}
	for _, s := range f.Sources {
		if s.Source == "Manual" {
			t.Fatalf("Manual listed as a Source: %+v", f.Sources)
		}
	}
}

// A Source is measured against the Account's last datum, not against now: 30 days
// behind it is active, 31 is retired.
func TestFreshnessRetiresASourcePastThirtyDaysBehindTheAccount(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		steps("Apple Watch", "2026-07-25T12:00:00Z"),
		steps("iPhone", "2026-06-25T12:00:00Z"),    // 30 days behind
		steps("Zepp Life", "2026-06-24T12:00:00Z"), // 31 days behind
	)

	f := freshness(t, e, acc, "2026-09-22T10:00:00Z")
	if w := source(t, f, "Apple Watch"); w.LagDays != 0 || w.Retired {
		t.Fatalf("leader: %+v", w)
	}
	if p := source(t, f, "iPhone"); p.LagDays != 30 || p.Retired {
		t.Fatalf("30 days behind should be active: %+v", p)
	}
	if z := source(t, f, "Zepp Life"); z.LagDays != 31 || !z.Retired {
		t.Fatalf("31 days behind should be retired: %+v", z)
	}
}

// A device replaced years ago is retired even when the Account itself is fresh, and
// one that went quiet recently leads the retired ones.
func TestFreshnessOrdersActiveThenRetired(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		steps("Old iPhone", "2019-03-01T12:00:00Z"),
		steps("Apple Watch", "2026-09-21T12:00:00Z"),
		steps("iPhone", "2026-09-10T12:00:00Z"),
		steps("Zepp Life", "2026-05-01T12:00:00Z"),
	)

	f := freshness(t, e, acc, "2026-09-22T10:00:00Z")
	want := []string{"Apple Watch", "iPhone", "Zepp Life", "Old iPhone"}
	if len(f.Sources) != len(want) {
		t.Fatalf("sources: %+v", f.Sources)
	}
	for i, name := range want {
		if f.Sources[i].Source != name {
			t.Fatalf("order at %d: got %s, want %s (%+v)", i, f.Sources[i].Source, name, f.Sources)
		}
	}
	if !source(t, f, "Old iPhone").Retired {
		t.Fatal("a device replaced in 2019 is retired")
	}
	if f.AgeDays != 1 {
		t.Fatalf("age %d, want 1", f.AgeDays)
	}
}

// Sleep is dated by its Night: an interval that starts at 23:30 on the 20th belongs
// to the morning of the 21st, which is the bar the sleep Panel draws it on.
func TestFreshnessDatesSleepByItsNight(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc, steps("Apple Watch", "2026-09-20T12:00:00Z"))
	seedSleep(t, models, acc, "Apple Watch", "2026-09-20T23:30:00Z", "2026-09-21T06:30:00Z")

	f := freshness(t, e, acc, "2026-09-22T10:00:00Z")
	if f.LastDay != "2026-09-21" {
		t.Fatalf("last day %s, want the Night 2026-09-21", f.LastDay)
	}
	if len(f.Sources) != 1 || f.Sources[0].LastDay != "2026-09-21" {
		t.Fatalf("one Source across both families: %+v", f.Sources)
	}
}

// Freshness and a Panel agree on which day the data stops: the day is date(start_at)
// in UTC, the same bucket the engine draws.
func TestFreshnessLastDayIsTheEnginesDayBucket(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc, steps("Apple Watch", "2026-09-12T23:59:59Z"))

	f := freshness(t, e, acc, "2026-09-22T10:00:00Z")
	if f.LastDay != "2026-09-12" || f.AgeDays != 10 {
		t.Fatalf("got %s aged %d", f.LastDay, f.AgeDays)
	}
}
