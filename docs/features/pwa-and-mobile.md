# PWA and mobile

- **Status:** Planned
- **Milestone:** M2
- **Related:** [Single binary](platform-single-binary.md), [Today page](page-today.md)

## Summary

Verve is designed mobile first, because most daily checks happen on the iPhone. It is installable as a Progressive Web App (home screen icon, standalone display) without offline support: data lives on the server, and the app shows a clear screen when the server is unreachable.

## User stories

- As an iPhone user, I want to add Verve to my home screen and open it like an app.
- As a user, I want charts and tables to be usable with one hand.

## Scope

### In scope

- Responsive layouts for every page.
- Web app manifest, icons, standalone display, app shell caching.
- Touch interactions for charts.

### Out of scope

- Offline data access and background sync.
- Web push notifications (ntfy and other channels cover notifications).

## Functional requirements

- **FR-1**: Every page works from 360 px wide upwards, with no horizontal scrolling of the page.
- **FR-2**: Navigation: bottom tab bar on mobile (Today, Explore, Sleep, Workouts, More), sidebar on desktop.
- **FR-3**: Web app manifest with name, icons (including maskable and Apple touch icons), theme colors for light and dark, `display: standalone`, start URL `/`.
- **FR-4**: A service worker caches only the app shell and static assets for fast startup. API responses are never cached by the service worker.
- **FR-5**: When the API is unreachable, a dedicated screen explains it and retries automatically when the network returns.
- **FR-6**: Safe area insets are respected in standalone mode on iPhone.
- **FR-7**: Since standalone mode on iOS has no pull to refresh, pages provide a refresh control and refresh on focus.
- **FR-8**: Charts on touch devices: tap to show a tooltip, drag to scrub, pinch to zoom where zoom is supported.
- **FR-9**: Tables switch to card lists on narrow screens.
- **FR-10**: After a server upgrade, the app detects the new version and reloads at the next navigation.

## UX notes

- Touch targets of at least 44 px.
- Respect reduced motion preferences.

## Data and API notes

- No additional API.

## Edge cases

- iOS evicts PWA storage after inactivity: only the app shell is cached, so nothing important is lost.
- Session cookie expiry while the PWA is open: redirect to login, then back to the same page.

## Acceptance criteria

- [ ] Verve can be added to the iPhone home screen and opens without Safari chrome.
- [ ] Every page passes a manual check at 375 px wide in both themes.
- [ ] No API response is served from the service worker cache.

## Future ideas

- Home screen widgets through the iOS companion app.
