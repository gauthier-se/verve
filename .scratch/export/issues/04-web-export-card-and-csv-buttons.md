Status: done
Blocked by: 03

# 04: web: the way out, on the two pages where it is looked for

## What

- **`web/src/lib/download.ts`**, a pure module, because it is the only part of
  this issue a test can execute (`web/src/lib/*.test.ts` is what the runner
  covers):
  - `seriesCsvHref(metric: string | string[], range: RangeTokens, bucket: Bucket)`
    builds `/v1/series.csv?…` from the same tokens `use-series.ts` puts on
    `/v1/series`, deliberately reusing that hook's query-string builder rather
    than a second one: the file and the chart must ask the same question, and two
    builders is how they stop.
  - `archiveHref()` returns `/v1/export/archive`.
  - Both are plain `<a href download>` targets, not `fetch`: the session cookie is
    same-origin (`lib/api.ts:35`), the browser owns the download UI, and pulling a
    hundred megabytes through JavaScript to hand it back as a blob would buy
    nothing but a memory ceiling.
  - `download.test.ts` beside it: the CSV href carries the preset, a custom
    from/to, the bucket and every Metric of a multi-Metric request, and it carries
    **no** baseline token even when the range tokens hold one, which is the 422 in
    issue 03 avoided before it is provoked.
- **The CSV button lives in `ledger-detail-table.tsx`**, next to the existing
  `Copy` (line 5 imports `copyTsv`). The PRD said the Data page and the Metric
  page; there is one component and only `metric-page.tsx:159` mounts it, so one
  button reaches every surface that shows the numbers, and the Data page keeps
  its overview table untouched (a Ledger row is a scoreboard line, not a series).
  - The two controls answer different questions and the labels must say so:
    **Copy** puts the table as you sorted and filtered it on the clipboard,
    **CSV** downloads the whole window from the server at the grain on screen.
    One tooltip each, and no third control that tries to be both.
- **The Export card on `/import`** (`import-page.tsx`), below the drop zone and
  the Exclusions card, since the page reads top to bottom as data coming in and
  then data going out:
  - one sentence naming what the Archive holds, in the product's plain register:
    every measurement, night, workout and GPX trace this account holds, plus what
    the catalog could not read, as one zip;
  - one sentence saying what it does not hold: dashboards, panels and pins stay
    with this instance;
  - one plain warning, not a destructive banner: the file is your whole history in
    clear, and nothing encrypts it for you;
  - a `Download archive` button, and under it, quietly, that re-importing the file
    on the Import page above is how it comes back, which is the sentence that
    turns the feature from an export into a round trip;
  - no progress and no spinner after the click: the browser's own download UI is
    the feedback, and a spinner that cannot know when it is finished is a lie
    (the response has no `Content-Length`, issue 03).
- **`app-shell.tsx:34`**: the nav entry becomes
  `{ to: "/import", label: "Import & export", short: "Import", icon: Download }`.
  The route does not change, because a URL is not worth breaking over a label, and
  nobody looks for their data's exit under "Import". `short` stays as it is: the
  collapsed rail has room for one word and the page still leads with importing.
- **Types**: none. Neither endpoint returns the JSON envelope, so `lib/types.ts`
  and the contract test are untouched, which is worth a line in the PR description
  because that file changes on nearly every other milestone.
- **Docs pass**, the milestone's last step (PRD's Docs section): the README bullet
  under "Own your data", `docs/deployment.md`'s backup section gaining the
  distinction between copying the directory and taking an Archive, the ROADMAP
  shipped row, and the new "Later" entry for sharing a Dashboard as JSON.

## Why here

The Export card goes on the Import page and not into Appearance or a new Settings
screen for the same reason the Exclusions list went there: that page is where a
person already goes to think about the whole of their data, and a second page
about data movement would leave each half looking incomplete.

The CSV button sits beside Copy rather than replacing it because the two are not
the same file. Copy is the table in front of you, sorted the way you sorted it,
for pasting into a message. CSV is the window as the server computed it, for
keeping. Replacing the first with the second would be a regression for the
gesture people actually use most.

No `fetch` and no blob anywhere in this issue. Every download here is a link, so
the browser streams it to disk and an Archive larger than the tab's memory is
still just a file.

## Comments

Shipped. `web/src/lib/series-url.ts` and its test, the CSV button in
`ledger-detail-table.tsx`, the Export card in `import-page.tsx`, the nav label
in `app-shell.tsx`, and the docs pass across README.md, ROADMAP.md and
docs/deployment.md. Typecheck, production build and `npm run test` (62 tests)
all clean.

Smoke-tested on the real binary rather than only in tests: import a four-record
Apple export into account A, `verve export` it, import the archive into account
B with no flag (the registry recognizes it by content), re-import it to see
`0 added / 2 skipped`, re-export B and diff the data entries against A's, which
are identical. Then over HTTP: `/v1/series.csv` returns the pinned bytes with
its dated filename, a `baseline_rule` answers 422 with the sentence that names
the second download, `/v1/export/archive` streams a zip that the CLI reads back,
and the same URL without a session is 401.

Four things came out of the implementation that the spec did not anticipate:

- **The module is `lib/series-url.ts`, not `lib/download.ts`.** The spec had the
  download module reuse the hook's query builder; there was no builder to reuse,
  only twenty inline lines in `useSeries`'s `queryFn`. Extracting it is the whole
  point ("the file and the curve ask the same question"), and once extracted the
  module is about the Series URL, with the two hrefs beside it. `useSeries` now
  calls it, so the extraction is load-bearing rather than a copy.
- **One surface, not two.** `LedgerDetailTable` is mounted only by the Metric
  page, so the CSV button reaches every screen that shows the numbers from one
  place. The Data page's overview table is a scoreboard of latest values and
  window folds, not a series, and a CSV of it would be a third grain.
- **The page title moved too.** The nav says "Import & export"; leaving the
  screen's own header at "Import data" would have made the page look like the
  wrong one for half of what it does.
- **`Content-Length` is sometimes there after all.** The handler sets none, and
  net/http infers one for a body small enough to fit its write buffer, so a
  nearly empty Account gets a determinate download. The "no progress bar" call
  stands for any real Archive, and the handler comment now says so rather than
  claiming more than it can.
