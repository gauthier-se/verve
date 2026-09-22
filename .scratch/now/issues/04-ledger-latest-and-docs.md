Status: ready-for-agent
Blocked by: 02

# 04: query, web: the Ledger's latest value unbounded, and the docs pass

## What

- **Ledger** (`internal/query/ledger.go`): `LedgerRow.Latest` comes from
  `Engine.Latest` instead of the last Point of the 30-day series, and carries
  `age_days`. The week and month figures and the delta are untouched: those are
  windows (ADR 0021).
- **Data page**: the latest column prints the date's age when it is not today, so
  a stale row reads as stale instead of as "—".
- **Docs**: a README line in the features list, and a ROADMAP row ("Now:
  Freshness per Account and Source, and each Pin at its latest value"). ADR 0045
  and the CONTEXT.md entries already landed with the PRD.

## Tests

- a Metric that stopped 35 days ago keeps its latest value in the Ledger, with
  its date, and has no week or month figure;
- the Ledger's latest and the Now card for the same Metric agree.
