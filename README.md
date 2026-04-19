# agent-htop

`htop` for AI agent fleets. A live terminal dashboard that shows every running Paperclip agent with token burn, cost, current tool, elapsed time, and a kill key.

```
agent-htop  v0.1.0   agents: 5   running: 3   cost today: $0.42   [q]uit [K]ill

  NAME                STATUS   MODEL        TOKENS IN  TOKENS OUT  COST     ELAPSED   LAST TOOL
  ──────────────────────────────────────────────────────────────────────────────────────────────
  BackendEngineer     running  haiku-4-5      142,310       8,421  $0.0234  12m 34s   Bash
  TUIEngineer         running  haiku-4-5       98,002       6,103  $0.0171   8m 12s   Write
  QALaunch            running  gemini-flash    43,118       2,940  $0.0000   3m 07s   Read
  Researcher          idle     gemini-flash    89,442       5,217  $0.0000       -    -
  CEO                 idle     sonnet-4-6      21,003       1,882  $0.0412       -    -
```

## Install

```bash
go install github.com/acunningham-ship-it/agent-htop/cmd/agent-htop@latest
```

Requires Go 1.21+.

## Usage

```bash
# Required: specify company ID
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96

# With options
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96 \
  --api-url http://localhost:3101 \
  --runtime all \
  --refresh-ms 2000 \
  -v  # verbose logging
```

The binary auto-discovers your Paperclip log directory at `~/.paperclip/instances/default/data/run-logs/` and polls by default every 2 seconds.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--company` | *required* | Company ID from Paperclip |
| `--api-url` | `http://localhost:3101` | Paperclip API URL |
| `--runtime` | `all` | Runtime types to monitor (comma-separated): `paperclip`, `claude`, `codex`, `all` (default) |
| `--refresh-ms` | `2000` | Polling interval in milliseconds |
| `--alert-level` | `info` | Minimum alert level for Discord: `info`, `warn`, `critical` |
| `--config` | *optional* | Config file path |
| `-v, --verbose` | `false` | Enable debug logging to stderr |
| `--version` | - | Show version and exit |
| `--help` | - | Show help and exit |

### Discord Webhook Alerts

agent-htop can post anomaly alerts to Discord when agents exhibit concerning behavior (high spend, error streaks, cost anomalies). This keeps you informed even when the terminal isn't open.

#### Setup

1. Create a Discord webhook in your server:
   - Right-click the channel → Edit Channel → Integrations → Webhooks → New Webhook
   - Copy the webhook URL

2. Set the environment variable and run agent-htop:
   ```bash
   export AGENT_HTOP_DISCORD_WEBHOOK="https://discord.com/api/webhooks/YOUR_WEBHOOK_ID/YOUR_WEBHOOK_TOKEN"
   agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96 --alert-level warn
   ```

#### Alert Types

| Anomaly | Emoji | Severity | Trigger |
|---------|-------|----------|---------|
| **High Spend** | 💸 | warning | Agent spending >$0.50/hr sustained over 15 min |
| **Error Streak** | ⚠️ | critical | ≥3 failed runs in 10 min window |
| **Cost Anomaly** | 🚨 | critical | Today's spend >5× the 7-day rolling average |

#### Alert Levels

- `info` — All alerts (default)
- `warn` — Warnings and critical only (excludes info-level alerts)
- `critical` — Critical alerts only

Rate limiting: **max 1 alert per agent per 10 minutes** to prevent Discord spam.

#### Example Discord Message

```
💸 BackendEngineer
High spend detected: $0.75/hr (threshold: $0.50/hr)

Type: HIGH_SPEND
Severity: warning
Detected: 2026-04-19T21:30:45Z
```

## Keybindings

| Key | Action |
|-----|--------|
| `↑` / `k` | Navigate up |
| `↓` / `j` | Navigate down |
| `/` | Search / filter agents |
| `f` | Cycle filter (all → error → running → idle → paused) |
| `s` | Cycle sort (name → cost → heartbeat → spend rate) |
| `K` | Kill selected agent (with confirm: press `Y` to confirm) |
| `P` | Pause selected agent |
| `R` | Resume selected agent |
| `r` | Force refresh |
| `q` / `Ctrl+C` | Quit |

## Supported runtimes

agent-htop works with multiple execution environments. Use the `--runtime` flag to select which logs to monitor:

| Runtime | Log path | Status | Details |
|---------|----------|--------|---------|
| **Paperclip** | `~/.paperclip/instances/default/data/run-logs/<co>/<agent>/` | ✅ Fully supported | NDJSON format with full metrics (tokens, cost, tool calls) |
| **Claude Code** | `~/.claude/projects/*/` | ✅ Fully supported | JSONL format with session metadata and token usage |
| **Codex** | TBD | 🔬 Research phase | Format under investigation; stub parser in place for future implementation |

### Using with Claude Code (not Paperclip)

If you use Claude Code standalone (without Paperclip), you can still monitor your agent runs. Claude Code logs are stored in `~/.claude/projects/*/` and include session metadata, token usage, and execution details.

```bash
# Monitor Claude Code sessions only
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96 --runtime claude

# Monitor both Paperclip and Claude Code (default)
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96 --runtime all

# Monitor specific runtimes (comma-separated)
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96 --runtime paperclip,claude
```

**How Claude Code Logs Work**
- Logs are stored in `~/.claude/projects/<project-name>/*.jsonl`
- Each `.jsonl` file is a Claude Code session
- agent-htop discovers and parses these automatically
- Project paths are used as agent identifiers for display
- Claude Code logs don't have a company context, so all sessions are visible regardless of the `--company` flag
- Use `--runtime=paperclip` if you want only Paperclip logs for a specific company

**Mapping Project Paths to Agent Names**
- agent-htop tries to resolve Paperclip agent names via the API
- If a Claude project name doesn't match a Paperclip agent, the project path is used as the agent ID
- Example: `~/.claude/projects/-home-armani-projects-agent-htop/` → agent ID: `-home-armani-projects-agent-htop`

### Codex Support (Research Phase)

Codex support is currently in research phase. A stub parser is in place to document the expected format and provide guidance for future implementation.

```bash
# Codex is not yet available
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96 --runtime codex
# Error: Codex parser not yet implemented
```

**What We Need to Know**
- Where Codex stores execution logs (directory path, file naming)
- Log format (JSONL, JSON, plain text, or other)
- Available fields (token usage, model name, execution time, status, etc.)
- How to map Codex runs to agent identities

Once Codex documentation is available, the parser will:
1. Follow the same pattern as Paperclip and Claude Code parsers
2. Normalize logs into the unified `AgentRun` struct
3. Integrate seamlessly with the fleet dashboard

## Build from source

```bash
git clone https://github.com/acunningham-ship-it/agent-htop
cd agent-htop
make build
./agent-htop --version
```

The `Makefile` embeds version info via ldflags:

```bash
make build      # Build with version injection
make test       # Run tests
make clean      # Clean artifacts
```

Version is injected as: `-X main.Version=v0.1.0 -X main.GitSHA=<commit>`
