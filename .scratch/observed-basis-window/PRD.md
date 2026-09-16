# PRD: the observed basis expires quietly, and the fallback is the figure it was built to replace

## Goal

ADR 0023 is unambiguous: `observed` outranks `recorded` because it is grounded in an
outcome rather than a model, and it measured `recorded` as wrong by ~38% — roughly
971 kcal/day — on the reference Account. The Estimates engine implements that
cascade and the Plan page names the basis beside the figure, as the ADR requires.

What neither anticipated is what happens when the Account simply stops logging food
for three weeks. The observed basis needs 70% intake coverage and 10 mass readings
over **the last 28 calendar days**. Nothing else. The moment the window slides past
the last food log, the basis disappears and the page falls through to `recorded` —
the device figure the ADR exists to distrust — with no statement that anything
changed.

This is the reference Account's state today, 16 September 2026:

| | Needed | Actual |
| --- | --- | --- |
| Intake coverage over 28 days | ≥ 70% | **32%** (9 days) |
| Mass readings over 28 days | ≥ 10 | **9** |

Both thresholds fail by a small margin, the cascade falls through, and the headline
reads ~3 889 kcal/day. Meanwhile the Account holds **44 consecutive days of dense
logging** — 44/44 days of intake, 40 weigh-ins — that ended 20 days ago. The
evidence has not gone anywhere. The window moved.

The consequence is not a missing number, which would be honest. It is a *worse*
number presented with the same confidence as the better one, at the exact moment the
Account is most likely to be looking: after a break, deciding what to eat next.

## The two things wrong, which are separable

### 1. The fall-through is silent

`Expenditure` carries detail fields for the basis that produced the figure and
nothing about the bases that were skipped. The page therefore cannot say why the
good figure went away, and the copy it does show for `recorded` — which is well
written, and already warns about the ~970 kcal overstatement — reads as a general
caveat rather than as "this is a downgrade, and here is what would undo it".

An Account that sees the headline jump by ~700 kcal has no way to learn that the
cause is nine missing food logs.

### 2. The window is a calendar window, not an evidence window

`observed` reads `[now-28d, now)` and gives up. It never asks whether a qualifying
28-day window exists slightly further back. On this Account one does, ending at the
last food log:

| Window | Coverage | Slope | Observed TDEE |
| --- | --- | --- | --- |
| 31 Jul – 27 Aug 2026 | 28/28 intake days, 24 weigh-ins | −119 g/day (−0.83 kg/wk) | **3 185 kcal** |
| what the page shows today | — | — | 3 889 kcal (`recorded`) |

A 704 kcal/day difference, from data already in the database.

## The decision this needs

**Should a dated observed figure outrank a current recorded one?**

Yes, and the ADR already contains the argument. `observed` wins because it is
grounded in an outcome; `recorded` loses because it is a device claim measured as
~38% wrong. Neither property is about freshness. A three-week-old figure computed
from what the body actually did is still grounded in an outcome; a figure computed
this morning from what the Watch claims is still a device claim. Ranking recency
above groundedness inverts the ADR's own reasoning.

**With three constraints**, because a stale figure is not a free win:

- **It is dated on screen.** The window's real bounds go in the payload
  (`window_from`, `window_to`) and the page says them. "3 185 kcal, from 31 July to
  27 August" is a different claim from "3 185 kcal", and only the first is true.
- **The lookback is capped.** A qualifying window from 2023 describes a body that no
  longer exists. A cap — 90 days from `now` is the obvious first value, three times
  the window — bounds how stale the answer may be, and past it the cascade falls
  through as it does now.
- **Stale means stale for targets too.** The Plan page builds a calorie target on
  this figure. A target derived from a dated basis must carry the date wherever it
  is shown, not only on the expenditure card.

**The counter-argument, recorded honestly:** behaviour and metabolism change, and a
figure from a cut the Account has since abandoned could misdescribe it. That is real,
and it is what the cap and the date are for — not a reason to prefer a number the
ADR measured as worse.

## Out of scope

Changing the thresholds (70%, 10 readings). They are fine; nothing here argues the
bar is in the wrong place, only that it is being applied to the wrong 28 days.
