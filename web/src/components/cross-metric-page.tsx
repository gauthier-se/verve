import * as React from "react";
import { Link } from "@tanstack/react-router";
import { Pin } from "lucide-react";
import {
  CartesianGrid,
  ReferenceLine,
  ResponsiveContainer,
  Scatter,
  ScatterChart,
  Tooltip,
  XAxis,
  YAxis,
  ZAxis,
} from "recharts";
import { useCoVary } from "@/hooks/use-covary";
import { AXIS, DIRECTION_DOWN, DIRECTION_UP, GRID, PRIMARY, RECESSED, SERIES_COLORS, shade } from "@/lib/chart";
import { formatAxisValue, formatDayRange, formatExact } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import { RANGE_PRESETS, type RangeTokens } from "@/lib/time-range";
import { cn } from "@/lib/utils";
import type { Bucket, CoVary, CoVaryLag, Pair, Scatter as ScatterData } from "@/lib/types";
import { Card } from "./ui/card";
import { Chip, Figure, Meta, ScreenTitle, SectionTitle, Track } from "./ui/figure";
import { CenteredSpinner } from "./spinner";

type Preset = (typeof RANGE_PRESETS)[number]["value"];

/** The lag presets. Each carries both a shift and the grain the question is asked
 *  at, because the two are one choice: "the next morning" is a day-grain question
 *  and "the week after" a week-grain one, and offering them separately would offer
 *  combinations that mean nothing. The server owns the same table. */
const LAGS: { value: CoVaryLag; label: string }[] = [
  { value: "same", label: "same bucket" },
  { value: "next_day", label: "+1 day" },
  { value: "next_week", label: "+1 week" },
];

/** ρ below this prints no number: two decimals on a coefficient that weak claim a
 *  precision the relationship does not have. The cell keeps its (barely visible)
 *  tint, so the matrix still reads as a field rather than as holes. */
const RHO_LEGIBLE = 0.15;

/** CrossMetricPage answers "does this move with that" across the Metrics the
 *  Account pinned.
 *
 *  It is the one screen built entirely out of relationships rather than values, and
 *  the one that most needs to refuse to say more than it knows. So: rank correlation
 *  and never a cause; hue for direction and never for valence; a shared-bucket count
 *  beside every coefficient; and a threshold under which a pair is shown, greyed,
 *  and not ranked at all. The caveat paragraph under the header is part of the
 *  design, not boilerplate. */
export function CrossMetricPage() {
  const [preset, setPreset] = React.useState<Preset>("1y");
  const [lag, setLag] = React.useState<CoVaryLag>("same");
  const range: RangeTokens = { preset, from: null, to: null };
  const query = useCoVary({ range, lag });
  const data = query.data;

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3.5">
        <div className="flex min-w-0 items-center gap-2.5">
          <ScreenTitle>Cross-metric</ScreenTitle>
          {data && (
            <Chip>
              {formatDayRange(data.range.from, data.range.last)} · {grainWord(data.bucket)} buckets
            </Chip>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Segmented
            options={LAGS}
            value={lag}
            onChange={setLag}
            label="Lag"
            className="font-mono tabular-nums"
          />
          <Segmented
            options={RANGE_PRESETS.map((p) => ({ value: p.value, label: p.label }))}
            value={preset}
            onChange={setPreset}
            label="Range"
            className="font-mono tabular-nums"
          />
        </div>
      </header>

      <div className="flex-1 space-y-4 overflow-y-auto p-6">
        <p className="max-w-[47rem] text-xs leading-relaxed text-muted-foreground">
          Co-variation between the metrics you pinned, over the window above. Verve reports the
          strength and the direction of a relationship and nothing else: it does not know which
          direction is good for your metric, and a relationship here is not a cause.
        </p>

        {query.isLoading ? (
          <CenteredSpinner />
        ) : query.isError ? (
          <p className="py-6 text-center text-sm text-destructive">Couldn’t compute this window.</p>
        ) : !data || data.metrics.length < 2 ? (
          <NotEnoughPins pinned={data?.metrics.length ?? 0} />
        ) : (
          <>
            <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(21rem,1fr))]">
              <MatrixCard data={data} />
              <ScatterCard scatter={data.strongest} lag={lag} />
            </div>
            <RankingCard data={data} />
            {data.skipped && data.skipped.length > 0 && <SkippedNote data={data} />}
          </>
        )}
      </div>
    </div>
  );
}

/** MatrixCard is every ordered pair at a glance: rows read "this metric against the
 *  one in the column". Hue is direction — together in the accent, opposite in the
 *  cool chart colour — and alpha is strength. */
function MatrixCard({ data }: { data: CoVary }) {
  const byPair = new Map(data.pairs.map((p) => [pairKey(p.a, p.b), p]));

  return (
    <Card className="flex flex-col p-4">
      <div className="flex items-baseline justify-between gap-3 pb-3.5">
        <SectionTitle>Pinned metrics, pairwise</SectionTitle>
        <Meta>{data.lag_shift === 0 ? "no lag" : `lag ${lagNote(data.lag)}`}</Meta>
      </div>

      <div className="overflow-x-auto">
        <div
          className="grid min-w-max items-center gap-[3px]"
          style={{ gridTemplateColumns: `7.5rem repeat(${data.metrics.length}, minmax(2.25rem, 1fr))` }}
        >
          <div />
          {data.metrics.map((slug) => (
            <div
              key={slug}
              className="truncate text-center font-mono text-3xs tabular-nums text-muted-foreground"
              title={metricLabel(slug)}
            >
              {abbreviate(slug)}
            </div>
          ))}

          {data.metrics.map((row) => (
            <React.Fragment key={row}>
              <div className="truncate pr-2 text-2xs" title={metricLabel(row)}>
                {metricLabel(row)}
              </div>
              {data.metrics.map((col) => (
                <MatrixCell key={col} row={row} col={col} pair={byPair.get(pairKey(row, col))} />
              ))}
            </React.Fragment>
          ))}
        </div>
      </div>

      {/* Hue encodes direction, never valence: "moves opposite" is not bad news,
          and a red-to-green ramp here would say it was. */}
      <div className="flex items-center gap-2.5 pt-3.5 text-3xs text-muted-foreground">
        <span>moves together</span>
        <span
          className="h-1.5 flex-1 rounded-full"
          style={{
            background: `linear-gradient(90deg, ${shade(DIRECTION_UP, 0.63)}, ${RECESSED}, ${shade(
              DIRECTION_DOWN,
              0.63,
            )})`,
          }}
          aria-hidden
        />
        <span>moves opposite</span>
      </div>
    </Card>
  );
}

/** MatrixCell is one ordered pair. The diagonal is a dot: a Metric against itself
 *  is not a question, and filling it with 1.00 would put the strongest colour in
 *  the grid on the one cell carrying no information. */
function MatrixCell({ row, col, pair }: { row: string; col: string; pair?: Pair }) {
  if (row === col) {
    return (
      <div className="flex aspect-square items-center justify-center rounded-[3px] bg-muted/40 text-3xs text-muted-foreground/70">
        ·
      </div>
    );
  }
  if (!pair || !pair.ranked) {
    return (
      <div
        className="flex aspect-square items-center justify-center rounded-[3px] bg-muted/40"
        title={
          pair
            ? `${metricLabel(row)} × ${metricLabel(col)}: ${pair.shared} shared buckets, too few to rank`
            : undefined
        }
      />
    );
  }

  // Alpha from strength, floored so a real-but-weak relationship is still a tint
  // and not an empty cell, capped so the strongest one stays legible under text.
  const strength = Math.min(Math.abs(pair.rho) / 0.5, 1);
  const alpha = 0.08 + strength * 0.55;
  const hue = pair.rho >= 0 ? DIRECTION_UP : DIRECTION_DOWN;

  return (
    <div
      className="flex aspect-square items-center justify-center rounded-[3px] font-mono text-3xs tabular-nums"
      style={{
        background: shade(hue, alpha),
        // Above this fill the cell is dark enough (or saturated enough) that muted
        // text stops reading; the page background is the one colour guaranteed to
        // contrast with a saturated chart hue in either mode.
        color: alpha > 0.5 ? "hsl(var(--background))" : "hsl(var(--foreground))",
      }}
      title={`${metricLabel(row)} × ${metricLabel(col)}: ρ ${formatRho(pair.rho)} over ${pair.shared} shared buckets`}
    >
      {Math.abs(pair.rho) < RHO_LEGIBLE ? "" : formatRho(pair.rho)}
    </div>
  );
}

/** ScatterCard draws the strongest pair bucket by bucket, with the server's fitted
 *  line. A matrix says how strong; only a scatter says what shape — whether the
 *  relationship is a trend, a cluster with two outliers, or a wall. */
function ScatterCard({ scatter, lag }: { scatter?: ScatterData; lag: CoVaryLag }) {
  if (!scatter) {
    return (
      <Card className="flex min-h-[17rem] items-center justify-center p-4 text-center text-xs text-muted-foreground">
        No pair in this window shares enough buckets to draw.
      </Card>
    );
  }

  const points = scatter.points.map((p) => ({ x: p.x, y: p.y, bucket: p.bucket }));
  const fit = scatter.fit;

  return (
    <Card className="flex flex-col p-4">
      <div className="flex items-baseline justify-between gap-3 pb-1.5">
        <SectionTitle>
          {metricLabel(scatter.a)} → {metricLabel(scatter.b)}
          {lag !== "same" && <span className="text-muted-foreground"> the {lagWord(lag)} after</span>}
        </SectionTitle>
        <Meta>{scatter.shared} buckets</Meta>
      </div>
      <div className="flex items-baseline gap-2 pb-2.5">
        <Figure size="strip">{formatRho(scatter.rho)}</Figure>
        <span className="text-2xs text-muted-foreground">
          rank correlation{lag === "same" ? "" : `, lag ${lagNote(lag)}`}
        </span>
      </div>

      <div className="h-[11.5rem]">
        <ResponsiveContainer width="100%" height="100%">
          <ScatterChart margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
            <CartesianGrid stroke={GRID} strokeDasharray="3 3" />
            <XAxis
              type="number"
              dataKey="x"
              domain={["dataMin", "dataMax"]}
              stroke={AXIS}
              fontSize={10}
              tickLine={false}
              axisLine={false}
              tickFormatter={(v: number) => formatAxisValue(v, scatter.unit_a)}
            />
            <YAxis
              type="number"
              dataKey="y"
              domain={["dataMin", "dataMax"]}
              width={40}
              stroke={AXIS}
              fontSize={10}
              tickLine={false}
              axisLine={false}
              tickFormatter={(v: number) => formatAxisValue(v, scatter.unit_b)}
            />
            <ZAxis range={[10, 10]} />
            <Tooltip
              cursor={{ stroke: GRID }}
              content={({ active, payload }) => {
                if (!active || !payload?.length) return null;
                const d = payload[0].payload as { x: number; y: number; bucket: string };
                return (
                  <div className="rounded-md border bg-popover px-2.5 py-1.5 text-2xs shadow-md">
                    <div className="font-mono tabular-nums">{d.bucket}</div>
                    <div className="text-muted-foreground">
                      {metricLabel(scatter.a)} <span className="font-mono tabular-nums">{formatExact(d.x, scatter.unit_a)}</span>{" "}
                      {scatter.unit_a}
                    </div>
                    <div className="text-muted-foreground">
                      {metricLabel(scatter.b)} <span className="font-mono tabular-nums">{formatExact(d.y, scatter.unit_b)}</span>{" "}
                      {scatter.unit_b}
                    </div>
                  </div>
                );
              }}
            />
            {/* The fit is drawn as a two-point segment rather than a trend line
                component: the line was computed server-side, and this only joins the
                endpoints it came back with. */}
            {fit && (
              <ReferenceLine
                ifOverflow="extendDomain"
                segment={[
                  { x: fit.x1, y: fit.y1 },
                  { x: fit.x2, y: fit.y2 },
                ]}
                stroke={PRIMARY}
                strokeWidth={1.5}
              />
            )}
            <Scatter data={points} fill={SERIES_COLORS[1]} fillOpacity={0.55} />
          </ScatterChart>
        </ResponsiveContainer>
      </div>

      <div className="flex justify-between pt-2 font-mono text-3xs tabular-nums text-muted-foreground/70">
        <span>{scatter.unit_a}</span>
        <span>{scatter.unit_b}</span>
      </div>
    </Card>
  );
}

/** RankingCard is the same pairs read as a list: strongest first, each with a bar
 *  proportional to |ρ|, its own shared-bucket count, and a plain-language direction.
 *  The matrix is for scanning; this is for reading. */
function RankingCard({ data }: { data: CoVary }) {
  // Each unordered pair appears twice in an unlagged matrix (A×B and B×A are the
  // same number), so the list keeps one of them. With a lag they are two different
  // questions and both stay.
  const rows = data.lag_shift === 0 ? dedupe(data.pairs) : data.pairs;
  const ranked = rows.filter((p) => p.ranked).slice(0, 8);

  return (
    <Card>
      <div className="flex items-baseline justify-between gap-3 px-4 pb-2.5 pt-3.5">
        <SectionTitle>Strongest relationships in this window</SectionTitle>
        <Meta>ranked by |ρ| · {grainWord(data.bucket)}</Meta>
      </div>

      {ranked.length === 0 ? (
        <p className="border-t px-4 py-5 text-center text-xs text-muted-foreground">
          No pair shares enough buckets in this window to be ranked.
        </p>
      ) : (
        ranked.map((pair) => (
          <div
            key={`${pair.a}-${pair.b}`}
            className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 border-t border-border/60 px-4 py-2.5"
          >
            <span className="w-56 shrink-0 truncate text-xs">
              <Link to="/data/$metric" params={{ metric: pair.a }} className="hover:underline">
                {metricLabel(pair.a)}
              </Link>
              <span className="text-muted-foreground/70"> × </span>
              <Link to="/data/$metric" params={{ metric: pair.b }} className="hover:underline">
                {metricLabel(pair.b)}
              </Link>
            </span>
            <span className="min-w-16 flex-1">
              <Track
                fill={Math.abs(pair.rho)}
                color={pair.rho >= 0 ? shade(DIRECTION_UP, 1) : shade(DIRECTION_DOWN, 1)}
              />
            </span>
            <span className="w-12 text-right font-mono text-xs tabular-nums">{formatRho(pair.rho)}</span>
            <span className="w-24 text-right font-mono text-2xs tabular-nums text-muted-foreground">
              {pair.shared} buckets
            </span>
            <span className="w-28 text-right text-2xs text-muted-foreground">{direction(pair.rho)}</span>
          </div>
        ))
      )}

      <p className="border-t px-4 py-2.5 text-2xs text-muted-foreground/70">
        Pairs sharing fewer than {data.min_shared} buckets in this window are not ranked. Add a metric
        to this page by pinning it.
      </p>
    </Card>
  );
}

/** SkippedNote names the pinned Metrics that are not on the matrix. A Pin the
 *  Account cannot find here is a bug until the page says why it is missing. */
function SkippedNote({ data }: { data: CoVary }) {
  return (
    <p className="text-2xs text-muted-foreground/70">
      Not paired:{" "}
      {data.skipped?.map((s, i) => (
        <span key={s.metric}>
          {i > 0 && ", "}
          {metricLabel(s.metric)} <span className="opacity-70">({s.reason})</span>
        </span>
      ))}
    </p>
  );
}

function NotEnoughPins({ pinned }: { pinned: number }) {
  return (
    <Card className="flex flex-col items-center gap-2 px-6 py-12 text-center">
      <Pin className="size-5 text-muted-foreground" aria-hidden />
      <p className="text-heading font-medium">
        {pinned === 0 ? "Nothing pinned yet" : "One metric is not a pair"}
      </p>
      <p className="max-w-md text-xs leading-relaxed text-muted-foreground">
        This page pairs the metrics you pin. Open a metric from{" "}
        <Link to="/data" className="text-primary hover:underline">
          Data
        </Link>{" "}
        and pin it — two or more, and they start being compared here.
      </p>
    </Card>
  );
}

/** Segmented is this page's preset control: the same shape as the Dashboard's range
 *  control, in mono because every option is a quantity. */
function Segmented<T extends string>({
  options,
  value,
  onChange,
  label,
  className,
}: {
  options: readonly { value: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
  label: string;
  className?: string;
}) {
  return (
    <div className="flex items-center gap-0.5 rounded-md border p-0.5" role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          aria-pressed={value === o.value}
          className={cn(
            "rounded px-2 py-1 text-2xs transition-colors",
            className,
            value === o.value
              ? "bg-secondary font-medium text-secondary-foreground"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** pairKey is the map key for one ordered pair. Both the map and the lookup go
 *  through it, so the two can never disagree about the separator: a matrix whose
 *  keys are built one way and read another renders every cell as "not ranked",
 *  which looks exactly like an account with no overlapping data.
 *
 *  A Catalog slug is [a-z0-9_], so a space cannot appear inside one. */
function pairKey(a: string, b: string): string {
  return `${a} ${b}`;
}

/** dedupe keeps one of each unordered pair, which is what an unlagged matrix's two
 *  halves are: ρ(A, B) and ρ(B, A) are the same number. */
function dedupe(pairs: Pair[]): Pair[] {
  const seen = new Set<string>();
  return pairs.filter((p) => {
    const key = [p.a, p.b].sort().join(" ");
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/** direction is the coefficient in words. Deliberately toneless: "moves together"
 *  and "moves opposite", never "improves" or "worsens". */
function direction(rho: number): string {
  const strength = Math.abs(rho);
  if (strength < RHO_LEGIBLE) return "no clear link";
  const how = rho > 0 ? "together" : "opposite";
  if (strength >= 0.5) return `moves ${how}`;
  return `leans ${how}`;
}

/** formatRho renders a coefficient with its sign, always two decimals, always the
 *  same width — a column of coefficients is read by comparison. */
function formatRho(rho: number): string {
  const sign = rho < 0 ? "−" : "";
  return `${sign}${Math.abs(rho).toFixed(2)}`;
}

/** lagNote is the lag as a quantity, for a mono chip: "+1 day". */
function lagNote(lag: CoVaryLag): string {
  return lag === "next_day" ? "+1 day" : lag === "next_week" ? "+1 week" : "none";
}

/** lagWord is the same lag as a noun, for a sentence: "the day after" reads, and
 *  "the +1 day after" does not. */
function lagWord(lag: CoVaryLag): string {
  return lag === "next_day" ? "day" : lag === "next_week" ? "week" : "bucket";
}

function grainWord(bucket: Bucket): string {
  return bucket === "day" ? "daily" : bucket === "week" ? "weekly" : "monthly";
}

/** abbreviate shortens a slug for a matrix column head, where a cell is 36px wide:
 *  the initials of its words, so "resting_heart_rate" heads as "RHR". */
function abbreviate(slug: string): string {
  return slug
    .split("_")
    .map((word) => word[0]?.toUpperCase() ?? "")
    .join("")
    .slice(0, 4);
}

