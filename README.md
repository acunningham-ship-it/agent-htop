# agent-htop

Live terminal dashboard for your AI coding sessions. Works with Claude Code today.

```
agent-htop  v0.2.0   sessions: 3   running: 2   cost today: $0.42   [q]uit

  NAME                           STATUS   MODEL         TOKENS IN  TOKENS OUT  COST     ELAPSED   LAST TOOL
  ────────────────────────────────────────────────────────────────────────────────────────────────────────
  -home-armani-projects-fnaf     running  claude-sonnet   142,310       8,421  $0.0234  12m 34s   Bash
  -home-armani-projects-api      running  claude-haiku     98,002       6,103  $0.0171   8m 12s   Write
  -home-armani-projects-web      idle     claude-haiku     43,118       2,940  $0.0041   3m 07s   Read
```

## Install

```bash
go install github.com/acunningham-ship-it/agent-htop/cmd/agent-htop@latest
```

Or via Homebrew:

```bash
brew install acunningham-ship-it/tap/agent-htop
```

Requires Go 1.21+.

## Quick start

```bash
# Run — auto-discovers your Claude Code sessions
agent-htop
```

That's it. agent-htop finds your `~/.claude/projects/**/*.jsonl` sessions automatically.

## Supported runtimes

| Runtime | Auto-discovered | Kill support | Cost tracking |
|---------|----------------|--------------|---------------|
| Claude Code | `~/.claude/projects/**/*.jsonl` | No (read-only) | Yes |
| Codex | `~/.codex/` | No | Planned |
| Paperclip | `--company <id>` | Yes | Yes |

## Keybindings

| Key | Action |
|-----|--------|
| `↑` / `k` | Navigate up |
| `↓` / `j` | Navigate down |
| `/` | Search / filter sessions |
| `f` | Cycle filter (all → error → running → idle) |
| `s` | Cycle sort (name → cost → heartbeat → spend rate) |
| `K` | Kill selected agent (Paperclip only — confirm with `Y`) |
| `P` | Pause selected agent (Paperclip only) |
| `R` | Resume selected agent (Paperclip only) |
| `r` | Force refresh |
| `q` / `Ctrl+C` | Quit |

Kill/pause/resume only work for Paperclip agents. Claude Code sessions are read-only.

## Advanced: Paperclip fleet monitoring

If you run a [Paperclip](https://paperclipai.com) agent fleet, pass your company ID to see all agents alongside your Claude Code sessions:

```bash
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96
```

With Paperclip you also get kill/pause control (`K` / `P` / `R` keys) and agent-level cost tracking.

### Discord webhook alerts

Post anomaly alerts to Discord when agents exhibit high spend, error streaks, or cost spikes:

```bash
export AGENT_HTOP_DISCORD_WEBHOOK="https://discord.com/api/webhooks/YOUR_ID/YOUR_TOKEN"
agent-htop --alert-level warn
```

| Anomaly | Severity | Trigger |
|---------|----------|---------|
| High Spend | warning | >$0.50/hr sustained over 15 min |
| Error Streak | critical | ≥3 failed runs in 10 min window |
| Cost Anomaly | critical | Today's spend >5× 7-day rolling average |

Rate-limited to max 1 alert per agent per 10 minutes.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--company` | *optional* | Paperclip company ID (enables Paperclip agents + kill/pause) |
| `--runtime` | `auto` | `auto`, `claude`, `codex`, `paperclip`, or `all` |
| `--api-url` | `http://localhost:3101` | Paperclip API URL |
| `--refresh-ms` | `2000` | Polling interval in milliseconds |
| `--alert-level` | `info` | Discord alert threshold: `info`, `warn`, `critical` |
| `--config` | *optional* | Config file path |
| `-v, --verbose` | `false` | Debug logging to stderr |
| `--version` | — | Show version and exit |

### Config file

Auto-created at `~/.config/agent-htop/config.toml` on first run:

```toml
[defaults]
refresh_ms = 2000
runtime = "auto"

[alerts]
spend_threshold = 0.50
error_streak = 3
```

### Environment variables

```bash
AGENT_HTOP_API_URL                  # Paperclip API URL
AGENT_HTOP_REFRESH_MS               # Refresh rate in ms
AGENT_HTOP_DISCORD_WEBHOOK          # Discord webhook URL
AGENT_HTOP_RUNTIMES                 # Comma-separated: claude,paperclip,codex
```

## Build from source

```bash
git clone https://github.com/acunningham-ship-it/agent-htop
cd agent-htop
make build
./agent-htop --version
```
