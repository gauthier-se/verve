// The Usual in words: where a bucket's value sits against the owner's own past.
//
// The band is computed server-side (ADR 0046) and never re-derived here. This only
// says it, and says it without a verdict: "above", "within" or "below" is a position,
// not a judgement, because a resting heart rate above its usual is neither good nor
// bad until the owner decides what it means.
import type { Bucket, Usual } from "./types";

/** usualWindow is how many buckets before a bucket its Usual is drawn from, per
 *  grain. It mirrors the engine's reference windows so the tooltip can say "from 17
 *  of the 28 days before" when some were missing. */
const usualWindow: Partial<Record<Bucket, { size: number; noun: string }>> = {
  day: { size: 28, noun: "days" },
  week: { size: 12, noun: "weeks" },
};

/** usualLine places value against its Usual: "above your usual 46–52 bpm (28 days
 *  before)". Bounds are inclusive, as the band is drawn. show formats each bound as
 *  the value itself is formatted; unit, when given, is named once after the range. */
export function usualLine(
  value: number,
  usual: Usual,
  bucket: Bucket,
  show: (v: number) => string,
  unit = "",
): string {
  const position = value > usual.high ? "above" : value < usual.low ? "below" : "within";
  const range = `${show(usual.low)}–${show(usual.high)}${unit ? ` ${unit}` : ""}`;
  const w = usualWindow[bucket];
  const basis = !w
    ? ""
    : usual.n < w.size
      ? ` (from ${usual.n} of the ${w.size} ${w.noun} before)`
      : ` (${w.size} ${w.noun} before)`;
  return `${position} your usual ${range}${basis}`;
}
