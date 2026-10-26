# Internationalization

- **Status:** In progress
- **Milestone:** M0
- **Related:** [User preferences and profile](user-preferences-and-profile.md), [Notifications](notifications.md), [ADR 0009](../decisions/0009-i18next.md)

## Summary

Verve is translated from day one, starting with English and French, so that the community can add languages without touching code. Numbers, dates and units are formatted according to the user's locale and unit preferences.

## User stories

- As a French-speaking user, I want the whole interface in French.
- As a contributor, I want to add a language by adding one translation file.

## Scope

### In scope

- Translation infrastructure in the SPA.
- English (reference) and French.
- Localized notifications and emails.
- Locale-aware formatting of numbers, dates and durations.

### Out of scope

- Right-to-left layouts (future, when a RTL language is contributed).
- Translating metric names coming from third-party apps (source names stay as received).

## Functional requirements

- **FR-1**: All user-facing strings in the SPA go through i18next (react-i18next) and come from translation files in i18next JSON format, `web/src/locales/<language>/<namespace>.json`, with English as the reference.
- **FR-2**: CI runs `pnpm i18n:check`: `i18next-cli extract --ci --dry-run` (fails when translation files are out of date with the keys used in code, without writing them) and `i18next-cli lint` (fails on hardcoded user-facing strings). It reports translation coverage per language with `i18next-cli status` in the job summary without failing. The brand name "Verve" is accepted as untranslated text by a lint plugin in `web/i18next.config.ts`. `task lint` runs the same check locally.
- **FR-3**: TypeScript types for translation keys are generated with `i18next-cli extract --with-types` (`pnpm i18n:extract`, part of `task generate`) into `web/src/i18n/resources.d.ts`, so an unknown key is a type error.
- **FR-4**: Missing translations fall back to English.
- **FR-5**: The language is a user preference, defaulting to the browser language when supported, otherwise English. Regional codes resolve to their language (`fr-FR` gives French). Until the server-side preference exists (M2), `localStorage["verve.language"]` set by hand overrides the browser language. Detection never writes to `localStorage`.
- **FR-6**: The login page follows the browser language.
- **FR-7**: Numbers, dates, times and relative times use the `Intl` APIs with the user's locale (`formatNumber` and `formatDate` in `web/src/i18n/format.ts`). Durations use a shared formatter, `formatDuration(seconds, lng)`, showing hours and minutes, minutes and seconds, or seconds alone, leaving out zero units, from translation keys (for example `7 h 32 min` in French, `7h 32m` in English, `45 s` and `45s`), so the output is the same in every browser.
- **FR-8**: API errors carry a stable `code`. The SPA maps codes to translated messages. The server never returns pre-translated error text for the SPA.
- **FR-9**: Notification and email templates exist per language and use the recipient's language.
- **FR-10**: Metric type display names and descriptions from the catalog are translatable.

## UX notes

- Language selector in preferences, showing each language in its own name (English, Français).

## Data and API notes

- Translation files live in `web/src/locales/<language>/<namespace>.json`. Server-side templates live next to the notification code.
- The SPA uses a single `common` namespace for now, imported statically. Plurals use i18next JSON v4 suffixes (`days_one`, `days_other`; French also has `days_many`). Keys not used in code yet are kept by `extract.preservePatterns`.
- `initI18n()` in `web/src/i18n/index.ts` initializes i18next before the first render and sets `<html lang>` on every language change.
- Tooling: i18next 26, react-i18next 17, i18next-cli 1.x (see [ADR 0009](../decisions/0009-i18next.md)).

## Edge cases

- Plural rules differ between languages: translation messages support plural forms.
- Long German or French strings must not break mobile layouts: components are tested with a pseudo-locale that expands strings. In development, `?lng=pseudo` shows every English text accented, about 40 percent longer and wrapped in brackets (`[Ṽéŕṽé~~]`). The pseudo-locale is not part of production builds.

## Acceptance criteria

- [ ] Switching language updates the whole UI without reload.
- [ ] No hardcoded user-facing string remains in components (lint rule).
- [ ] A new language can be added with one JSON file and one entry in the language list.

## Future ideas

- Hosted translation platform (Weblate) for community contributions.
- Right-to-left support.
