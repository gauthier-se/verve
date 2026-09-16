Status: done

# 01: estimate, web: say why the better basis is unavailable

## What

The smaller half of the PRD, and the one that needs no decision: when the cascade
falls through, carry the reason and show it.

### `internal/estimate/estimate.go`

- **`Shortfall`**, a struct describing one unmet requirement, and `Expenditure`
  gains `Shortfall *Shortfall` populated only when a *better* basis was skipped:

  ```go
  // Shortfall is why the cascade did not stop at a better basis: the evidence the
  // observed basis needed, and what the window actually held. It is populated on the
  // figure that was served, not on the one that was not, because the caller is
  // rendering the former and owes the Account an explanation for it.
  type Shortfall struct {
      Basis          Basis  `json:"basis"`            // the basis that was skipped
      IntakeDays     int    `json:"intake_days"`      // days with logged intake in the window
      IntakeDaysNeed int    `json:"intake_days_need"` // ceil(minIntakeCoverage * windowDays)
      MassDays       int    `json:"mass_days"`
      MassDaysNeed   int    `json:"mass_days_need"`
      LastIntakeDay  string `json:"last_intake_day,omitempty"` // YYYY-MM-DD, "" if never
  }
  ```

- **`observed` returns the shortfall instead of a bare `false`.** It already computes
  `len(intake)` and `len(mass)` before the threshold check; the change is to return
  them rather than discard them. `LastIntakeDay` is one extra read, and it is the
  field that turns a diagnosis into an instruction — "32% coverage" tells an Account
  nothing, "your last food log was 27 August" tells it exactly what to do.

- **`Expenditure` threads it** onto whichever basis it ends up returning. A
  `predicted` result carries the shortfall of *both* better bases, so the field is a
  slice or the struct names only the best one skipped; the second is simpler and
  loses nothing, since an Account fixing the observed basis never sees `recorded`
  either.

Existing behaviour is untouched: when `observed` succeeds the field is nil, which is
every Account the current tests cover.

### `web/src/lib/types.ts`, `web/src/components/plan-page.tsx`

`Expenditure` gains the optional field, and `ExpenditureCard` renders one extra
sentence when it is present. The existing `recorded` copy is good and stays; this
sits after it, and it is the specific half:

> This is a fallback. Your observed figure needs 20 of the last 28 days logged and
> 10 weigh-ins; this window has 9 and 9. Your last food log was 27 August.

Numbers from the payload, never recomputed client-side — the same rule `summary`
and `days` already follow.

## Tests

- **`TestShortfallNamesWhatWasMissing`**: an Account with intake on 9 of 28 days and
  9 weigh-ins falls to `recorded` and carries `Shortfall{Basis: observed,
  IntakeDays: 9, IntakeDaysNeed: 20, MassDays: 9, MassDaysNeed: 10}`.
- **`TestShortfallAbsentWhenObservedSucceeds`**: a dense Account gets nil, which is
  the guard that this cannot start appearing on healthy accounts.
- **`TestShortfallLastIntakeDayEmptyWhenNeverLogged`**: an Account that has never
  logged food gets `""` rather than a zero date, so the copy can omit the sentence
  rather than print 0001-01-01.

## Comments

**Implemented.** Three departures from the spec, all small:

- **`observed` returns `(Expenditure, *Shortfall, error)`** rather than keeping its
  `bool`. A zero `Basis` is the "did not produce" sentinel, which avoids a fourth
  return value and keeps the one case that is *not* a coverage failure honest: when
  coverage is met and the fit still fails (every reading on one day), the shortfall is
  nil and the cascade falls through unexplained, because handing back counts that met
  their thresholds would blame the thresholds for something else.
- **The shortfall rides the predicted basis too**, not only the recorded one. An
  Account with no devices lands there and has exactly the same thing to fix.
- **`intakeDaysNeeded` is a written-out `20`**, because Go will not truncate an
  untyped float in a const. `TestIntakeDaysNeededMatchesTheCoverageRule` pins it to
  `ceil(minIntakeCoverage x observedWindowDays)` so it cannot drift from the rule that
  actually gates the cascade.

`lastIntakeDay` reads through the query engine over a 365-day lookback rather than
touching SQL, so the Source election and the Manual overlay are not reimplemented here
— and the lookback is the point: the reference Account's last log is 20 days back,
inside the window, but an Account that stopped three months ago must still get a date.
`TestLastIntakeDayLooksPastTheWindow` covers the second case.

**`Expenditure`, `Shortfall` and `MeasuredRate` are now in the contract registry.**
They were not pinned, which is how a new field could have gone out undeclared; the
`Series` types have been pinned since ADR 0037 and these are the same kind of contract.
