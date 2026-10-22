# 0006. AGPL-3.0 license

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

Verve is an open source, self-hosted health application. We want anyone to be free to run and modify it for themselves, but we do not want a third party to turn it into a closed health SaaS without contributing back.

## Decision

License Verve under AGPL-3.0. If a self-contained part becomes broadly useful on its own (for example the Apple Health export parser as a Go library), it may be published separately under a permissive license such as MIT.

## Consequences

- Self-hosters are unaffected.
- Anyone offering a modified Verve as a network service must publish their modifications.
- Some companies avoid AGPL dependencies. This is accepted: Verve is an application, not a library.

## Alternatives considered

- **MIT or Apache-2.0**: maximum reuse, including in closed SaaS products.
- **GPL-3.0**: does not cover network use, so a hosted fork could stay closed.
