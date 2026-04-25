# Show HN: agent-htop v0.3 — Terminal Dashboard for Claude Code & AI Agent Fleets

Running Claude Code sessions or managing AI agent fleets and wondering what's actually happening? agent-htop is a live terminal dashboard that shows your AI sessions in real-time with costs, errors, system health, and intelligent incident detection.

## The Problem

I had three Claude Code sessions running overnight. Woke up to a larger bill than expected but had no quick way to see which session caused it—and no insight into *why* they were expensive. The logs are scattered across `~/.claude/projects/`, costs are buried in JSON, and there's no visibility into system health or anomalies until things break.

For teams running Paperclip agent fleets, the problem is worse: you're managing dozens of agents but can't quickly see their status, cost anomalies, or health issues without polling an API or digging through logs.

## The Solution

A single Go binary that auto-discovers Claude Code sessions and optionally connects to Paperclip agent fleets. Everything in one live-updating table with **automatic incident detection and postmortem generation**.

```
agent-htop  v0.3.0   sessions: 3   running: 2   cost today: $0.42   [q]uit

  NAME                           STATUS   MODEL         TOKENS IN  TOKENS OUT  COST     ELAPSED   LAST TOOL
  ────────────────────────────────────────────────────────────────────────────────────────────────────────
  -home-armani-projects-fnaf     running  claude-sonnet   142,310       8,421  $0.0234  12m 34s   Bash
  -home-armani-projects-api      running  claude-haiku     98,002       6,103  $0.0171   8m 12s   Write
  -home-armani-projects-web      idle     claude-haiku     43,118       2,940  $0.0041   3m 07s   Read
```

**Works offline for Claude Code** — no server required, just parses `~/.claude/projects/**/*.jsonl`.

**Optional Paperclip fleet support** — add `--company <id>` to monitor your AI agent fleet with kill/pause control (`K` / `P` keys).

## v0.3.0 Features

- ✅ Real-time cost tracking (Anthropic pricing built-in)
- ✅ Token usage per session / agent
- ✅ **Anomaly detection** (runaway spend, error streaks, filesystem full alerts)
- ✅ **Automatic incident postmortem generation** (captures session state when errors occur)
- ✅ **Task queue management** (view pending tasks in Paperclip queues)
- ✅ **System health monitoring** (CPU, memory, disk, GPU, network per-filesystem alerts)
- ✅ **MCP tools** (process listing, killing, queue operations)
- ✅ Optional Discord alerts
- ✅ 7-day cost history and spend rate projections
- ✅ Kill / pause / resume agents (Paperclip only)
- ✅ Filter and sort by name, cost, status, spend rate
- ✅ **Incremental log parsing** (fixed scanner errors for large logs)

## Why v0.3?

- **Incident detection with automatic postmortem**: When a session fails or gets expensive, agent-htop now automatically writes a structured postmortem capturing error context, cost spike details, and recovery state. No more manually digging through logs after an incident.
- **Task visibility**: For Paperclip users, see exactly what tasks are queued and which agent will handle them. Useful for detecting stuck queues or load imbalances.
- **Health checks**: System-level monitoring (disk full, GPU thermal, network down) that ties directly to agent anomalies. Know if your expensive run was due to a misconfigured retry loop or a real disk issue.

## Installation

```bash
go install github.com/acunningham-ship-it/agent-htop/cmd/agent-htop@latest
# or
brew install acunningham-ship-it/tap/agent-htop
```

**Run:**

```bash
agent-htop   # auto-discovers Claude Code sessions
agent-htop --company <id>   # also add Paperclip agents  
```

Watch the demo: `asciinema play agent-htop-demo.cast` (20 seconds)

Single Go binary, ~11MB. Linux and macOS. No external dependencies for Claude Code mode.

**GitHub:** https://github.com/acunningham-ship-it/agent-htop
