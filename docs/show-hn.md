# Show HN: agent-htop — Live Dashboard for Claude Code & AI Agent Fleets

Running Claude Code sessions or AI agents and wondering where the compute budget went? I built agent-htop—a single-command terminal dashboard that shows all your AI sessions in real-time with costs, tokens, and status updates.

## The Problem

I had three Claude Code sessions running overnight on a side project. Woke up to a larger bill than expected but had no quick way to see which session caused it. The logs are scattered across `~/.claude/projects/`, costs are buried in JSON, and there's no live view of what's actually running.

## The Solution

A single Go binary that auto-discovers Claude Code sessions and optionally connects to Paperclip agent fleets. Everything in one table, updating in real-time.

```
agent-htop  v0.2.0   sessions: 3   running: 2   cost today: $0.42   [q]uit

  NAME                           STATUS   MODEL         TOKENS IN  TOKENS OUT  COST     ELAPSED   LAST TOOL
  ────────────────────────────────────────────────────────────────────────────────────────────────────────
  -home-armani-projects-fnaf     running  claude-sonnet   142,310       8,421  $0.0234  12m 34s   Bash
  -home-armani-projects-api      running  claude-haiku     98,002       6,103  $0.0171   8m 12s   Write
  -home-armani-projects-web      idle     claude-haiku     43,118       2,940  $0.0041   3m 07s   Read
```

**Works offline for Claude Code** — no server required, just parses `~/.claude/projects/**/*.jsonl`.

**Optional Paperclip fleet support** — add `--company <id>` to monitor your AI agent fleet with kill/pause control (`K` / `P` keys).

## Features

- ✅ Real-time cost tracking (Anthropic pricing built-in)
- ✅ Token usage per session / agent
- ✅ Anomaly detection (runaway spend, error streaks)
- ✅ Optional Discord alerts
- ✅ 7-day cost history (`H` key toggle)
- ✅ Kill / pause / resume agents (Paperclip only)
- ✅ Filter and sort by name, cost, status, spend rate

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

Built as a single Go binary with bubbletea TUI. Works on Linux and macOS. ~11MB.

**GitHub:** https://github.com/acunningham-ship-it/agent-htop
