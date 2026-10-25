# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) and the project
adheres to [Semantic Versioning](https://semver.org/lang/pt-BR/).

## [Unreleased]

### Added

- Nothing yet.

## [0.1.0] — 2026-10-25

### Added

- Official TSE EA20 JSON adapter validated against the 2026 presidential runoff,
  2026 first round, 2026 state runoff and a 2026 municipal supplementary result.
- Conditional requests with ETag / Last-Modified, HTTP 304, retries with
  exponential backoff and `Retry-After` handling.
- Normalized snapshot model with exact integer percentages and conservative
  completion semantics based on the official `tf` flag.
- Change detector with revision detection for decreasing values and
  deduplication by deterministic event IDs.
- Durable JSON state store with atomic replacement and a persistent Telegram
  outbox.
- Telegram HTML messages, long-message splitting, retries, rate-limit handling
  and token redaction.
- Short, user-focused Telegram messages with the official human results page.
- Optional PNG bar charts sent as Telegram photos (`TELEGRAM_CHARTS`).
- Brazil map dashboard coloured by the winning candidate in each state
  (`TELEGRAM_MAPS`), using the official IBGE state mesh.
- In-TUI UF selector and terminal map of state winners, shown on the initial
  wide layout and expandable with `m`.
- Stable candidate colours: Lula/PT red, Bolsonaro/PL blue, regardless of the
  current position.
- TUI dispute panel with current percentage-point/vote lead and a persisted
  local trend chart (up to 240 compact points).
- Bubble Tea / Lip Gloss dashboard with keyboard and mouse interaction,
  responsive layout, details, event history, on-demand UF breakdown and
  headless mode.
- Optional read-only `/healthz` and `/status` HTTP endpoints.
- Dockerfile, Compose file, Makefile, GitHub Actions CI and documentation.
- Automated tests with real, sanitized TSE fixtures and local HTTP servers.

### Known limitations

- The TSE result files are not a documented, versioned public API; endpoints can
  change without notice.
- Signed `.jws` variants are documented but signature verification is not
  implemented; the collector reads the equivalent `.json` files.
- State files support one running writer per election state path.
- Switching elections requires a different state path.
