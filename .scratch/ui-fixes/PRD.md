# PRD: UI fixes from use

## Goal

A list of problems met while using Verve, verified in the browser preview on a
real Account on 2026-09-22. Four are bugs, one is a decision reversed (Goals on
`latest` Metrics, ADR 0048), two are small features the frame was missing
(collapsing the sidebar, a Panel in full screen).

## What was found

1. **The login mark is not the product mark.** `auth-screen.tsx` draws lucide's
   generic `Activity` icon in the badge; the sidebar draws `Mark`, the rising
   stroke ending on a plotted point that the favicon also uses.
2. **Panels in one row are not the same height.** A width-2 Panel is `h-80` and a
   width-1 Panel `h-72`, so a wide Panel beside a narrow one leaves them
   misaligned. Inside equal cards, a summary that wraps to two lines ("106 186,5
   kcal ↓ 65 % (vs 307 305,7)") pushes its chart down, so neighbouring plots do
   not line up either.
3. **The sleep Panel overlaps itself at width 1.** Its five-Stage legend sits in
   the header beside the title and covers it ("Sleep" under "Deep"), and the
   duration ticks ("60h 0m") wrap onto two lines and collide.
4. **No Goal on body mass.** Deliberate in ADR 0044; reversed in ADR 0048.
5. **The sidebar cannot be closed.** Always 240 px at `lg` and above.
6. **Responsive layout.** Below `lg` (1024 px) the sidebar is replaced by a top
   bar and a tab bar, and:
   - nothing lists the Dashboards or opens "New dashboard": the Dashboards tab
     forwards to the first one, so a second Dashboard cannot be reached and a new
     one cannot be made;
   - the tab bar holds eight destinations and scrolls sideways on a phone
     ("Workouts" cut off at 375 px);
   - the Dashboard header takes three rows at 375 px, and the range control and
     the comparison select wrap unevenly at intermediate widths;
   - a Panel summary with a Baseline wraps into a tangle at 375 px
     ("(vs Jul 20 6 379,4 96,9 k)").
7. **No way to look at one Panel large.**

## Decisions

- **Mark** moves to its own module and is the one mark on every screen.
- **One height per grid row.** A card fills its grid cell (`h-full`) with a
  minimum per width, so a row takes its tallest Panel's height and every card in
  it matches. The summary keeps to one line and truncates what does not fit (the
  full figure stays in its title attribute), so plots in a row start at the same
  height.
- **A legend that does not fit the header drops under it.** The Stage and
  Activity legends move to their own row below the title on a width-1 Panel.
  Duration ticks are compact ("60h", "45h 30m" only when minutes are non-zero)
  and never wrap.
- **Goals on every Metric** (ADR 0048).
- **The sidebar collapses to an icon rail**, 56 px: the mark, Now, one entry per
  Dashboard (its initial), the tools, and the account controls as icons, each
  with a tooltip. The state is a per-device preference in `localStorage`, like
  the Appearance, never server data. A button at the top toggles it, and so does
  `[` from the keyboard.
- **Responsive**:
  - below `lg`, the Dashboards tab opens a sheet listing the Dashboards and a
    "New dashboard" entry, instead of forwarding to the first one;
  - the tab bar keeps five destinations (Now, Dashboards, Data, Workouts, More)
    and "More" opens the rest (Cross-metric, History, Plan, Import & export), so
    it never scrolls;
  - the Dashboard header lays out in two rows on a phone (title and actions, then
    the range and the comparison sharing one row, the comparison as an icon
    button when it does not fit);
  - the summary's Baseline delta goes to its own line on a narrow card instead of
    wrapping mid-figure.
- **Full screen is a dialog over the Dashboard**: an expand button in each
  Panel's header opens the same Panel, same Metrics, range, bucket and Baseline,
  in a near-full-viewport dialog. Escape closes it. It is a view, never a copy:
  nothing is stored, and the Panel settings stay on the card.

## Issues

1. `01-login-mark.md`: web: the product mark on the auth screens
2. `02-panel-heights.md`: web: one height per row, and plots that line up
3. `03-stacked-panel-at-width-1.md`: web: a stacked Panel that fits one column
4. `04-goals-on-latest.md`: api, web: a Goal on every Metric (ADR 0048)
5. `05-collapsible-sidebar.md`: web: the sidebar collapses to an icon rail
6. `06-responsive.md`: web: Dashboards on a phone, a tab bar that fits, a header
   in two rows
7. `07-panel-full-screen.md`: web: a Panel in full screen

## Out of scope

- A time zoom (drag to select a window on a chart).
- Resizing the sidebar to an arbitrary width.
- Storing the sidebar state per Account.
