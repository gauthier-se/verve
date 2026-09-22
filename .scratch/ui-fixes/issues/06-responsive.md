Status: done

# 06: web: Dashboards on a phone, a tab bar that fits, a header in two rows

- **Dashboards tab** below `lg`: opens a sheet (a Dialog anchored at the
  bottom) listing the Dashboards, the current one marked, and "New dashboard".
- **Tab bar**: Now, Dashboards, Data, Workouts, More. "More" opens the same kind
  of sheet with the remaining tools, from `TOOLS`, so the lists cannot drift.
- **Dashboard header** at 375 px: title and actions on one row, the range and
  the comparison on the next; the comparison collapses to an icon button with a
  menu when it does not fit.
- **Panel summary** on a phone: the Baseline delta on its own line.
- Verify at 375, 768 and 1024 px: no horizontal scroll anywhere, every
  Dashboard reachable, a Dashboard can be created, nothing overlaps.

## Comments

- The comparison collapses to its icon in the trigger on a phone (accent while a
  comparison is on), rather than to a separate icon button: the same Select, one
  control. shadcn's `[&>span]:line-clamp-1` on the trigger overrides a plain
  `hidden`, hence `max-sm:!hidden` on the label.
- The Notes toggle, the Add panel button and the Custom range button keep their
  icon and drop their word on a phone; the range picker shows one month there.
- The delta line break is keyed on the viewport (`max-sm:`), not on the card:
  a card query would also catch the narrow cards of a two-column grid and undo
  `02`'s alignment.
- The old tab bar comment argued for sideways scrolling over a More menu. At
  eight entries it hid two of them off screen at 375 px, which is the failure it
  meant to prevent; the comment is rewritten.
- Verified at 375 and 768 px: no horizontal scroll, header in two rows, every
  Dashboard reachable from the sheet, New dashboard from the sheet.
