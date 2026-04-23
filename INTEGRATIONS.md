# Integrating with claude-usage

> Note: the current CLI writes schema version 2 and can include both `claude` and `codex` providers. Claude data comes from Anthropic headers. Codex data comes from the local Codex app-server, with recent local session data as fallback. The examples below still focus on the original Claude-only shape and need a full refresh.

This document explains how to integrate with `claude-usage` in your own tools, scripts, or AI workflows. You can paste this entire document into an AI assistant's context to give it everything it needs to build an integration.

## Overview

`claude-usage` is a CLI tool that monitors Claude rate limits. It writes usage data to a JSON file at `~/.claude/usage-data.json`. Any tool can read this file to get real-time rate limit information.

**Architecture:** A background daemon polls the Anthropic API every 60 seconds and writes fresh data. Your integration just reads the JSON file — no API calls needed on your side.

## Reading usage data

### From the JSON file (recommended)

The daemon writes to `~/.claude/usage-data.json`. Read it directly:

```bash
# Bash
cat ~/.claude/usage-data.json | jq '.limits.five_hour.utilization_pct'
```

```python
# Python
import json, pathlib
data = json.loads(pathlib.Path("~/.claude/usage-data.json").expanduser().read_text())
session_pct = data["limits"]["five_hour"]["utilization_pct"]
```

```javascript
// Node.js
const fs = require("fs");
const path = require("path");
const data = JSON.parse(fs.readFileSync(path.join(process.env.HOME, ".claude", "usage-data.json"), "utf8"));
const sessionPct = data.limits?.five_hour?.utilization_pct;
```

```go
// Go
type UsageData struct {
    Version   int   `json:"version"`
    Timestamp int64 `json:"timestamp"`
    Error     *string `json:"error"`
    Limits    *struct {
        FiveHour *struct {
            UtilizationPct *int     `json:"utilization_pct"`
            ResetsAt       *float64 `json:"resets_at"`
        } `json:"five_hour"`
        SevenDay *struct {
            UtilizationPct *int     `json:"utilization_pct"`
            ResetsAt       *float64 `json:"resets_at"`
        } `json:"seven_day"`
        Status string `json:"status"`
    } `json:"limits"`
}
```

### From the CLI

```bash
# Formatted display
claude-usage

# Machine-readable JSON (triggers a fresh API call)
claude-usage --json

# One-line for scripting
claude-usage --status
# Output: session 67%  weekly 31%  [allowed]  resets 2h14m

# Read cached file without API call
claude-usage --read
```

## JSON schema (version 1)

```jsonc
{
  "version": 1,                          // Always check this equals 1
  "timestamp": 1711882800000,            // Unix ms — when data was fetched
  "source": "api",                       // Always "api"
  "auth": {
    "subscription_type": "team",         // "individual", "team", etc.
    "rate_limit_tier": "default_claude_max_5x",  // Plan tier identifier
    "token_expires_at": 1774996245508    // Unix ms — OAuth token expiry
  },
  "limits": {
    "five_hour": {                       // 5-hour rolling session window
      "utilization": 0.67,              // 0.0–1.0 (raw ratio)
      "utilization_pct": 67,            // 0–100 (for display)
      "resets_at": 1711890000,          // Unix epoch SECONDS (not ms)
      "resets_at_iso": "2026-03-31T17:00:00+00:00"
    },
    "seven_day": {                       // 7-day rolling window
      "utilization": 0.31,
      "utilization_pct": 31,
      "resets_at": 1712300000,
      "resets_at_iso": "2026-04-05T13:00:00+00:00"
    },
    "overage": {
      "status": "allowed",              // "allowed" | "allowed_warning" | "rejected"
      "utilization": 0.0                // 0.0–1.0
    },
    "status": "allowed",                // Overall: "allowed" | "allowed_warning" | "rejected"
    "representative_claim": "five_hour", // Which limit is most restrictive
    "fallback": "available"             // "available" if model fallback exists
  },
  "error": null                          // null when OK, string when failed
}
```

## Important details for integrators

### Staleness

- `timestamp` is Unix milliseconds. Compare with current time.
- Data older than **5 minutes** (300,000 ms) should be treated as stale.
- If stale, either show a "stale" indicator or run `claude-usage --json` to force a refresh.

```javascript
const STALE_MS = 5 * 60 * 1000;
const isStale = Date.now() - data.timestamp > STALE_MS;
```

### Error handling

- Always check `error` first. If non-null, `limits` may be null.
- Common errors: `"auth_failed"`, `"token_expired"`, `"HTTP 429"`, `"request_failed"`.

```python
if data.get("error"):
    # Show error state, don't try to read limits
    pass
elif data.get("limits") is None:
    # No limit data yet
    pass
else:
    # Safe to read limits
    pct = data["limits"]["five_hour"]["utilization_pct"]
```

### Timestamps

- `timestamp` is Unix **milliseconds**
- `resets_at` is Unix **seconds** (not milliseconds!)
- `resets_at_iso` is UTC ISO 8601 string

```javascript
// Reset countdown
const resetMs = data.limits.five_hour.resets_at * 1000; // seconds → ms
const minutesLeft = Math.floor((resetMs - Date.now()) / 60000);
```

### Status values

| Value | Meaning | Suggested action |
|-------|---------|-----------------|
| `"allowed"` | Normal usage | Green indicator |
| `"allowed_warning"` | Approaching limit | Yellow indicator |
| `"rejected"` | Rate limited | Red indicator, suggest waiting |

### Rate limit tier mapping

| Tier string | Display name |
|-------------|-------------|
| `*max_5x*` | Max 5x |
| `*max*` | Max |
| `*pro*` | Pro |
| `*free*` | Free |

## Integration examples

### Claude Code statusline

`claude-usage` can integrate directly into the Claude Code status bar. The `install` command handles this automatically, including wrapping any existing statusline.

```json
{
  "statusLine": {
    "type": "command",
    "command": "claude-usage statusline"
  }
}
```

To wrap an existing statusline (appends usage bar with ` │ ` separator):

```json
{
  "statusLine": {
    "type": "command",
    "command": "claude-usage statusline --wrap \"node ~/.claude/hooks/my-statusline.js\""
  }
}
```

The `statusline` subcommand:
- Reads `~/.claude/usage-data.json` (no API call)
- Outputs a colored progress bar: `██████░░░░ 67% session  resets 2h 14m`
- When `--wrap` is used, it runs the wrapped command first (passing stdin through), then appends the usage bar
- Shows weekly usage when >= 75%
- Colors: green (< 50%), yellow (< 80%), red (>= 80%)

### tmux status bar

```bash
# In .tmux.conf
set -g status-right '#(claude-usage --status)'
```

### Shell prompt

```bash
# In .zshrc or .bashrc
claude_usage_prompt() {
  local data=$(cat ~/.claude/usage-data.json 2>/dev/null)
  if [ -n "$data" ]; then
    echo "$data" | python3 -c "
import sys, json
d = json.load(sys.stdin)
if not d.get('error') and d.get('limits'):
    pct = d['limits']['five_hour']['utilization_pct']
    print(f'[claude:{pct}%]')
" 2>/dev/null
  fi
}
PROMPT='$(claude_usage_prompt) %~ $ '
```

### Raycast / Alfred script

```bash
#!/bin/bash
# Returns JSON for Raycast script command
data=$(claude-usage --json 2>/dev/null)
pct=$(echo "$data" | jq -r '.limits.five_hour.utilization_pct // "?"')
status=$(echo "$data" | jq -r '.limits.status // "unknown"')
echo "Session: ${pct}% | Status: ${status}"
```

### Conditional logic (pause when rate limited)

```python
import json, time, pathlib

def should_pause():
    """Check if we should pause work due to rate limits."""
    path = pathlib.Path("~/.claude/usage-data.json").expanduser()
    if not path.exists():
        return False

    data = json.loads(path.read_text())

    # Stale data — can't tell
    if time.time() * 1000 - data.get("timestamp", 0) > 300_000:
        return False

    if data.get("error"):
        return False

    limits = data.get("limits", {})

    # Rate limited
    if limits.get("status") == "rejected":
        return True

    # Session > 90% — slow down
    five_hour = limits.get("five_hour", {})
    if five_hour.get("utilization_pct", 0) > 90:
        return True

    return False
```

### macOS menu bar app (SwiftUI)

```swift
// Read usage data
let url = FileManager.default.homeDirectoryForCurrentUser
    .appendingPathComponent(".claude/usage-data.json")
let data = try JSONDecoder().decode(UsageData.self, from: Data(contentsOf: url))
let pct = data.limits?.fiveHour?.utilizationPct ?? 0
```

## Prerequisites

- `claude-usage` installed and daemon running (`claude-usage install --daemon`)
- Claude Code logged in (`claude /login`)
- File exists at `~/.claude/usage-data.json` after first daemon poll

## Refreshing data

| Method | When to use |
|--------|------------|
| Read `~/.claude/usage-data.json` | Default — daemon keeps it fresh |
| `claude-usage --json` | Force a fresh API call |
| `claude-usage --read` | Display cached data without API call |
| `claude-usage --status` | One-liner, triggers API call |

Most integrations should just read the JSON file. Only call the CLI directly if you need to force a refresh.
