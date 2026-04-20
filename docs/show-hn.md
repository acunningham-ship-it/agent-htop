# Show HN: agent-htop — live terminal dashboard for Claude Code sessions

I was running several Claude Code agents overnight on a side project. Woke up to a larger bill than expected and no quick way to see which session caused it. The logs are buried in `~/.claude/projects/`, costs are scattered across JSON, and there's no live view.

So I built agent-htop.

```
agent-htop  v0.2.0   sessions: 3   running: 2   cost today: $0.42   [q]uit

  NAME                           STATUS   MODEL         TOKENS IN  TOKENS OUT  COST     ELAPSED   LAST TOOL
  ────────────────────────────────────────────────────────────────────────────────────────────────────────
  -home-armani-projects-fnaf     running  claude-sonnet   142,310       8,421  $0.0234  12m 34s   Bash
  -home-armani-projects-api      running  claude-haiku     98,002       6,103  $0.0171   8m 12s   Write
  -home-armani-projects-web      idle     claude-haiku     43,118       2,940  $0.0041   3m 07s   Read
```

It's a single Go binary that auto-discovers your Claude Code sessions from `~/.claude/projects/**/*.jsonl` and shows them in a live-updating table. Cost and token counts update in real time as the sessions run.

**Install:**

```bash
go install github.com/acunningham-ship-it/agent-htop/cmd/agent-htop@latest
# or
brew install acunningham-ship-it/tap/agent-htop
```

**Run:**

```bash
agent-htop   # no flags needed, finds Claude Code sessions automatically
```

**What it tracks:**
- Tokens in / out per session
- Cost in real-time (uses Anthropic's pricing table)
- Elapsed time and last tool call
- Anomaly flags for runaway spend or error streaks
- Optional Discord webhook for alerts when you're not watching

**Paperclip support (additive):** If you run a Paperclip agent fleet, pass `--company <id>` and your Paperclip agents appear in the same table with kill/pause control (`K` / `P` keys). Otherwise it's purely local file-watching — no API required.

Built it as a single Go binary using bubbletea for the TUI. Works on Linux and macOS. The binary is ~11MB.

**Links:**
- GitHub: https://github.com/acunningham-ship-it/agent-htop
- Homebrew: `brew install acunningham-ship-it/tap/agent-htop`

I'm 17 and this is a tool I actually use daily. Happy to answer questions or take feedback.

---

**Cross-post targets:** r/golang, r/commandline, r/ClaudeAI  
**Best timing:** Tuesday 8–10am US-East
