# Notifications

- **Status:** Planned
- **Milestone:** M3
- **Related:** [Plans and check-ins](goals-plans-and-check-ins.md), [Health Auto Export endpoint](ingestion-health-auto-export.md), [Ingestion pipeline](ingestion-pipeline.md), [Backups](backups.md)

## Summary

Verve sends notifications through channels homelabbers already use: ntfy, a generic webhook (which covers Discord, Slack, Home Assistant, n8n and more) and email over SMTP. Each user picks channels and events. Health values are not included in messages by default, since some channels go through third-party servers.

## User stories

- As a user, I want to know when my sync has stopped, before I discover a two-week gap.
- As a user on a plan, I want a reminder when my weekly check-in is ready.
- As an admin, I want to know when a backup fails.

## Scope

### In scope

- Channels: ntfy, webhook, SMTP email.
- User events and admin events.
- Delivery with retries and a delivery log.

### Out of scope

- Health anomaly alerts (future, with user-defined thresholds only).
- Web push, Apprise (future).
- Quiet hours (future).

## Functional requirements

- **FR-1**: Channels per user, several allowed:

  | Channel | Settings |
  |---|---|
  | ntfy | Server URL (default `https://ntfy.sh`), topic, optional access token, priority |
  | Webhook | URL, optional custom headers, optional HMAC secret (signature sent in `Verve-Signature`) |
  | Email | Uses the instance SMTP settings and the user's email address |

- **FR-2**: SMTP is configured by the admin (host, port, TLS mode, username, password, sender) through environment variables or the admin UI, with a test button.
- **FR-3**: User events:

  | Event | Trigger |
  |---|---|
  | `sync.stale` | No data received from an origin or automation for longer than the user's threshold (default 24 hours), sent once per stale period |
  | `import.completed`, `import.failed` | End of an import (XML imports by default, pushes only on failure by default) |
  | `plan.checkin_ready` | A weekly check-in is ready |
  | `plan.target_reached`, `plan.ended`, `plan.off_track` | Plan milestones |
  | `export.ready` | A data export is ready to download |

- **FR-4**: Admin events: `backup.failed`, `job.failed` (repeated failures), `storage.low` (free space below a threshold in the data directory).
- **FR-5**: Users subscribe each event to one or more channels. Sensible defaults are pre-selected when the first channel is added.
- **FR-6**: Message content: localized title and body, a link to the relevant page using `VERVE_PUBLIC_URL`. A per-user option "Include details" adds values (for example new targets at check-in); it is off by default.
- **FR-7**: Webhook payload: JSON with event type, timestamp, title, body, link, and a `data` object filled only when details are enabled.
- **FR-8**: Delivery runs as background jobs with 3 attempts and exponential backoff. The delivery log keeps the last 100 deliveries per user with status and error.
- **FR-9**: Each channel has a "Send test" button.

## UX notes

- Notifications settings page: channels list, then a matrix of events by channels.
- Warning next to ntfy when the public `ntfy.sh` server is used, explaining that messages transit through a third party.

## Data and API notes

- `notification_channels` (secrets encrypted), `notification_subscriptions`, `notification_deliveries`.
- Endpoints under `/api/v1/account/notifications/...` and `/api/v1/admin/smtp`.

## Edge cases

- Channel failing permanently (topic deleted, webhook 404): after 10 consecutive failures the channel is paused and the user sees a warning in the UI.
- Many imports in a short time: import notifications are coalesced.
- SMTP not configured: the email channel is not offered.

## Acceptance criteria

- [ ] A user without data for 24 hours receives exactly one `sync.stale` notification until data arrives again.
- [ ] Messages contain no health values unless "Include details" is enabled.
- [ ] Webhook signatures can be verified with the shared secret.

## Future ideas

- Apprise URLs.
- Quiet hours.
- Threshold alerts on metrics defined by the user.
