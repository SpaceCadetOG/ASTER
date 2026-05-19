# ASTER Trading Bot Documentation

This GitBook documents the current ASTER trading system as it runs today.

The production runtime is `cmd/live`. It consumes scanner output, applies the
risk shell, manages live and paper execution, handles Telegram operations, and
maintains the perp account through the current host runtime.

It covers:
- End-to-end architecture and module boundaries.
- How the `live` runtime scans, enters, protects, trails, exits, and re-enters.
- Manual trade handoff with `/manage SYMBOL y` and protection attach behavior.
- Funds maintenance, leverage, sizing, logging, and current host/local ops.
- CLI, HTTP, Telegram, and internal reference contracts.

Use [SUMMARY.md](./SUMMARY.md) as the table of contents.
