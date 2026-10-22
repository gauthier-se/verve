# Internationalization

- **Status:** Planned
- **Milestone:** M0
- **Related:** [User preferences and profile](user-preferences-and-profile.md), [Notifications](notifications.md)

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

- **FR-1**: All user-facing strings in the SPA come from translation files, one JSON file per language, with English as the reference.
- **FR-2**: CI fails when a key used in code is missing from the English file, and reports (without failing) keys missing in other languages.
- **FR-3**: Missing translations fall back to English.
- **FR-4**: The language is a user preference, defaulting to the browser language when supported, otherwise English.
- **FR-5**: The login page follows the browser language.
- **FR-6**: Numbers, dates, times and relative times use the `Intl` APIs with the user's locale. Durations use a shared formatter (for example `7 h 32 min` in French, `7h 32m` in English).
- **FR-7**: API errors carry a stable `code`. The SPA maps codes to translated messages. The server never returns pre-translated error text for the SPA.
- **FR-8**: Notification and email templates exist per language and use the recipient's language.
- **FR-9**: Metric type display names and descriptions from the catalog are translatable.

## UX notes

- Language selector in preferences, showing each language in its own name (English, Français).

## Data and API notes

- Translation files live in `web/src/locales/`. Server-side templates live next to the notification code.

## Edge cases

- Plural rules differ between languages: translation messages support plural forms.
- Long German or French strings must not break mobile layouts: components are tested with a pseudo-locale that expands strings.

## Acceptance criteria

- [ ] Switching language updates the whole UI without reload.
- [ ] No hardcoded user-facing string remains in components (lint rule).
- [ ] A new language can be added with one JSON file and one entry in the language list.

## Open questions

- Library choice: Paraglide JS (compiled, type-safe, tree-shakable messages) or i18next (largest ecosystem, broad translation platform support). To decide at the start of M0, considering compatibility with a hosted translation platform such as Weblate.

## Future ideas

- Hosted translation platform (Weblate) for community contributions.
- Right-to-left support.
