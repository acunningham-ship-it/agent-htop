# Cookbook — Supervisor Agent Recipes Using agent-htop

**What it is**: Worked examples of agents-managing-agents. These recipes show how to build supervisor agents that use the Paperclip API to monitor, control, and triage agent fleets.

**Who this is for**: You're running Paperclip agents at scale and want to automate ops tasks (cost control, error triage, stall detection, reporting).

**What you get**: 4 production-ready patterns that plug into your fleet.

---

## Quick Start

1. Pick a recipe (see below)
2. Copy the prompt from `{recipe}/prompt.md` into your agent config
3. Deploy the agent to Paperclip (or Claude Code for the daily ledger)
4. Monitor results in agent-htop

Each recipe is self-contained; you can use them together or independently.

---

## The Recipes

### 1. [Budget Sentinel](./budget-sentinel/) — Cost Guard Dog

**What**: Haiku agent running every 15 minutes that monitors costs and kills runaway agents.

**Why**: LLM calls can spiral from $5 to $500 in minutes. Budget Sentinel catches them before they blow up.

**How**: Reads cost projections, identifies top spenders, terminates if over threshold, files ticket.

**Use if**: You have budget caps, multiple teams using same fleet, or history of runaway jobs.

**Setup time**: 10 minutes

---

### 2. [Stall Janitor](./stall-janitor/) — Stuck Agent Cleaner

**What**: Routine cleanup (every 5 min) that finds agents stuck in "running" state with no heartbeat.

**Why**: Agent processes sometimes hang (network timeout, deadlock, API stuck). Without cleanup, they block queues.

**How**: Detects heartbeat gaps, pauses agents, collects logs, files triage ticket.

**Use if**: You have complex agents, external API dependencies, or frequent infrastructure flakes.

**Setup time**: 10 minutes

---

### 3. [Error Triage Agent](./error-triage/) — Automated Diagnosis

**What**: Triggered by agent-htop's anomaly detector when error patterns emerge.

**Why**: Errors happen. Instead of waiting for humans to investigate, auto-triage and suggest fixes.

**How**: Analyzes error logs, classifies pattern (timeout, OOM, API error, rate limit), drafts fix.

**Use if**: You want to speed up incident response, have recurring error patterns, or limited ops staff.

**Setup time**: 15 minutes (requires agent-htop integration)

---

### 4. [Daily Ledger](./daily-ledger/) — Fleet Reporter

**What**: Claude Code agent running nightly that snapshots fleet metrics and writes a summary.

**Why**: Daily dashboards are noise. A human-readable summary tells you what changed and why.

**How**: Aggregates per-agent costs, tokens, errors. Compares to yesterday + 7-day avg. Writes markdown.

**Use if**: You want audit trails, budget tracking, or to share daily updates with stakeholders.

**Setup time**: 20 minutes

---

## How to Use

### Step 1: Choose a Recipe

Start with one. Budget Sentinel and Stall Janitor are simplest; Daily Ledger is most useful for long-term.

### Step 2: Read the README

Each `{recipe}/README.md` explains:
- What the agent does
- Why it matters
- How it works
- When to use it
- Caveats

### Step 3: Copy the Prompt

Each `{recipe}/prompt.md` contains the full system prompt. Copy it directly into your Paperclip agent config or Claude Code script.

### Step 4: Deploy

**For Paperclip agents** (Budget Sentinel, Stall Janitor, Error Triage):
```bash
# Hire agent via Paperclip UI or API
curl -X POST http://localhost:3101/api/companies/{companyId}/agents \
  -H "Authorization: Bearer {token}" \
  -d '{
    "name": "BudgetSentinel",
    "model": "claude-3-5-haiku-20241022",
    "provider": "anthropic",
    "schedule": {"frequency": "15m"},
    "systemPrompt": "# [paste prompt.md content here]"
  }'
```

**For Claude Code** (Daily Ledger):
```bash
# Create a new Claude Code script
# Paste prompt.md as system message
# Set to run on cron schedule: 0 0 * * * (midnight daily)
```

### Step 5: Monitor

Watch results in:
- **agent-htop**: Real-time fleet metrics + anomalies
- **Paperclip dashboard**: Issues filed by supervisor agents
- **Your infrastructure**: Kill signals sent, agents paused, costs tracked

---

## Integration with agent-htop

These recipes work best alongside agent-htop:

| Task | Agent | Tool |
|------|-------|------|
| Real-time monitoring | — | agent-htop (terminal dashboard) |
| Cost caps | Budget Sentinel | Reads agent-htop's cost fields |
| Stall detection | Stall Janitor | Checks heartbeat_age |
| Error detection | — | agent-htop's anomaly detector |
| Error triage | Error Triage Agent | Reads anomalies from agent-htop |
| Daily reporting | Daily Ledger | Reads agent-htop's log format |

**They work together**: agent-htop is your observability, recipes are your automation.

---

## Paperclip API Reference

All recipes use the Paperclip REST API. Here's the reference:

**Base URL**: http://localhost:3101 (or your Paperclip instance)
**Auth**: Bearer token in `Authorization` header

### Agent Operations

```bash
# List agents
GET /api/companies/{companyId}/agents

# Get single agent
GET /api/agents/{agentId}

# Control agents
POST /api/agents/{agentId}/pause
POST /api/agents/{agentId}/resume
POST /api/agents/{agentId}/interrupt
POST /api/agents/{agentId}/terminate

# Check fleet health
GET /health
```

### Issue Management

```bash
# List issues
GET /api/companies/{companyId}/issues

# Create issue
POST /api/companies/{companyId}/issues
{
  "title": "...",
  "description": "...",
  "priority": "high|medium|low",
  "assigneeAgentId": "..."
}
```

### Caching Notes

- **Agent list**: Cache locally; refresh hourly
- **Agent details**: Cache per-agent; 5min TTL
- **Cost/token data**: Read from logs; API aggregation coming in v0.4

---

## Customization

Each recipe is a template. Common customizations:

### Budget Sentinel
- Change budget threshold: `projected_daily > $50` → `$20` or `$100`
- Change kill behavior: `terminate` → `pause` (safer)
- Add whitelist: Don't cap certain agents (e.g., "LongRunningReport")

### Stall Janitor
- Increase timeout: 10 min → 20 min (for slow tasks)
- Change pause to resume: Auto-recover if no errors
- Add notification: Email ops team when agents paused

### Error Triage
- Add custom classifications: Extend error type list
- Fine-tune suggestions: Adjust fix templates
- Add notification: Slack alert when critical errors found

### Daily Ledger
- Change schedule: Midnight → 6am (or twice daily)
- Add detail: Include per-tool breakdown, or model performance
- Export format: Markdown → JSON (for programmatic use)

---

## Troubleshooting

### "Agent won't start"

Check logs:
```bash
# Paperclip logs
tail ~/.paperclip/instances/default/logs/agent-{agentId}.log

# System logs
journalctl -u paperclip -n 50
```

Verify API is reachable:
```bash
curl http://localhost:3101/health
```

### "No anomalies detected" (Error Triage doesn't run)

agent-htop only files anomalies if error patterns emerge. To test:
1. Manually create an agent that errors
2. Wait for agent-htop to detect pattern (5+ min)
3. Error Triage should trigger

### "Daily Ledger missing data"

Ensure log directory is readable:
```bash
ls -la ~/.paperclip/instances/default/data/run-logs/ | head
```

Verify format (should be JSONL):
```bash
head -1 ~/.paperclip/instances/default/data/run-logs/*.jsonl | jq .
```

### "API calls are slow"

- Check Paperclip is healthy: `curl localhost:3101/health`
- Reduce request frequency (e.g., Budget Sentinel every 30m instead of 15m)
- Cache agent list locally instead of fetching each run

---

## FAQ

**Q: Can I run all 4 recipes at once?**
A: Yes! They operate independently. Only Error Triage depends on agent-htop anomalies.

**Q: Do I need Paperclip, or can I use Claude Code for all of them?**
A: Paperclip agents are easier to schedule and integrate. Claude Code works for Daily Ledger (and others, but requires more setup).

**Q: What if an agent gets stuck in a loop and kills itself?**
A: Good question. Budget Sentinel terminates agents, which stops the loop. Stall Janitor pauses but doesn't restart. Error Triage only suggests fixes. For auto-restart, you'd add another agent or use Paperclip's built-in recovery.

**Q: Can I share these with my team?**
A: Yes! Copy the `docs/cookbook/` directory into your team's docs. Each recipe is self-explanatory.

**Q: How do I integrate with Slack/email?**
A: Recipes file Paperclip issues, which can trigger Slack webhooks. Or extend Daily Ledger to POST to Slack directly.

---

## Version History

- **v0.3** (2026-04-20): Initial release with 4 recipes
- Planned v0.4: Cost projection API, historical metrics, advanced filtering

---

## Questions?

- Check individual recipe READMEs
- Review Paperclip API docs: http://localhost:3101/docs
- Examine agent-htop logs: `agent-htop` in terminal
- File an issue: GitHub issues for bugs/feature requests

**Happy automating!** 🤖
