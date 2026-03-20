# Changelog

All notable changes to this project will be documented in this file.

## [1.0.1.0] - 2026-03-20

### Fixed
- Add HTTP status code checks on Feishu and WeChat API responses — non-2xx errors now return clear messages instead of misleading JSON parse errors

### Changed
- Parallelize 4 table syncs (articles, users, reads, shares) in RunFullSync for faster execution
- Update TODOS.md with new improvement items from eng review

### Added
- Shell script for full historical data sync (scripts/full-sync.sh)

## [1.0.0.0] - 2026-03-18

### Changed
- Rewrite backend from TypeScript/Express to Go single binary
- Replace PM2 process management with systemd service
- Replace node-cron with native Go time-based scheduler
- Simplify token management to lazy-loading (no background refresh timer)
- Fix all date calculations to use explicit Asia/Shanghai timezone
- Fix concurrent request limiter (replace buggy withConcurrency with goroutine+semaphore)
- Add HTTP client timeout (30s) for all external API calls
- Default server port changed to 3002 to match production

### Added
- 27 unit tests covering date utilities, field mappers, cursor I/O, dedup logic
- TODOS.md tracking known improvements

### Removed
- TypeScript/Node.js codebase (server/, client/, package.json, etc.)
- Stale TS implementation plan document
