package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gauthier-se/verve/internal/archive"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// The two ways data leaves Verve (ADR 0039), which are two grains and
// deliberately nothing between them: a Series as CSV, which is a download of a
// read, and an Archive, which is a transfer of the store.
//
// They are the grown-up version of handleDownloadRoute, whose comment already
// states the thesis: "your data is yours" does not survive a server that only
// ever returns its own simplified version.

// csvHeader is the CSV contract, pinned by a golden test. bucket_end is the
// bucket's exclusive end, so a reader can reconstruct the grid without knowing
// Verve's boundary rules; count is how many readings are behind the figure.
var csvHeader = []string{"bucket_start", "bucket_end", "metric", "unit", "aggregation", "value", "count"}

// handleExportArchive streams this Account's whole canonical data as a zip.
//
// It is a plain streamed read and not an import-style job (importjob.go):
// nothing is written, so there is no state to poll and cancelling is closing the
// connection. There is no rate limiter either, and that is a decision rather than
// an omission: the only limiter here guards login brute-forcing, and an
// authenticated Account reading its own rows twice in a row is not an attack.
func (s *Server) handleExportArchive(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	now := time.Now()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("verve-%s.zip", now.UTC().Format("2006-01-02"))))
	// No Content-Length is set: the size is not known when the headers go out,
	// which is the trade a streamed export accepts and why the browser shows no
	// progress. net/http still infers one for a body small enough to fit its
	// write buffer, so a nearly empty Account happens to get a determinate
	// download; nothing should depend on that.

	err := s.models.Tx(r.Context(), func(tx data.Models) error {
		// One read transaction for the whole export, so an import running beside
		// it cannot land half of itself in the middle of the file (ADR 0038).
		src := archive.NewModelSource(tx, accountID, s.artifactsDir)
		_, err := archive.Write(r.Context(), w, src, s.version, now)
		return err
	})
	if err != nil {
		// The 200 went out with the first byte, so there is no status left to
		// change. Returning without finalizing the zip leaves it with no central
		// directory, which every unzip tool refuses loudly: a file that cannot be
		// opened is a better outcome than one that opens and is short.
		s.logger.Error("archive export failed mid-stream", "err", err,
			"account", accountID, "uri", r.URL.RequestURI())
	}
}

// handleSeriesCSV answers the same question /v1/series answers, as a file.
//
// It takes exactly the same parameters and runs them through the same read
// module, so the file and the curve cannot disagree (ADR 0037). What it refuses
// is a Baseline: a caller who asked for a comparison and received one window
// would believe the file holds two.
func (s *Server) handleSeriesCSV(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	v := NewValidator()
	if rule := r.URL.Query().Get("baseline_rule"); rule != "" && rule != "none" {
		v.AddError("baseline_rule",
			"a CSV holds one window: download the comparison window as its own file")
	}
	metrics, resolved, ok := s.seriesParams(w, r, v)
	if !ok {
		return
	}

	list := make([]query.Series, 0, len(metrics))
	for _, metric := range metrics {
		series, err := s.engine.Series(r.Context(), query.Request{
			AccountID: accountID, Metric: metric,
			From: resolved.Current.From, To: resolved.Current.To, Bucket: resolved.Bucket,
		})
		if err != nil {
			s.respondSeriesError(w, r, err)
			return
		}
		list = append(list, series)
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", csvFilename(metrics, resolved.Current)))

	cw := csv.NewWriter(w)
	cw.UseCRLF = true // RFC 4180
	if err := cw.Write(csvHeader); err != nil {
		s.logger.Error("series csv failed mid-stream", "err", err, "account", accountID)
		return
	}
	for _, series := range list {
		for _, point := range series.Points {
			for _, row := range csvRows(series, point, resolved.Bucket) {
				if err := cw.Write(row); err != nil {
					s.logger.Error("series csv failed mid-stream", "err", err, "account", accountID)
					return
				}
			}
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		s.logger.Error("series csv failed mid-stream", "err", err, "account", accountID)
	}
}

// csvRows renders one Point.
//
// A day with nothing recorded is a day with no row: Series is sparse, and the
// file says what the read said. Where a Point does arrive marked as a gap, its
// value cell is empty and never a zero (ADR 0014). Nothing this endpoint serves
// produces one today, since a gap Point comes from the dense reads (the History
// band, the ordinal alignment of a Baseline) and the CSV refuses a Baseline; the
// branch stays because the day a read path is made dense, the alternative is a
// zero written silently, and in a spreadsheet a zero is a day you did not move.
//
// A Metric read as durations per state (sleep, ADR 0027) has no single value, so
// it emits one row per state, named `sleep.asleep_rem`. That keeps the file one
// shape rather than growing a second header for one family.
func csvRows(series query.Series, point query.Point, bucket timeaxis.Bucket) [][]string {
	end, ok := bucket.Shift(point.Bucket, 1)
	if !ok {
		end = ""
	}
	base := []string{point.Bucket, end, series.Metric, series.Unit, string(series.Aggregation)}

	if len(point.States) > 0 {
		states := make([]string, 0, len(point.States))
		for state := range point.States {
			states = append(states, state)
		}
		sort.Strings(states)

		rows := make([][]string, 0, len(states))
		for _, state := range states {
			row := append([]string{}, base...)
			row[2] = series.Metric + "." + state
			rows = append(rows, append(row, csvNumber(point.States[state]), strconv.Itoa(point.Count)))
		}
		return rows
	}

	value := csvNumber(point.Value)
	if point.Gap {
		value = ""
	}
	return [][]string{append(base, value, strconv.Itoa(point.Count))}
}

// csvNumber renders a figure for a spreadsheet: a plain dot decimal, never
// grouped and never comma-decimal, the same rule the clipboard path states.
func csvNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// csvFilename names the download after what it holds: one Metric names itself,
// several are a series, and both carry the window.
//
// The window is half-open [From, To), and the name says the last day the file
// actually covers rather than the exclusive bound: a file called
// verve-steps-2026-06-01-2026-07-01.csv for a June download would be read as
// holding a day it does not.
func csvFilename(metrics []string, window timeaxis.Window) string {
	what := "series"
	if len(metrics) == 1 {
		what = metrics[0]
	}
	return fmt.Sprintf("verve-%s-%s-%s.csv", what,
		window.From.UTC().Format("2006-01-02"), window.To.UTC().AddDate(0, 0, -1).Format("2006-01-02"))
}
