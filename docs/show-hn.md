# Show HN: agent-htop — Live Terminal Dashboard for AI Agent Fleets

## The Problem

I was running 20+ Claude Code agents overnight for a side project. Woke up the next morning to a $40 API bill and no idea which agent caused it. The logs are buried in directories, cost is scattered across JSON, and there's no way to immediately kill a runaway agent without SSH'ing and hunting through processes.

## The Solution

**agent-htop** is `htop` for your AI agent fleet. A single Go binary that gives you real-time visibility and instant control:

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

**Features:**
- **Real-time cost tracking** per agent (see exactly what's burning money)
- **Instant kill** — press `K` on a runaway agent and it's gone
- **One-liner install:** `go install github.com/acunningham-ship-it/agent-htop/cmd/agent-htop@latest`
- **Works with Claude Code, Codex, Paperclip** — and single-binary deployment (11MB)

## How It Works

1. `agent-htop --company <id>` — connects to your local Paperclip instance
2. Live-streams agent status, tokens, cost, and elapsed time
3. Keyboard control: arrows/vim keys to navigate, `K` to kill, `q` to quit

The binary watches your Paperclip logs in real-time. No API calls needed once it starts (graceful fallback if offline).

## Why This Matters

AI agents are now as much of an operational liability as they are a feature. If you're running more than one agent in production—especially with Claude—you need visibility into what they're doing and how fast they're burning tokens.

This fills that gap.

## Links

- **GitHub:** https://github.com/acunningham-ship-it/agent-htop
- **Install:** `go install github.com/acunningham-ship-it/agent-htop/cmd/agent-htop@latest`
- **Homebrew (coming soon):** Will be available at acunningham-ship-it/homebrew-tap

---

Built in 4 weeks as a single Go binary. I'm 17 and building the tools I actually use.
