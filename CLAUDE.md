# claude-usage

## What this is
CLI tool that monitors Claude rate limits by reading the OAuth token from macOS Keychain (stored by Claude Code) and making a 1-token Haiku API call. Rate limit data comes from `anthropic-ratelimit-unified-*` response headers. No browser extension needed.

Based on the Claude Code source code leak (March 31, 2026) which revealed OAuth auth flow and rate limit header format. 100% vibecoded by Claude — author takes no responsibility.

## Architecture
Single Go binary. All functionality in one command:

- `main.go` — entry point, CLI routing
- `auth.go` — macOS Keychain credential reading
- `api.go` — 1-token API ping, header parsing, data types
- `data.go` — JSON file I/O (atomic writes)
- `display.go` — terminal formatting, progress bars, statusline bar
- `daemon.go` — background polling loop, watch mode
- `install.go` — install/uninstall, statusline wrapping, launchd plist, settings.json merge

## Data contract
Output file: `~/.claude/usage-data.json` (schema version 1).

Key paths:
- `limits.five_hour.utilization` — 0.0-1.0 session usage
- `limits.five_hour.utilization_pct` — 0-100 for display
- `limits.five_hour.resets_at` — unix epoch seconds
- `limits.seven_day.*` — same structure for weekly
- `limits.overage.status` — overage state
- `limits.status` — `allowed`, `allowed_warning`, `rejected`
- `auth.rate_limit_tier` — tier from keychain
- `error` — null or error string

## Rules
- Zero external Go dependencies — stdlib only
- Never store or transmit credentials — reads from Keychain on each call
- Atomic file writes (write .tmp then mv)
- Brand-safe: no Claude logos, no Anthropic brand colors, clear unofficial disclaimers
- Statusline `--wrap` must pass stdin through to wrapped command and not break existing output
