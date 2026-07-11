# Web onboarding: non-developer-friendly first run

## Goal

Let a non-developer stand up and use Verve from the browser alone — create the
first account, import an Apple Health export, and land on a populated dashboard —
without ever touching a shell.

## Scope

Three vertical slices, each demoable on its own:

1. **Seeded default dashboard + shared account-creation path** — extract account
   creation into one reusable path and seed an "Aperçu" template dashboard, so no
   account ever faces an empty app. Delivered first through the CLI, where it is
   verifiable today.
2. **First-run bootstrap** — a web account-creation screen active only while the
   instance has zero accounts; auto-login; web signup closes (server-enforced)
   once the first account exists.
3. **Web self-service import** — upload an Apple Health `.zip` from the browser, a
   background import job with real two-phase progress and a final report, and an
   empty-state CTA that fills the seeded dashboard.

## Out of scope

Web invitations for N+1 accounts (stay CLI), browser folder / `export.xml` upload
(stays CLI), resumable/chunked upload, persisted job state, a sleep panel
(`duration_by_state` not yet wired into the series path).

## Decisions

See ADR 0016 (web self-service import), ADR 0017 (first-run bootstrap, closed
signup), ADR 0018 (seeded default dashboard). Glossary: **Import job**,
**Bootstrap**, **Dashboard template** in CONTEXT.md.

## Issues

- `issues/01-seeded-default-dashboard.md`
- `issues/02-web-bootstrap-account.md`
- `issues/03-web-self-service-import.md`
