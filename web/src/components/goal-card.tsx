import * as React from "react";
import { Target, Trash2 } from "lucide-react";
import { useCloseGoal, useDeleteGoal, useGoals, useSetGoal } from "@/hooks/use-goals";
import { ApiError } from "@/lib/api";
import { formatDay, formatDayRange } from "@/lib/format";
import {
  describeGoal,
  goalUnit,
  isDuration,
  lastDayHeld,
  storedGoalValue,
  todayUTC,
  type GoalInput,
} from "@/lib/goals";
import { cn } from "@/lib/utils";
import type { Goal, GoalDirection, Metric } from "@/lib/types";
import { Button } from "./ui/button";
import { Card } from "./ui/card";
import { Eyebrow } from "./ui/figure";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

/** GoalCard is where a Metric's Goal is set, stopped and corrected, with its history
 *  underneath (ADR 0044). It is the only place a Goal is edited: a Goal belongs to the
 *  Metric and the Account, and a form on a Panel would suggest it was the Panel's.
 *
 *  It writes the Goal as a fact ("At least 7 500 a day, since 12 Mar") and says
 *  nothing about how it went: the counts sit beside the chart, and even there they
 *  are counts, never a grade. The caller hides it for a `latest` Metric. */
export function GoalCard({ metric }: { metric: Metric }) {
  const goals = useGoals(metric.slug);
  const close = useCloseGoal();
  const remove = useDeleteGoal();
  const [editing, setEditing] = React.useState(false);

  const rows = goals.data ?? [];
  const open = rows.find((g) => g.ended_on === undefined);
  const past = rows.filter((g) => g !== open);
  // A Goal that started today has no day to keep, so stopping it is deleting it; the
  // server refuses a close on the start date and says so.
  const stoppable = open !== undefined && open.started_on < todayUTC();

  return (
    <Card className="px-4 py-3.5">
      <div className="flex items-center justify-between gap-3">
        <Eyebrow className="flex items-center gap-1.5">
          <Target className="size-3.5" /> Goal
        </Eyebrow>
        {!editing && (
          <Button variant="outline" size="sm" className="h-7 px-2.5 text-xs" onClick={() => setEditing(true)}>
            {open ? "Change" : "Set a goal"}
          </Button>
        )}
      </div>

      {open ? (
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 pt-2">
          <p className="text-sm">
            <span className="font-medium">{describeGoal(open, metric)}</span>
            <span className="text-muted-foreground">, since {formatDay(open.started_on)}</span>
          </p>
          <div className="flex items-center gap-1">
            {stoppable && (
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs text-muted-foreground"
                disabled={close.isPending}
                onClick={() => close.mutate(open.id)}
                title="Stop this Goal today. The days it held keep it."
              >
                Stop
              </Button>
            )}
            <DeleteButton goal={open} pending={remove.isPending} onDelete={(id) => remove.mutate(id)} />
          </div>
        </div>
      ) : (
        !editing && (
          <p className="pt-2 text-sm text-muted-foreground">
            No goal. Set one and Verve counts the days that meet it, day by day. It does not grade them.
          </p>
        )
      )}

      {editing && (
        <GoalForm metric={metric} current={open} onDone={() => setEditing(false)} />
      )}

      {past.length > 0 && (
        <div className="mt-3 space-y-1 border-t pt-3">
          {past.map((g) => (
            <div key={g.id} className="flex items-center justify-between gap-3 text-xs">
              <span>{describeGoal(g, metric)}</span>
              <span className="flex items-center gap-1 font-mono tabular-nums text-muted-foreground">
                {g.ended_on && formatDayRange(g.started_on, lastDayHeld(g.ended_on))}
                <DeleteButton goal={g} pending={remove.isPending} onDelete={(id) => remove.mutate(id)} />
              </span>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

/** DeleteButton removes one entry. It is how the past is corrected: a new Goal cannot
 *  be inserted into the history, so a mistyped one is deleted and set again. */
function DeleteButton({
  goal,
  pending,
  onDelete,
}: {
  goal: Goal;
  pending: boolean;
  onDelete: (id: number) => void;
}) {
  return (
    <Button
      variant="ghost"
      size="icon"
      className="size-6 text-muted-foreground"
      disabled={pending}
      aria-label="Delete this goal"
      title="Delete this entry, as if it had never been set"
      onClick={() => onDelete(goal.id)}
    >
      <Trash2 className="size-3" />
    </Button>
  );
}

const DIRECTIONS: { value: GoalDirection; label: string }[] = [
  { value: "at_least", label: "At least" },
  { value: "at_most", label: "At most" },
];

/** GoalForm sets a Goal, which closes the open one on its start date. The start date
 *  may go back in time ("since January") but never forward, and it cannot start
 *  inside an earlier Goal: the server refuses that and the message says to delete the
 *  entry instead, which is shown as written. */
function GoalForm({ metric, current, onDone }: { metric: Metric; current?: Goal; onDone: () => void }) {
  const set = useSetGoal();
  const [direction, setDirection] = React.useState<GoalDirection>(current?.direction ?? "at_least");
  const [input, setInput] = React.useState<GoalInput>({ value: "", hours: "", minutes: "" });
  const [startedOn, setStartedOn] = React.useState(todayUTC());
  const duration = isDuration(metric);
  const unit = goalUnit(metric);
  const value = storedGoalValue(input, metric);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (value === null) return;
    set.mutate(
      { metric: metric.slug, direction, value, started_on: startedOn },
      { onSuccess: onDone },
    );
  };

  const error = set.error instanceof ApiError
    ? (set.error.fields && Object.values(set.error.fields).join(" ")) || set.error.message
    : set.error
      ? "Could not save the goal."
      : null;

  return (
    <form className="mt-3 space-y-3 border-t pt-3" onSubmit={submit}>
      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1.5">
          <Label>Direction</Label>
          <div className="flex items-center rounded-md border p-0.5">
            {DIRECTIONS.map((d) => (
              <button
                key={d.value}
                type="button"
                onClick={() => setDirection(d.value)}
                className={cn(
                  "rounded px-2.5 py-1 text-xs transition-colors",
                  direction === d.value
                    ? "bg-secondary font-medium text-secondary-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {d.label}
              </button>
            ))}
          </div>
        </div>

        {duration ? (
          <div className="flex items-end gap-1.5">
            <div className="space-y-1.5">
              <Label htmlFor="goal-hours">Hours</Label>
              <Input
                id="goal-hours"
                autoFocus
                type="number"
                min={0}
                inputMode="numeric"
                className="w-20"
                value={input.hours}
                onChange={(e) => setInput({ ...input, hours: e.target.value })}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="goal-minutes">Minutes</Label>
              <Input
                id="goal-minutes"
                type="number"
                min={0}
                max={59}
                inputMode="numeric"
                className="w-20"
                value={input.minutes}
                onChange={(e) => setInput({ ...input, minutes: e.target.value })}
              />
            </div>
          </div>
        ) : (
          <div className="space-y-1.5">
            <Label htmlFor="goal-value">
              Value {unit && <span className="text-muted-foreground">({unit})</span>}
            </Label>
            <Input
              id="goal-value"
              autoFocus
              type="number"
              step="any"
              inputMode="decimal"
              className="w-32"
              value={input.value}
              onChange={(e) => setInput({ ...input, value: e.target.value })}
            />
          </div>
        )}

        <div className="space-y-1.5">
          <Label htmlFor="goal-start">Since</Label>
          <Input
            id="goal-start"
            type="date"
            max={todayUTC()}
            value={startedOn}
            onChange={(e) => setStartedOn(e.target.value)}
            required
          />
        </div>
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" size="sm" disabled={set.isPending || value === null}>
          {set.isPending ? "Saving…" : "Save"}
        </Button>
      </div>
    </form>
  );
}
