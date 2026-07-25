import * as React from "react";
import { AlertTriangle, Info, Pencil } from "lucide-react";
import {
  useClosePhase,
  useOpenPhase,
  usePhases,
  usePlan,
  useProfile,
  useUpdateProfile,
} from "@/hooks/use-plan";
import { formatExact } from "@/lib/format";
import type {
  Adherence,
  BasalEstimate,
  BodyCompositionTrust,
  EstimateBasis,
  EstimateInput,
  Expenditure,
  Guardrail,
  Phase,
  Plan,
  Targets,
} from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { CenteredSpinner } from "./spinner";
import { ManualEntryDialog } from "./manual-entry-dialog";

/** The Target rate scale. The zones are **vocabulary**, not options: they name regions so
 *  "aggressive cut" stays legible, while the rate itself stays continuous. */
const RATE_MIN = -1.25;
const RATE_MAX = 0.75;
const RATE_STEP = 0.05;

const ZONES: { from: number; to: number; label: string }[] = [
  { from: RATE_MIN, to: -0.75, label: "Aggressive cut" },
  { from: -0.75, to: -0.25, label: "Moderate cut" },
  { from: -0.25, to: 0.1, label: "Maintenance" },
  { from: 0.1, to: 0.35, label: "Lean bulk" },
  { from: 0.35, to: RATE_MAX, label: "Bulk" },
];

function zoneFor(rate: number): string {
  return ZONES.find((z) => rate >= z.from && rate < z.to)?.label ?? (rate < 0 ? "Aggressive cut" : "Bulk");
}

/** BASIS_LABEL says where a figure came from, in words. The basis is not a footnote: it is
 *  the difference between a number worth eating to and one worth ignoring. */
const BASIS_LABEL: Record<EstimateBasis, string> = {
  observed: "from your intake and weight trend",
  recorded: "from your devices",
  predicted: "estimated from an equation",
};

const INPUT_LABEL: Record<EstimateInput, string> = {
  lean_mass: "lean mass",
  mass: "body mass",
  height: "height",
  age: "your date of birth",
  sex: "biological sex",
};

const kcal = (v: number) => `${formatExact(Math.round(v))} kcal`;
const grams = (v: number) => `${formatExact(Math.round(v))} g`;
const pct = (v: number) => `${v > 0 ? "+" : ""}${formatExact(Math.round(v * 100) / 100)} %`;

/** PlanPage answers "what should I eat, and am I doing it?" — and is honest about how much
 *  confidence each of its numbers deserves. */
export function PlanPage() {
  const [preview, setPreview] = React.useState<number | null>(null);
  const plan = usePlan(preview ?? undefined);
  const [entryOpen, setEntryOpen] = React.useState(false);

  // The slider is uncontrolled until touched: until then the server decides the rate —
  // the open Phase's, else the measured actual — so the page opens by stating what the
  // Account is already doing rather than presenting an empty form.
  const rate = preview ?? plan.data?.rate_pct_per_week ?? 0;

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3">
        <h1 className="text-xl font-semibold">Plan</h1>
        <Button variant="outline" size="sm" className="h-7" onClick={() => setEntryOpen(true)}>
          <Pencil className="size-3.5" /> Enter a value
        </Button>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        {plan.isLoading && !plan.data ? (
          <CenteredSpinner />
        ) : plan.data?.insufficient ? (
          <InsufficientState onEnter={() => setEntryOpen(true)} />
        ) : plan.data ? (
          <div className="mx-auto flex max-w-3xl flex-col gap-6">
            <ExpenditureCard expenditure={plan.data.expenditure} />
            <RateCard
              plan={plan.data}
              rate={rate}
              onPreview={setPreview}
              onCommitted={() => setPreview(null)}
            />
            {plan.data.targets && <TargetsCard targets={plan.data.targets} />}
            {plan.data.guardrails.length > 0 && <GuardrailsCard rails={plan.data.guardrails} />}
            {plan.data.adherence && <AdherenceCard adherence={plan.data.adherence} />}
            <BasalCard basal={plan.data.basal} preselected={plan.data.preselected_equation} />
            <ProfileCard onEnter={() => setEntryOpen(true)} />
            <PhaseHistory />
          </div>
        ) : null}
      </div>

      <ManualEntryDialog open={entryOpen} onOpenChange={setEntryOpen} />
    </div>
  );
}

function Card({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border bg-card p-5">
      <div className="mb-3 flex items-center gap-1.5">
        <h2 className="text-sm font-medium uppercase tracking-wide text-muted-foreground">{title}</h2>
        {hint && (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="cursor-help text-muted-foreground">
                <Info className="size-3.5" />
              </span>
            </TooltipTrigger>
            <TooltipContent className="max-w-xs">{hint}</TooltipContent>
          </Tooltip>
        )}
      </div>
      {children}
    </section>
  );
}

/** ExpenditureCard is the headline. The basis sits next to the figure, always — a number
 *  whose provenance is unknown cannot be trusted or argued with. */
function ExpenditureCard({ expenditure }: { expenditure?: Expenditure }) {
  if (!expenditure) return null;
  return (
    <Card title="Daily expenditure">
      <div className="flex flex-wrap items-baseline gap-3">
        <span className="text-4xl font-semibold tabular-nums">{kcal(expenditure.kcal)}</span>
        <span className="text-sm text-muted-foreground">{BASIS_LABEL[expenditure.basis]}</span>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        {expenditure.basis === "observed" && expenditure.mean_intake_kcal !== undefined && (
          <>
            Back-computed over {expenditure.window_days} days: you averaged{" "}
            {kcal(expenditure.mean_intake_kcal)} across {expenditure.intake_days} logged days while
            your weight trend moved{" "}
            {formatExact(Math.round((expenditure.mass_slope_kg_per_day ?? 0) * 7000) / 1000)} kg per
            week.
          </>
        )}
        {expenditure.basis === "recorded" && (
          <>
            The mean of what your devices reported over {expenditure.window_days} days. Recorded
            expenditure often overstates the truth — on a reference account by around 970 kcal a day
            — so treat this as an upper bound until you have a food log to check it against.
          </>
        )}
        {expenditure.basis === "predicted" && expenditure.activity_factor !== undefined && (
          <>
            An equation ({kcal(expenditure.basal_kcal ?? 0)} at rest) times an activity factor of{" "}
            {expenditure.activity_factor}. A guess, not a measurement: log your food for a few weeks
            and Verve will replace it with what your body actually did.
          </>
        )}
      </p>
    </Card>
  );
}

/** RateCard is the Target rate slider. The rate reads three ways at once because each
 *  answers a different question: %/week is precise, kg/week is intuitive, and the calorie
 *  target is the one that gets acted on. */
function RateCard({
  plan,
  rate,
  onPreview,
  onCommitted,
}: {
  plan: Plan;
  rate: number;
  onPreview: (rate: number) => void;
  onCommitted: () => void;
}) {
  const open = useOpenPhase();
  const close = useClosePhase();
  const kgPerWeek = plan.targets?.kg_per_week;

  return (
    <Card
      title="Target rate"
      hint="A rate, not a calorie figure: the same deficit is trivial at one body size and dangerous at another. The calorie target is derived from it."
    >
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
        <span className="text-3xl font-semibold tabular-nums">{pct(rate)}</span>
        <span className="text-sm text-muted-foreground">per week</span>
        {kgPerWeek !== undefined && (
          <span className="text-sm tabular-nums text-muted-foreground">
            {formatExact(Math.round(kgPerWeek * 100) / 100)} kg / week
          </span>
        )}
        <span className="ml-auto text-sm font-medium">{zoneFor(rate)}</span>
      </div>

      <input
        type="range"
        min={RATE_MIN}
        max={RATE_MAX}
        step={RATE_STEP}
        value={rate}
        aria-label="Target rate, percent of body mass per week"
        onChange={(e) => onPreview(Number(e.target.value))}
        className="mt-4 w-full accent-primary"
      />
      {/* Each label is sized to the span of its own zone. Spacing them evenly would put
          "Maintenance" at the centre of the track when the maintenance zone is nowhere
          near it — the labels would claim positions they do not occupy. */}
      <div className="mt-1 flex text-[10px] uppercase tracking-wide text-muted-foreground">
        {ZONES.map((z) => (
          <span
            key={z.label}
            className="truncate border-l border-border/60 pl-1 first:border-l-0 first:pl-0"
            style={{ width: `${((z.to - z.from) / (RATE_MAX - RATE_MIN)) * 100}%` }}
            title={z.label}
          >
            {z.label}
          </span>
        ))}
      </div>

      {plan.actual_rate && (
        <p className="mt-3 text-xs text-muted-foreground">
          You are measurably moving at {pct(plan.actual_rate.pct_per_week)} per week, from{" "}
          {plan.actual_rate.mass_days} weigh-ins over {plan.actual_rate.window_days} days.
        </p>
      )}

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          disabled={open.isPending}
          onClick={() =>
            open.mutate(rate, {
              onSuccess: onCommitted,
            })
          }
        >
          {plan.phase ? "Start a new phase at this rate" : "Start a phase at this rate"}
        </Button>
        {plan.phase && (
          <Button
            size="sm"
            variant="ghost"
            disabled={close.isPending}
            onClick={() => close.mutate(plan.phase!.id, { onSuccess: onCommitted })}
          >
            End the current phase
          </Button>
        )}
        {plan.phase && (
          <span className="text-xs text-muted-foreground">
            Open since {plan.phase.started_at.slice(0, 10)} at {pct(plan.phase.rate_pct_per_week)}
          </span>
        )}
      </div>
    </Card>
  );
}

/** TargetsCard shows the macros with their authority made visible: protein is a floor with
 *  evidence behind it, the fat/carbohydrate split is a stated convention. Rendering them as
 *  three equal numbers would claim a precision Verve does not have. */
function TargetsCard({ targets }: { targets: Targets }) {
  return (
    <Card title="Daily targets">
      <div className="flex flex-wrap items-baseline gap-3">
        <span className="text-4xl font-semibold tabular-nums">{kcal(targets.kcal)}</span>
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-3">
        <div className="rounded-md border p-3">
          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Protein — floor
          </div>
          <div className="mt-1 text-2xl font-semibold tabular-nums">{grams(targets.protein_g)}</div>
          <p className="mt-1 text-xs text-muted-foreground">
            {formatExact(targets.protein_g_per_kg_lean)} g per kg of{" "}
            {targets.protein_from_body_mass ? "body mass" : "lean mass"}. Rises with the deficit:
            in restriction, protein is what decides whether the mass you lose is fat or muscle.
            {targets.protein_from_body_mass && " Scaled on body mass because no lean mass is known — a weaker basis."}
          </p>
        </div>

        <div className="rounded-md border border-dashed p-3">
          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Fat — convention
          </div>
          <div className="mt-1 text-2xl font-semibold tabular-nums text-muted-foreground">
            {grams(targets.fat_g)}
          </div>
        </div>

        <div className="rounded-md border border-dashed p-3">
          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Carbohydrate — convention
          </div>
          <div className="mt-1 text-2xl font-semibold tabular-nums text-muted-foreground">
            {grams(targets.carb_g)}
          </div>
        </div>
      </div>

      {targets.conventional_split && (
        <p className="mt-3 text-xs text-muted-foreground">
          Past a hormonal fat floor, the split between fat and carbohydrate has no demonstrated
          effect at equal calories and protein. These two are a convention Verve states, not a
          recommendation it can defend — unlike the protein floor.
        </p>
      )}
    </Card>
  );
}

function GuardrailsCard({ rails }: { rails: Guardrail[] }) {
  return (
    <section className="rounded-lg border border-amber-500/40 bg-amber-500/5 p-5">
      <div className="mb-2 flex items-center gap-1.5 text-amber-700 dark:text-amber-500">
        <AlertTriangle className="size-4" />
        <h2 className="text-sm font-medium uppercase tracking-wide">Worth knowing</h2>
      </div>
      <ul className="space-y-1.5 text-sm">
        {rails.map((rail) => (
          <li key={rail.code}>{rail.message}</li>
        ))}
      </ul>
    </section>
  );
}

/** AdherenceCard compares intent to outcome, neutrally — no red, no green. Verve does not
 *  know which direction is good for a given Account (the same rule as the Baseline delta,
 *  ADR 0015). */
function AdherenceCard({ adherence }: { adherence: Adherence }) {
  const rows: { label: string; target: string; actual?: string }[] = [
    {
      label: "Rate",
      target: `${pct(adherence.target_rate_pct_per_week)} / week`,
      actual:
        adherence.actual_rate_pct_per_week !== undefined
          ? `${pct(adherence.actual_rate_pct_per_week)} / week`
          : undefined,
    },
    {
      label: "Intake",
      target: kcal(adherence.target_kcal),
      actual: adherence.actual_kcal !== undefined ? kcal(adherence.actual_kcal) : undefined,
    },
    {
      label: "Protein",
      target: grams(adherence.target_protein_g),
      actual: adherence.actual_protein_g !== undefined ? grams(adherence.actual_protein_g) : undefined,
    },
  ];

  return (
    <Card title={`Since this phase started — ${adherence.window_days} days`}>
      {adherence.thin && (
        <p className="mb-3 text-xs text-muted-foreground">
          A short window. These figures will mean more after a couple of weeks.
        </p>
      )}
      <div className="grid gap-2">
        <div className="grid grid-cols-3 gap-2 text-[10px] uppercase tracking-wide text-muted-foreground">
          <span />
          <span>Target</span>
          <span>Actual</span>
        </div>
        {rows.map((row) => (
          <div key={row.label} className="grid grid-cols-3 items-baseline gap-2 text-sm">
            <span className="text-muted-foreground">{row.label}</span>
            <span className="tabular-nums">{row.target}</span>
            <span className="tabular-nums">{row.actual ?? <span className="text-muted-foreground">—</span>}</span>
          </div>
        ))}
      </div>
    </Card>
  );
}

/** BasalCard lists every equation with its figure. Ones that cannot run are greyed and say
 *  which input would unlock them — the server decides that, so the client never hardcodes
 *  which equation wants what. */
function BasalCard({
  basal,
  preselected,
}: {
  basal: BasalEstimate[];
  preselected?: string;
}) {
  return (
    <Card
      title="Resting expenditure"
      hint="What your body spends at rest. Shown for reference: when a better basis exists, your calorie target does not depend on which equation you pick."
    >
      <div className="grid gap-2 sm:grid-cols-2">
        {basal.map((b) => {
          const usable = b.kcal !== undefined;
          return (
            <div
              key={b.equation}
              className={cn(
                "flex items-baseline justify-between gap-3 rounded-md border px-3 py-2",
                b.equation === preselected && "border-primary/60 bg-primary/5",
                !usable && "opacity-60",
              )}
            >
              <span className="shrink-0 text-sm">
                {b.name}
                {b.equation === preselected && (
                  <span className="ml-2 text-[10px] uppercase tracking-wide text-primary">
                    preselected
                  </span>
                )}
              </span>
              {usable ? (
                <span className="tabular-nums">{kcal(b.kcal!)}</span>
              ) : (
                <span className="text-right text-xs text-muted-foreground">
                  needs {(b.missing ?? []).map((m) => INPUT_LABEL[m]).join(" and ")}
                </span>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}

const TRUST_OPTIONS: { value: BodyCompositionTrust; label: string; hint: string }[] = [
  { value: "measured", label: "Measured", hint: "DEXA, hydrostatic weighing" },
  { value: "estimated", label: "Estimated", hint: "a bioimpedance scale" },
  { value: "unknown", label: "Unknown", hint: "no idea where it comes from" },
];

/** ProfileCard edits the Account attributes that are not Measurements. Height, mass and
 *  body fat are absent on purpose: they are Metrics, entered through Manual entry, and
 *  mirroring them here would create a second height that diverges from the graphed one. */
function ProfileCard({ onEnter }: { onEnter: () => void }) {
  const profile = useProfile();
  const update = useUpdateProfile();
  const trust = profile.data?.body_composition_trust ?? profile.data?.derived_trust;
  const declared = profile.data?.body_composition_trust !== undefined;

  return (
    <Card title="Profile">
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="dob">Date of birth</Label>
          <Input
            id="dob"
            type="date"
            defaultValue={profile.data?.date_of_birth ?? ""}
            onBlur={(e) => {
              const value = e.target.value;
              if (value && value !== profile.data?.date_of_birth) {
                update.mutate({ date_of_birth: value });
              }
            }}
          />
        </div>

        <div className="space-y-1.5">
          <Label>Biological sex</Label>
          <div className="flex gap-1">
            {(["male", "female"] as const).map((sex) => (
              <Button
                key={sex}
                type="button"
                size="sm"
                variant={profile.data?.biological_sex === sex ? "secondary" : "outline"}
                onClick={() => update.mutate({ biological_sex: sex })}
              >
                {sex === "male" ? "Male" : "Female"}
              </Button>
            ))}
          </div>
          <p className="text-xs text-muted-foreground">
            An input to Mifflin-St Jeor and Harris-Benedict, nothing more. The lean-mass
            equations do not use it.
          </p>
        </div>
      </div>

      <div className="mt-5 space-y-1.5">
        <Label>Body-composition data</Label>
        <div className="flex flex-wrap gap-1">
          {TRUST_OPTIONS.map((opt) => (
            <Button
              key={opt.value}
              type="button"
              size="sm"
              variant={trust === opt.value ? "secondary" : "outline"}
              onClick={() => update.mutate({ body_composition_trust: opt.value })}
            >
              {opt.label}
              <span className="ml-1.5 text-xs text-muted-foreground">{opt.hint}</span>
            </Button>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">
          {declared
            ? "Estimated or unknown moves the lean-mass equations below the anthropometric ones — demoted, never hidden."
            : `Suggested from where your data comes from. A cheap scale can report a body fat that simply
               tracks your weight, which would make the lean-mass equations weight equations in disguise.`}
        </p>
      </div>

      <p className="mt-5 text-xs text-muted-foreground">
        Height, body mass and body fat are measurements, not profile fields —{" "}
        <button type="button" className="underline underline-offset-2" onClick={onEnter}>
          enter one by hand
        </button>{" "}
        and it will outrank your devices for the day it falls on.
      </p>
    </Card>
  );
}

function PhaseHistory() {
  const phases = usePhases();
  const rows = phases.data ?? [];
  if (rows.length === 0) return null;

  return (
    <Card title="Phase history">
      <div className="space-y-1">
        {rows.map((phase: Phase) => (
          <div key={phase.id} className="flex items-baseline justify-between gap-3 text-sm">
            <span className="tabular-nums">{pct(phase.rate_pct_per_week)} / week</span>
            <span className="text-muted-foreground">
              {phase.started_at.slice(0, 10)} → {phase.ended_at ? phase.ended_at.slice(0, 10) : "now"}
            </span>
          </div>
        ))}
      </div>
    </Card>
  );
}

/** InsufficientState routes somewhere rather than showing a blank card or a zero. */
function InsufficientState({ onEnter }: { onEnter: () => void }) {
  return (
    <div className="mx-auto max-w-md py-16 text-center">
      <h2 className="text-lg font-medium">Not enough to go on yet</h2>
      <p className="mt-2 text-sm text-muted-foreground">
        A calorie target needs to know what you spend. Verve works that out from your food log
        against your weight trend, or failing that from what your devices record.
      </p>
      <div className="mt-4 flex justify-center gap-2">
        <Button size="sm" onClick={onEnter}>
          Enter a measurement
        </Button>
      </div>
    </div>
  );
}
