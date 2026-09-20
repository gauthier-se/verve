import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { api } from "@/lib/api";
import { isValidDay } from "@/lib/day";
import type { Bucket, Day } from "@/lib/types";

/** The query key every Day read shares, so a write that lands on a date can stale
 *  the page showing it without naming which date. */
export const DAY_KEY = ["day"];

/** useDay loads one date as an index (GET /v1/days/{date}, ADR 0043): its figures,
 *  the Night that woke into it, the workouts that started on it, and what the
 *  Account wrote on it. One call, because which Night belongs to a date and which
 *  side of midnight a workout falls on are read-path decisions and not a
 *  component's.
 *
 *  A date that is not one never asks: the server answers 422 and the page says so
 *  itself, which is cheaper than a round trip to be told what the string already
 *  shows. */
export function useDay(date: string) {
  return useQuery({
    queryKey: [...DAY_KEY, date],
    queryFn: () => api<{ day: Day }>(`/v1/days/${date}`).then((r) => r.day),
    enabled: isValidDay(date),
    retry: false,
  });
}

/** useDayNavigation turns a bucket into a destination, and only at day grain.
 *
 *  A week or a month has no Day, so the callback is undefined there and whatever
 *  holds it renders as it always did: a chart that navigated to the first date of a
 *  month would be presenting a guess as a destination. Handing back undefined rather
 *  than a no-op is what lets a caller show the difference in its cursor, before the
 *  click rather than after it.
 *
 *  It is one hook rather than a check at each call site because every day-grain
 *  bucket in the app goes to the same place: no Metric is privileged, and the
 *  condition is the grain the caller already knows (ADR 0043). */
export function useDayNavigation(bucket: Bucket | null | undefined) {
  const navigate = useNavigate();
  const go = useCallback(
    (date: string) => {
      if (!isValidDay(date)) return;
      void navigate({ to: "/days/$date", params: { date } });
    },
    [navigate],
  );
  return bucket === "day" ? go : undefined;
}
