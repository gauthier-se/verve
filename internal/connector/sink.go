package connector

import (
	"context"
	"fmt"
	"io"

	"github.com/gauthier-se/verve/internal/data"
)

// Sink is what a Connector writes through: it takes canonical rows one at a time,
// batches them, and hands back the Report at the end.
//
// It exists because the rules between "I parsed a row" and "the row is stored" are
// the contract's, not the format's. An Exclusion is refused before the batch and
// counted rather than swallowed (ADR 0033); rows flush at BatchSize to bound memory
// and keep the WAL small; every write tallies into the Report; the Import record is
// written last. Each of those was previously a thing every Connector had to
// remember on its own, spelled slightly differently in each, and a Connector that
// forgot one would have failed silently: dropping data without saying so is the
// exact failure mode this contract is built to avoid.
//
// A Connector is therefore "read the file and offer rows". Everything else is here.
type Sink struct {
	store     Store
	accountID int64
	opts      Options
	report    Report

	measurements []data.Measurement
	unmapped     []data.UnmappedRecord
	states       []data.State
}

// NewSink returns a Sink writing to store on behalf of one Connector run. name and
// sourceFile are recorded with the Import and carried on the Report.
func NewSink(store Store, accountID int64, name, sourceFile string, opts Options) *Sink {
	return &Sink{
		store:     store,
		accountID: accountID,
		opts:      opts,
		report: Report{
			Connector:     name,
			SourceFile:    sourceFile,
			PerMetric:     map[string]Tally{},
			UnmappedTypes: map[string]int{},
			PerState:      map[string]Tally{},
			PerActivity:   map[string]Tally{},
			Ignored:       map[string]int{},
		},
		measurements: make([]data.Measurement, 0, BatchSize),
		unmapped:     make([]data.UnmappedRecord, 0, BatchSize),
		states:       make([]data.State, 0, BatchSize),
	}
}

// AccountID is the Account this run writes for, so a Connector building a row does
// not have to carry it separately.
func (s *Sink) AccountID() int64 { return s.accountID }

// ArtifactsDir is where a Connector copies an artifact it references (ADR 0004).
func (s *Sink) ArtifactsDir() string { return s.opts.ArtifactsDir }

// Measurement offers one Measurement.
//
// A Metric the Account has excluded is counted and dropped here, after the
// Connector has classified the row and before it reaches a batch: an Exclusion
// names a Catalog Metric, so there is nothing to check until the row has one. A
// refused row deliberately does not fall through to the Unmapped bin, because that
// bin exists so no source data is lost to a Catalog gap (ADR 0002), and routing a
// refusal there would keep, row for row, exactly what was asked to be dropped
// (ADR 0033).
func (s *Sink) Measurement(ctx context.Context, m data.Measurement) error {
	if m.AccountID == 0 {
		m.AccountID = s.accountID
	}
	if s.opts.Exclusions.Excludes(m.Metric, m.StartAt) {
		t := s.report.PerMetric[m.Metric]
		t.Excluded++
		s.report.PerMetric[m.Metric] = t
		s.report.Excluded++
		return nil
	}
	s.measurements = append(s.measurements, m)
	if len(s.measurements) >= BatchSize {
		return s.flushMeasurements(ctx)
	}
	return nil
}

// Unmapped offers one row for the Unmapped bin: a record the Catalog cannot map,
// kept verbatim and inspectable rather than discarded (ADR 0002).
func (s *Sink) Unmapped(ctx context.Context, u data.UnmappedRecord) error {
	if u.AccountID == 0 {
		u.AccountID = s.accountID
	}
	s.unmapped = append(s.unmapped, u)
	if len(s.unmapped) >= BatchSize {
		return s.flushUnmapped(ctx)
	}
	return nil
}

// State offers one State: a categorical value holding over an interval.
//
// Exclusions do not apply: one names a Catalog Metric, and nothing but a
// Measurement carries one yet (ADR 0033).
func (s *Sink) State(ctx context.Context, st data.State) error {
	if st.AccountID == 0 {
		st.AccountID = s.accountID
	}
	s.states = append(s.states, st)
	if len(s.states) >= BatchSize {
		return s.flushStates(ctx)
	}
	return nil
}

// Workout writes one Session with the summary stats and Routes it carries, as one
// unit. Unlike the scalar families it is not batched: a workout is written when
// the Connector finishes reading it, because everything attached to it needs the
// ID the write assigns.
func (s *Sink) Workout(ctx context.Context, sess *data.Session, stats []data.SessionStat, routes []data.Route) error {
	if sess.AccountID == 0 {
		sess.AccountID = s.accountID
	}
	wrote, err := s.store.InsertWorkout(ctx, sess, stats, routes)
	if err != nil {
		return fmt.Errorf("connector: insert workout: %w", err)
	}

	t := s.report.PerActivity[sess.ActivityType]
	if wrote.SessionAdded {
		t.Added++
		s.report.SessionsAdded++
	} else {
		t.Skipped++
		s.report.SessionsSkipped++
	}
	s.report.PerActivity[sess.ActivityType] = t

	for _, added := range wrote.RoutesAdded {
		if added {
			s.report.RoutesAdded++
		} else {
			s.report.RoutesSkipped++
		}
	}
	return nil
}

// Ignored counts a file an archive holds that is not a health record at all:
// account settings, subscriptions, a README. They are not Unmapped, because that
// bin exists so no *source data* is lost to a Catalog gap and filing a notification
// preference there would be filing a receipt as a measurement. They are still
// counted, because an import that drops something silently is the failure mode
// every part of this contract avoids.
func (s *Sink) Ignored(where string) { s.report.Ignored[where]++ }

// Report is the run so far. Callers read it through Close; this is for a Connector
// that has to report something the Sink does not own.
func (s *Sink) Report() *Report { return &s.report }

// Close flushes every pending batch, records the Import, and returns the Report.
//
// The Import row is written last and outside the batches' transactions, so a crash
// mid-import leaves the rows it did write and no record claiming more. Re-running
// is idempotent by content key (ADR 0006), which is what makes that safe.
func (s *Sink) Close(ctx context.Context) (Report, error) {
	if err := s.flushMeasurements(ctx); err != nil {
		return Report{}, err
	}
	if err := s.flushUnmapped(ctx); err != nil {
		return Report{}, err
	}
	if err := s.flushStates(ctx); err != nil {
		return Report{}, err
	}

	imp := &data.Import{
		AccountID:     s.accountID,
		Connector:     s.report.Connector,
		SourceFile:    s.report.SourceFile,
		AddedCount:    s.report.Added,
		SkippedCount:  s.report.Skipped,
		UnmappedCount: s.report.Unmapped,
	}
	if err := s.store.Record(ctx, imp); err != nil {
		return Report{}, fmt.Errorf("connector: record import: %w", err)
	}
	return s.report, nil
}

func (s *Sink) flushMeasurements(ctx context.Context) error {
	if len(s.measurements) == 0 {
		return nil
	}
	mask, err := s.store.InsertBatch(ctx, s.measurements)
	if err != nil {
		return fmt.Errorf("connector: insert measurements: %w", err)
	}
	for i, added := range mask {
		t := s.report.PerMetric[s.measurements[i].Metric]
		if added {
			t.Added++
			s.report.Added++
		} else {
			t.Skipped++
			s.report.Skipped++
		}
		s.report.PerMetric[s.measurements[i].Metric] = t
	}
	s.measurements = s.measurements[:0]
	return nil
}

func (s *Sink) flushUnmapped(ctx context.Context) error {
	if len(s.unmapped) == 0 {
		return nil
	}
	mask, err := s.store.InsertUnmappedBatch(ctx, s.unmapped)
	if err != nil {
		return fmt.Errorf("connector: insert unmapped: %w", err)
	}
	for i, added := range mask {
		if added {
			s.report.Unmapped++
			s.report.UnmappedTypes[s.unmapped[i].SourceType]++
		}
	}
	s.unmapped = s.unmapped[:0]
	return nil
}

func (s *Sink) flushStates(ctx context.Context) error {
	if len(s.states) == 0 {
		return nil
	}
	mask, err := s.store.InsertStateBatch(ctx, s.states)
	if err != nil {
		return fmt.Errorf("connector: insert states: %w", err)
	}
	for i, added := range mask {
		t := s.report.PerState[s.states[i].Kind]
		if added {
			t.Added++
			s.report.StatesAdded++
		} else {
			t.Skipped++
			s.report.StatesSkipped++
		}
		s.report.PerState[s.states[i].Kind] = t
	}
	s.states = s.states[:0]
	return nil
}

// ProgressCounter reports read progress against a total, across as many readers as
// one import happens to open.
//
// A streamed export is one reader and one declared size; an archive walk is a
// reader per entry against the summed size of the entries it will read. Both are
// the same question, so both ask it here rather than each keeping its own counter
// and calling Progress by hand. Only the ratio is used, so the unit is the
// Connector's business. A nil Progress makes Wrap the identity, which is the CLI
// path.
type ProgressCounter struct {
	read     int64
	total    int64
	progress Progress
}

// NewProgressCounter returns a counter over total. A nil Progress is inert.
func NewProgressCounter(total int64, p Progress) *ProgressCounter {
	return &ProgressCounter{total: total, progress: p}
}

// Wrap returns r, counting every byte read from it against the total.
func (c *ProgressCounter) Wrap(r io.Reader) io.Reader {
	if c == nil || c.progress == nil {
		return r
	}
	return &progressReader{r: r, counter: c}
}

type progressReader struct {
	r       io.Reader
	counter *ProgressCounter
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.counter.read += int64(n)
	p.counter.progress(p.counter.read, p.counter.total)
	return n, err
}
