package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule declares a recurring job. It is declared in internal/app, and only a Worker enqueues it.
type Schedule struct {
	// Name identifies the schedule across restarts.
	Name string
	// Kind and Payload describe the jobs to enqueue.
	Kind string
	// Every is the time between two runs. A schedule without Every and Cron is disabled.
	Every time.Duration
	// Cron is a cron expression with the standard five fields. It takes precedence over Every.
	Cron    string
	Payload json.RawMessage
}

// enabled reports whether s has times to run at.
func (s Schedule) enabled() bool {
	return s.Every > 0 || s.Cron != ""
}

// Next returns the first run after now. An interval is real time, and a cron expression is read in
// the location of now. Next fails on an expression that is invalid or that no date matches.
func (s Schedule) Next(now time.Time) (time.Time, error) {
	if s.Cron == "" {
		return now.Add(s.Every), nil
	}

	expression, err := parseCron(s.Cron)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cron expression %q: %w", s.Cron, err)
	}

	next := expression.Next(now)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("cron expression %q never runs", s.Cron)
	}

	return next, nil
}

// CheckCron tells why a Schedule would refuse the cron expression, or returns nil. The error does
// not quote the expression.
func CheckCron(expression string) error {
	parsed, err := parseCron(expression)
	if err != nil {
		return err
	}

	// A date matches within the 5 years that the parser searches, from any start: February 29 too.
	if parsed.Next(time.Unix(0, 0).UTC()).IsZero() {
		return errors.New("never runs")
	}

	return nil
}

// parseCron parses a cron expression with the standard five fields.
func parseCron(expression string) (cron.Schedule, error) {
	// A TZ= prefix would escape the location of now, and the parser panics on one without a space.
	if strings.Contains(expression, "=") {
		return nil, errors.New("time zones are not supported")
	}

	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(expression)
}
