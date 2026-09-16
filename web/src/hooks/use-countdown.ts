import * as React from "react";

/** useCountdown counts `seconds` down to zero, one tick at a time, and returns
 *  the whole seconds left. null means nothing to count.
 *
 *  It counts against a deadline rather than by decrementing, so a backgrounded
 *  tab that gets its timers throttled still shows the true remaining wait when
 *  it comes back. restartKey restarts the count even when the duration is
 *  identical, which is the common case: two throttled attempts in a row both ask
 *  for the same wait. */
export function useCountdown(seconds: number | null, restartKey: unknown): number {
  const [remaining, setRemaining] = React.useState(0);

  React.useEffect(() => {
    if (seconds === null) {
      setRemaining(0);
      return;
    }
    const deadline = Date.now() + seconds * 1000;
    const tick = () => setRemaining(Math.max(0, Math.ceil((deadline - Date.now()) / 1000)));
    tick();
    const id = window.setInterval(tick, 500);
    return () => window.clearInterval(id);
  }, [seconds, restartKey]);

  return remaining;
}
