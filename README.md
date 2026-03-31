# claude-usage

> **Disclaimer:** Unofficial, community-built tool. Not affiliated with, endorsed by, or sponsored by Anthropic. "Claude" and "claude.ai" are trademarks of Anthropic, PBC.
>
> **Background:** This tool was built based on findings from the [Claude Code source code leak](https://github.com/nirholas/claude-code) on March 31, 2026, which revealed how Claude Code authenticates via OAuth and how rate limit data is returned in API response headers.
>
> **100% vibecoded.** This entire tool was written by Claude. The author takes no responsibility for anything — use at your own risk.

See your Claude rate limits in real time. Session percentage, reset timers, weekly usage, overage status. Works anywhere — terminal, status bar, scripts.

Reads your existing Claude Code OAuth token from macOS Keychain and makes a single 1-token API call to get rate limit data from response headers.

## Quick install

**macOS (Apple Silicon):**
```bash
curl -fsSL https://github.com/Dede98/claude-usage/releases/latest/download/claude-usage-darwin-arm64 \
  -o /usr/local/bin/claude-usage && chmod +x /usr/local/bin/claude-usage
claude-usage install --daemon
```

**macOS (Intel):**
```bash
curl -fsSL https://github.com/Dede98/claude-usage/releases/latest/download/claude-usage-darwin-amd64 \
  -o /usr/local/bin/claude-usage && chmod +x /usr/local/bin/claude-usage
claude-usage install --daemon
```

**Linux (amd64 / arm64):**
```bash
curl -fsSL https://github.com/Dede98/claude-usage/releases/latest/download/claude-usage-linux-amd64 \
  -o /usr/local/bin/claude-usage && chmod +x /usr/local/bin/claude-usage
```

> Note: `install --daemon` uses macOS launchd. On Linux, run `claude-usage --daemon &` or create a systemd unit manually.

Drop `--daemon` if you only want manual refresh:

```bash
claude-usage install
```

## How it works

1. Reads your OAuth token from macOS Keychain (stored by Claude Code when you run `/login`)
2. Makes a minimal API call (1 output token, Haiku) — costs ~$0.00025/day
3. Parses `anthropic-ratelimit-unified-*` response headers for real-time usage data
4. Writes to `~/.claude/usage-data.json` for other tools to consume

No scraping. No cookies. Just one authenticated API call that returns your limits in the response headers.

## Usage

```bash
claude-usage              # Ping API, show formatted output
claude-usage --json       # Ping API, output JSON
claude-usage --watch      # Live refresh every 60s
claude-usage --daemon     # Background mode: ping + write to file
claude-usage --read       # Display from cached file (no API call)
claude-usage --status     # One-line summary for scripting
```

### Claude Code status bar

The `install` command automatically configures your Claude Code status bar:

```
██████████████░░░░░░ 67% session  resets 2h 14m
```

If you already have a statusline configured, it wraps the existing one — nothing breaks.

To configure manually, add to `~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "claude-usage statusline"
  }
}
```

Or to wrap an existing statusline:

```json
{
  "statusLine": {
    "type": "command",
    "command": "claude-usage statusline --wrap \"node ~/.claude/hooks/my-statusline.js\""
  }
}
```

Colors change automatically: green (< 50%), yellow (< 80%), red (>= 80%). Weekly usage appears when >= 75%.

### Background daemon

The `install --daemon` command sets up a launchd service for continuous polling:

```bash
# Manual daemon management
launchctl list com.claude-usage.daemon    # Check status
launchctl unload ~/Library/LaunchAgents/com.claude-usage.daemon.plist  # Stop
launchctl load ~/Library/LaunchAgents/com.claude-usage.daemon.plist    # Start
```

### Scripting

The `--status` flag outputs a single line, useful for tmux, polybar, or other status bars:

```bash
$ claude-usage --status
session 67%  weekly 31%  [allowed]  resets 2h14m
```

## Data format

The tool writes `~/.claude/usage-data.json` with this schema:

```jsonc
{
  "version": 1,
  "timestamp": 1711882800000,        // Unix ms when fetched
  "source": "api",
  "auth": {
    "subscription_type": "team",     // From keychain
    "rate_limit_tier": "default_claude_max_5x",
    "token_expires_at": 1774996245508
  },
  "limits": {
    "five_hour": {
      "utilization": 0.67,           // 0.0-1.0 (raw)
      "utilization_pct": 67,         // 0-100 (display)
      "resets_at": 1711890000,       // Unix epoch seconds
      "resets_at_iso": "2026-03-31T17:00:00+00:00"
    },
    "seven_day": {
      "utilization": 0.31,
      "utilization_pct": 31,
      "resets_at": 1712300000,
      "resets_at_iso": "2026-04-05T13:00:00+00:00"
    },
    "overage": {
      "status": "allowed",           // "allowed", "allowed_warning", "rejected"
      "utilization": 0.0
    },
    "status": "allowed",             // Overall: "allowed", "allowed_warning", "rejected"
    "representative_claim": "five_hour",
    "fallback": "available"          // "available" if model fallback exists
  },
  "error": null                      // null or error string
}
```

**Key fields for integrations:**

| Path | Type | Description |
|------|------|-------------|
| `version` | `number` | Always `1`. Check before parsing. |
| `timestamp` | `number` | Unix ms. Data older than 5 min is stale. |
| `limits.five_hour.utilization_pct` | `number` | Session usage 0-100% |
| `limits.five_hour.resets_at` | `number` | Unix epoch seconds |
| `limits.seven_day.utilization_pct` | `number` | Weekly usage 0-100% |
| `limits.status` | `string` | Overall rate limit status |
| `limits.overage.status` | `string` | Extra usage status |
| `auth.rate_limit_tier` | `string` | Subscription tier |
| `error` | `string\|null` | Error if fetch failed |

## Requirements

- macOS or Linux
- Claude Code installed and logged in (`claude /login`)
- macOS: reads OAuth token from Keychain automatically
- Linux: Keychain access requires `security` CLI (macOS only for now — Linux support for credential storage is planned)

## How it authenticates

It reads the OAuth access token that Claude Code stores in your macOS Keychain (service: `Claude Code-credentials`). No credentials are stored, copied, or transmitted by this tool — it reads from Keychain on every call and sends the token directly to `api.anthropic.com`.

The 1-token Haiku call exists only to trigger the API to return rate limit headers. The actual response content is discarded.

## Uninstall

```bash
claude-usage uninstall
```

This stops the daemon, restores your original statusline, and removes the symlink. The data file (`~/.claude/usage-data.json`) is preserved.

## Build from source

```bash
git clone https://github.com/Dede98/claude-usage.git
cd claude-usage
go build -o claude-usage .
./claude-usage install --daemon
```

## License

MIT
