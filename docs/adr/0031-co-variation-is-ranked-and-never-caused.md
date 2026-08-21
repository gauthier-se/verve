# Co-variation is a rank correlation over the Pins, and never a cause

## Context

The README makes a specific claim about what Apple Health is bad at: it holds
years of data from a dozen sources and will not tell you whether any of it moves
together. Cross-metric Panels (ADR 0020) answer that by eye — two curves, two
axes, look at them — which works for a pair you already suspect and not at all
for the question "what in here moves with what". That question needs every pair
computed, not one pair drawn.

Three things have to be settled before it can be answered honestly.

**Which measure.** Health series are neither normal nor free of outliers: one
holiday triples a step count, one bad scale reading moves a body mass by three
kilos, one week of flu empties four Metrics at once. Pearson's *r* lets a single
bucket write the answer, and a coefficient that swings from 0.2 to 0.7 because of
one Christmas is worse than no coefficient at all.

**Which Metrics.** Pairing the whole Catalog is quadratic in a set the Account
did not choose, and produces a wall of coefficients between things nobody was
asking about. Something has to name the inputs.

**What the interface is allowed to say.** This is the screen where a health app
usually stops being honest. "Your sleep is hurting your recovery" is one CSS
class and one verb away from what the data supports, and Verve has already
committed, twice, to not saying it: it does not colour a change good or bad
(ADR 0019), and it does not interpret (README, *Not planned*). A correlation
page is where that commitment is actually tested.

## Decision

**The measure is Spearman's ρ — a correlation of ranks.** Ties share their
average rank; zero variance on either side yields 0 rather than NaN, so nothing
travels into the JSON that the interface cannot render. Ranking bounds every
bucket's influence to one position, and it keeps a relationship that is monotone
but not linear — sleep against resting heart rate — legible.

**The inputs are the Pins** (ADR 0025). "How do I put a Metric on this page" has
exactly one answer: pin it. No second list to curate, no per-page selection to
persist, and the sidebar already shows what is on the page. The read is capped at
8 Metrics, dropped from the tail in Pin order, because 8 is already 56 ordered
pairs and a wider matrix stops being readable well before it stops being
affordable.

**A pair below a shared-bucket threshold is computed, shown, and marked
not-ranked.** The threshold is 60 % of the window's own buckets with a floor of
8, resolved server-side and returned with the answer so the interface states the
real number instead of a hard-coded one. It scales with the range, so a
three-month window is not silently unrankable. Unranked pairs sort below every
ranked one whatever their coefficient: an unranked ρ is not a weaker answer, it
is not an answer.

**A Lag carries its grain.** The three presets are `same`, `next_day` and
`next_week`, and each fixes both the shift and the bucket the pair is read at,
because they are one choice — "the next morning" is a day-grain question. A
non-zero Lag makes the matrix directional, which is the point of asking for one.

**Hue encodes direction, never valence.** Together takes the accent, opposite
takes the cool chart colour, alpha carries strength. No red, no green, no arrow
pointing at "better". The words follow: "moves together", "leans opposite", "no
clear link" — and the caveat paragraph above the matrix says in prose that a
relationship here is not a cause.

**Everything is computed server-side**, including the fitted line on the scatter
(ADR 0012). The client draws statistics; it does not compute them.

## Considered Options

- **Spearman's ρ (chosen).** Robust to the outliers this data actually has,
  legible on monotone non-linear relationships.
- **Pearson's r.** Cheaper to explain, and wrong for the shape of the data. A
  coefficient that a single Christmas can move is a number that will be believed
  and should not be. Rejected.
- **Kendall's τ.** Defensible, better small-sample behaviour, and nobody outside
  statistics has heard of it. ρ costs one sort and reads as "correlation" to the
  person looking at it. Rejected.
- **A p-value beside each coefficient.** It looks like rigour and it is a trap:
  56 pairs is 56 tests, so significance at 0.05 is expected several times over by
  chance alone, and reporting it invites exactly the reading this page refuses.
  The shared-bucket count is the honest evidence figure and it is shown instead.
  Rejected.
- **Pairing the whole Catalog.** Quadratic in a set the Account never chose.
  Rejected in favour of the Pins.
- **A per-page Metric selection, persisted.** A second list meaning the same
  thing as the Pins, that can disagree with them. Rejected.
- **Dropping unranked pairs from the matrix.** A blank cell reads as "no
  relationship" when it means "not enough overlap", which is a different and more
  interesting fact — usually that one of the two Metrics has a hole in it.
  Rejected.
- **A fixed threshold of 40 shared buckets.** Right for a year of weekly buckets
  and wrong for every other window: a three-month range would rank nothing and
  say nothing about why. Rejected in favour of a proportional one, reported.
- **Colouring by valence — green for "good", red for "bad".** Requires knowing
  which direction is good for a Metric. Verve does not know that for body mass,
  and cannot know it for anyone else's goals. Rejected, permanently.

## Consequences

- `GET /v1/covary` answers the whole page: pairs, ranking, threshold, the
  strongest pair's shared points and its fitted line. One call, because every
  figure on the page has to agree about one window and one grain.
- A Metric that is pinned but absent from the matrix is **named**, with a reason
  ("not in the catalog", "no data in this window", "beyond the 8 metrics this
  page pairs"). A Pin the Account cannot find on this page is a bug until the
  page says why.
- The diagonal is a dot rather than 1.00: a Metric against itself is not a
  question, and filling it would put the strongest colour in the grid on the one
  cell carrying no information.
- `|ρ| < 0.15` prints no number, keeping only its tint. Two decimals on a
  coefficient that weak claim a precision the relationship does not have.
- With no Lag, A×B and B×A are the same number, so the ranked list keeps one of
  each pair; with a Lag they are two questions and both stay.
- The read is O(n²) in pinned Metrics over one window of Series. It is bounded by
  the cap, and each Metric is read exactly once.
