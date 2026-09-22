Status: done

# 03: web: a stacked Panel that fits one column

- On a width-1 Panel, the Segment legend renders in its own row under the
  header, wrapping, instead of beside the settings button.
- Duration ticks: a pure formatter in `web/src/lib/format.ts` (`60h`,
  `45h 30m`, `50m`), tested; the axis uses it and never wraps.
- Verify the sleep Panel and the training time Panel at width 1 and 2.

## Comments

- The legend's place is chosen by the card's width through a container query
  (`.panel-card`, 36rem), not by the Panel's `width`: a wide Panel is one column
  on a phone. The old `hidden md:flex` hid the key entirely below `md`, against
  its own comment; it now shows on every width.
- A stacked Panel's plot starts one legend row lower than its neighbours'. The
  cards still match in height.
