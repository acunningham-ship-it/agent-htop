# agent-htop

Real-time terminal dashboard for Claude Code sessions, Paperclip agent fleets, and more. Monitor costs, tokens, and agent status without leaving the terminal.

## Demo

[![Demo](https://asciinema.org/a/placeholder.svg)](https://asciinema.org/a/placeholder)

Or watch the recording in your terminal:

```bash
asciinema play agent-htop-demo.cast
```

```
agent-htop  v0.2.0   sessions: 3   running: 2   cost today: $0.42   [q]uit

  NAME                           STATUS   MODEL         TOKENS IN  TOKENS OUT  COST     ELAPSED   LAST TOOL
  ────────────────────────────────────────────────────────────────────────────────────────────────────────
  -home-armani-projects-fnaf     running  claude-sonnet   142,310       8,421  $0.0234  12m 34s   Bash
  -home-armani-projects-api      running  claude-haiku     98,002       6,103  $0.0171   8m 12s   Write
  -home-armani-projects-web      idle     claude-haiku     43,118       2,940  $0.0041   3m 07s   Read
```

**In the demo:** Navigate sessions with `j`/`k`, filter with `f`, sort by cost with `s`, and quit with `q`.

## Install

**From source:**

```bash
go install github.com/acunningham-ship-it/agent-htop/cmd/agent-htop@latest
```

**Via Homebrew:**

```bash
brew install acunningham-ship-it/tap/agent-htop
```

Requires Go 1.21+.

## Quick start

**For Claude Code sessions (no setup needed):**

```bash
agent-htop
```

Auto-discovers your sessions from `~/.claude/projects/**/*.jsonl`.

**For Paperclip agent fleets:**

```bash
agent-htop --company <company-id>
```

Displays your local Claude Code sessions alongside Paperclip agents with kill/pause control.

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

## Paperclip fleet monitoring

If you run a [Paperclip](https://paperclipai.com) agent fleet, agent-htop displays all agents in the same view with kill/pause/resume control:

```bash
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96
```

**Paperclip features:**
- Kill/pause/resume agents with `K` / `P` / `R` keys
- Agent-level cost tracking and spend anomaly detection
- Error streak monitoring
- Real-time heartbeat updates from the Paperclip API

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

## JSON snapshot mode

Query current fleet state as JSON for programmatic use:

```bash
agent-htop --once --json
```

Useful for:
- Supervisor agents querying fleet state
- Cron health checks
- MCP wrappers integrating agent-htop into other systems
- Shell scripts branching on agent status

**Example:**

```bash
# Get JSON snapshot of all agents
agent-htop --once --json > fleet.json

# Count total agents
agent-htop --once --json | jq '.Agents | length'

# Find running agents
agent-htop --once --json | jq '.Agents[] | select(.Status == "running")'

# Check for errors
agent-htop --once --json | jq '.Agents[] | select(.IsError == true)'

# Monitor only Claude Code sessions
agent-htop --once --json --runtime claude | jq '.Agents[].AgentName'

# Include Paperclip agents
agent-htop --once --json --company <company-id>
```

**Exit codes:**
- `0` — Success (one or more agents found)
- `1` — No agents found in specified scope

**JSON Schema:**

The output is a `FleetSnapshot` object:

```typescript
interface FleetSnapshot {
  Agents: AgentView[]
  UpdatedAt: string  // RFC3339 timestamp
}

interface AgentView {
  AgentID: string           // UUID or session ID
  AgentName: string         // Display name
  Status: "running" | "idle" | "error"  // Current status
  Model: string             // LLM model (e.g. "claude-sonnet-4-6")
  ElapsedMS: number         // Milliseconds since start of current run
  TotalCostUSD: number      // Cumulative cost today
  InputTokens: number       // Total input tokens
  OutputTokens: number      // Total output tokens
  LastTool: string          // Name of last tool_use executed
  IsError: boolean          // Whether current run has error
  Runtime: "paperclip" | "claude" | "codex"  // Runtime type
  Anomalies: AnomalyEvent[] | null  // Active anomalies (if any)
  Projection: CostProjection  // Cost projection data
}

interface AnomalyEvent {
  Type: string      // e.g. "high_spend", "error_streak", "cost_anomaly"
  Severity: string  // "info", "warning", "critical"
  Message: string
  Timestamp: string // RFC3339
}

interface CostProjection {
  SpentToday: number      // USD spent so far today
  ProjectedToday: number  // Projected spend if trend continues
  DailyAverage: number    // 7-day rolling average daily cost
  SpendRate: number       // Current USD per hour
  Color: "green" | "yellow" | "red"  // Alert color
  HoursRemaining: number  // Estimated hours until daily budget
}
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--once` | `false` | Run once and exit instead of launching interactive TUI |
| `--json` | `false` | Output JSON snapshot instead of TUI (requires `--once`) |
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

## Troubleshooting

**Paperclip API not reachable**

If you see "Warning: Paperclip API not reachable", check:
- Paperclip is running: `curl http://localhost:3101/health`
- API URL is correct: check `~/.config/agent-htop/config.toml` or use `--api-url`

The dashboard will still show agents from log files, but real-time updates and kill control will be unavailable.

**Missing Claude Code sessions**

Make sure Claude Code has run at least once and created log files in `~/.claude/projects/`. Check:
- `ls -la ~/.claude/projects/`
- `find ~/.claude/projects -name "*.jsonl" | head`
