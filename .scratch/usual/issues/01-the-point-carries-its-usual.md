Status: done

# 01: query, api: the Usual on the Series

## What

### `internal/query`: the wire shape

`Point` gains one optional field, beside `trend`:

```go
// Usual is where this bucket's own past sits: the p25 to p75 of the Metric's
// values over the buckets just before it, the bucket itself excluded (usual.go).
// Nil when the window holds too few values to describe anything, on a bucket
// without a reading, and on the bucket in progress of an accumulating Metric.
Usual *Usual `json:"usual,omitempty"`
```

```go
// Usual is a descriptive band, never a norm: it compares the owner to themselves.
type Usual struct {
	Low  float64 `json:"low"`  // p25, linear interpolation (Hyndman and Fan type 7)
	High float64 `json:"high"` // p75
	N    int     `json:"n"`    // values behind it, the evidence, as Count is for a bucket
}
```

### Where it is computed

`Series` keeps its name and becomes a thin wrapper: the current body is the
unexported `series`, and `Series` calls `withUsual` (usual.go) only when the new
`Request.Usual` is set. Opt-in rather than always on, because `Series` has
thirteen internal callers (goal, estimate, covary, ledger, day, band, lastday,
export...) that never draw a band and would each pay a second read.

`withUsual` reads `series` once more over `[start(From) - window, From)`, extended
to the end of the bucket holding `From` when the window starts mid-bucket, and
takes each Point's reference from that read and the Series' own points, the prior
read winning where both hold a bucket (it has it whole).

Constants live in `usualReferences`, keyed by grain: day `{window: 28, min: 14}`,
week `{window: 12, min: 8}`, month absent.

The `maxPoints` concern did not materialise: the prior read is its own short
request, not an extension of the main one.

### Whole buckets only (changed from the first draft)

The first draft cleared only the in-progress bucket of `sum` Metrics. Building it
showed the real case is wider: presets end at today midnight, so "today" rarely
appears, but a window starting mid-week cuts its first week, which then read
against whole weeks *and* leaked into the reference of the weeks after it. The
rule is now one for every Metric: **a bucket the window only partly covers carries
no Usual.** Sleep at week grain is summed too, so the aggregation-based rule would
have missed it.

The API still clears the bucket holding now (`unfinished` in handlers.go), for a
custom range that runs past today.

### `internal/api`

`/v1/series` sets `Usual` for a lone Metric, with or without a Baseline; a
multi-Metric read does not ask for it. `Compare` clears it on the Baseline
request, so the Baseline's Usual is never read. `Usual` is in `contract_test.go`
and `types.ts`.

## Tests

`internal/query/usual_test.go`: quartiles of the days before, the bucket excluded;
gaps absent and the minimum at 14 and 13; week grain and its minimum; none at month
grain; whole weeks only, with the cut week read whole in later references; the
losing Source ignored; a derived Metric; sleep over Nights; `Compare` reads no
Usual for the Baseline; `quantile` on hand-checked type 7 cases.

`internal/api/usualhandlers_test.go`: a lone Metric carries it with and without a
Baseline; a multi-Metric read carries none; today carries none on a custom range.

## Docs

- ADR 0046, "The usual is the owner's own past": rolling strict-past window, pooled
  not per weekday (with the measurement), day and week only, descriptive never
  graded.
- CONTEXT.md entry **Usual**, with the _Avoid_ list from the PRD.
