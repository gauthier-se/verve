Status: ready-for-agent
Blocked by: 01

# 02: dashtemplate, data: the Overview seeded from a file

## What

- **`internal/dashtemplate/templates/overview.json`**, embedded with
  `//go:embed templates/*.json`. Same five Panels, same order, same chart types,
  same `30d` range as `defaultPanels` today (ADR 0018).
- **`dashtemplate.All()`** returns the roster decoded once, in a fixed order
  (Overview first, then alphabetical by name), and **`Get(slug)`**. A file that
  fails to decode or to validate panics at init: the test below makes that
  unreachable in a release.
- **`dashtemplate.Seeded`** names the one template seeded at account creation
  (`overview`).
- **`internal/data/provision.go`** loses `defaultPanels` and
  `defaultDashboardName`. `seedDefaultDashboard` becomes an instantiation of a
  `dashtemplate.File` through the same `insertDashboard` / `insertPanel` path,
  inside the account-creation transaction. `data` must not import `api`; it
  imports `dashtemplate`, which imports only `catalog` and `timeaxis`.
- The instantiation helper, `(Models).CreateDashboardFromFile(ctx, accountID,
  file, name)`, is written here because the seed needs it, and `03` reuses it.
  It runs in one transaction (ADR 0038) and takes an optional name override.

## Tests

- **`templates_test.go`**: every embedded file decodes, validates, has a unique
  slug, and its slug matches its file name. This is the test a contributed
  template must pass.
- `provision_test.go` still proves a new Account gets "Overview" with its five
  Panels in order, now read from the file.
- `CreateDashboardFromFile` writes a Dashboard and its Panels with their
  buckets and widths, and writes nothing if a Panel insert fails.
