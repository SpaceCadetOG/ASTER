# ASTER Trading Bot Documentation

This GitBook documents the current ASTER trading system as it runs today.

The production runtime is `cmd/live`. It is self-contained for market fetch,
scanner-worker passes, in-memory scanner snapshots, candidate enrichment,
strategy/risk routing, execution/protection orchestration, Telegram
operations, and perp-account maintenance through the current host runtime.

`cmd/long` and `cmd/short` are standalone scanner/dashboard/diagnostic
products. They are not required upstream runtime dependencies for `cmd/live`.

Autonomous entry logic exists in the codebase, but the current active runtime
posture is manual-only / ground-zero mode.

It covers:
- End-to-end architecture and module boundaries.
- How the `live` runtime scans, enters, protects, trails, exits, and re-enters.
- Manual trade handoff with `/manage SYMBOL y` and protection attach behavior.
- Funds maintenance, leverage, sizing, logging, and current host/local ops.
- CLI, HTTP, Telegram, and internal reference contracts.

Use [SUMMARY.md](./SUMMARY.md) as the table of contents.
