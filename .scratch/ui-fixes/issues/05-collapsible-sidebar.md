Status: ready-for-agent

# 05: web: the sidebar collapses to an icon rail

- A per-device preference in `localStorage` (read and written in try/catch,
  like the Appearance), default expanded.
- Collapsed: 56 px, the mark, Now, one entry per Dashboard (its initial, the
  name in a tooltip), New dashboard, the pins as icons, the tools as icons, and
  the account controls stacked. Every icon has a tooltip and an aria-label.
- A toggle button at the top of the sidebar, and the `[` hotkey
  (react-hotkeys-hook, ADR 0013).
- The pure part (reading and writing the preference, a Dashboard's initial) in
  `web/src/lib/`, tested.
- Verify: toggle, reload keeps the state, every destination still reachable.
