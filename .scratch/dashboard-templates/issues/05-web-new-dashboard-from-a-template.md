Status: done
Blocked by: 03, 04

# 05: web: starting a Dashboard from a template, and the docs pass

## What

- **`useDashboardTemplates()`** in `web/src/hooks/use-dashboards.ts`, over
  `GET /v1/dashboard-templates`.
- **`new-dashboard-dialog.tsx`**: above the name field, a choice between
  "Empty" (selected by default) and each template, as a radio list: name,
  description, and "4 of 5 metrics have data" in muted text. The count is a
  fact and carries no colour; a template with no data is not dimmed.
  Selecting a template fills the name with the template's name unless the
  field was edited by hand. Submit sends `template` when one is selected, then
  navigates to the new Dashboard as today. The dialog widens from `max-w-sm`
  as the list needs; keyboard: arrows move in the list, Enter submits.
- Errors from the route show under the field they name.

- **Docs pass**:
  - `CONTRIBUTING.md`: "Contributing a Dashboard template", beside the palette
    section: the file format, the rules the test enforces, what a template must
    not carry (Goals, Annotations, Pins) and why, and the check on the reference
    Account in both Modes.
  - `README.md`: the feature list mentions the templates.
  - `ROADMAP.md`: a row in the shipped table ("Dashboard templates: Sleep, Cut
    and Endurance as declarative files, offered at creation, in the format a
    shared Dashboard will use"), and the "Sharing a Dashboard as JSON" entry
    says the format now exists and only export and import remain.

## Tests

- the dialog creates an empty Dashboard exactly as before when "Empty" is
  selected;
- selecting a template fills the name, and a hand-edited name survives
  switching template;
- submit sends `template` and navigates to the returned id.
- Verified in the browser preview on a fresh Account: the three templates are
  listed with their coverage, and each one opens populated.

## Comments

- The web suite tests pure modules only, so the draft logic (the pick, the name
  that follows it until typed, the request body, the coverage text) is
  `web/src/lib/new-dashboard.ts` with its tests; the dialog renders it.
- Enter on a radio would re-click it, so the list submits the form on Enter.
- Verified in the preview on a real Account: the three templates listed with
  their coverage, arrows move the pick, a typed name survives a change of pick,
  Enter from the list creates and navigates, each template opens populated, and
  the dialog fits a 375 px screen in light mode with no horizontal scroll.
- Found in passing, not changed here: on a phone the sidebar is hidden and
  nothing else opens "New dashboard", so a Dashboard cannot be created at all
  below the sidebar breakpoint.
