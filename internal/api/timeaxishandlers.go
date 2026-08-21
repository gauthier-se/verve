package api

import (
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/timeaxis"
)

// windowView is a resolved half-open window as the client reads it: the inclusive
// first day, the exclusive bound, and the whole-day span between them. To is the
// half-open bound the engine queries on; Last is the day a label should print, so
// the interface never has to subtract a day from a bound it did not compute.
type windowView struct {
	From string `json:"from"`
	To   string `json:"to"`
	Last string `json:"last"`
	Days int    `json:"days"`
}

// timeAxisView is one resolved time axis: the current window, the bucket every
// Series on it will be drawn at, and the Baseline window when comparison is on.
type timeAxisView struct {
	Range    windowView  `json:"range"`
	Bucket   string      `json:"bucket"`
	Baseline *windowView `json:"baseline,omitempty"`
}

// handleTimeAxis resolves a set of time-axis tokens without touching any data:
// the same timeaxis.Resolve every Series query runs, exposed on its own so the
// interface can *print* the axis it is drawing on — the resolved dates under a
// Dashboard header, the axis marks under a Metric chart, the sentence naming the
// compared period.
//
// It exists because those labels are dates, and a date is exactly what the client
// must not compute (ADR 0012): a "1y" range that the SPA resolves with its own
// clock, in the browser's own zone, is a label that disagrees with the buckets
// beside it twice a year and near midnight. One module owns the boundaries; this
// is the door onto it. The endpoint is pure — no Account data is read — but it
// stays behind the session like every other /v1 route.
func (s *Server) handleTimeAxis(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	v := NewValidator()

	resolved, err := timeaxis.Resolve(timeaxis.Tokens{
		RangePreset:  qs.Get("range_preset"),
		RangeFrom:    optionalParam(qs, "range_from"),
		RangeTo:      optionalParam(qs, "range_to"),
		BaselineRule: qs.Get("baseline_rule"),
		BaselineFrom: optionalParam(qs, "baseline_from"),
		BaselineTo:   optionalParam(qs, "baseline_to"),
		Bucket:       optionalParam(qs, "bucket"),
	}, time.Now())
	if inv, ok := err.(timeaxis.Invalid); ok {
		for field, msg := range inv {
			v.AddError(field, msg)
		}
	} else if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	view := timeAxisView{Range: toWindowView(resolved.Current), Bucket: string(resolved.Bucket)}
	if resolved.Baseline != nil {
		base := toWindowView(*resolved.Baseline)
		view.Baseline = &base
	}

	if err := writeJSON(w, http.StatusOK, envelope{"time_axis": view}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// toWindowView projects a resolved window to its API shape, naming both its
// half-open bound and its last day.
func toWindowView(win timeaxis.Window) windowView {
	from, to := win.From.UTC(), win.To.UTC()
	return windowView{
		From: from.Format(dayLayout),
		To:   to.Format(dayLayout),
		Last: to.AddDate(0, 0, -1).Format(dayLayout),
		Days: int(to.Sub(from).Hours() / 24),
	}
}
