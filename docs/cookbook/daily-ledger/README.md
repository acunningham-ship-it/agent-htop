# Daily Ledger — Fleet Summary Reporter

A Claude Code agent that runs once per day (midnight UTC), captures a fleet snapshot, and writes a human-readable daily summary to Obsidian or markdown.

## What It Does

Runs nightly and:
1. Queries all agents + their daily metrics (cost, tokens, errors, run count)
2. Generates a markdown summary with:
   - Total daily spend and token usage
   - Top 5 most expensive agents
   - Error summary (if any)
   - Performance trends (vs. yesterday, vs. week ago)
3. Writes to Obsidian vault (or local file) with timestamp
4. Optionally emails/Slacks summary to stakeholders

## Use Case

Fleet operators need daily visibility:
- Did costs stay under budget?
- Which agents are most expensive?
- Any errors or anomalies to investigate?
- How do today's metrics compare to trends?

Daily Ledger automates the reporting so humans don't have to manually check dashboards. Perfect for:
- Daily standup context
- Budget tracking
- Performance audits
- Historical analysis

## Config

```yaml
name: DailyLedgerAgent
model: claude-opus-4-6
provider: anthropic
runtime: claude-code  # Runs in Claude Code harness, not Paperclip

# Scheduled execution
schedule:
  cron: 0 0 * * *  # Midnight UTC daily
  timezone: UTC

permissions:
  - agent:read
  - filesystem:write  # Write to Obsidian vault or local directory
  - email:send  # Optional: send daily summary
  - slack:send  # Optional: post to Slack

# Environment variables
env:
  OBSIDIAN_VAULT_DIR: ~/Obsidian/MyVault  # Or ~/daily-summaries/ if not using Obsidian
  PAPERCLIP_API_URL: http://localhost:3101
  PAPERCLIP_COMPANY_ID: {your-company-id}
  SMTP_SERVER: smtp.gmail.com  # For email
  SLACK_WEBHOOK_URL: {your-webhook}
```

## Agent Prompt

See `prompt.md`. Key capabilities:

- Claude Code agent (not Paperclip agent)
- Fetches agent list + aggregated daily metrics
- Generates markdown report with trends, outliers, alerts
- Writes to file with ISO date: `Daily-Ledger-2026-04-20.md`
- Compares metrics to previous day and 7-day average

## Metrics Included

**Per-agent:**
- Agent name + ID
- Daily cost (USD)
- Input + output tokens
- Number of runs completed
- Error count (if any)
- Status (idle, running, etc.)

**Fleet-wide:**
- Total daily cost
- Total tokens used
- Total runs
- Error summary
- Most expensive agent
- Most active agent
- Least used agent

## Integration with agent-htop

- Both read same Paperclip API
- Daily Ledger aggregates into daily summary
- agent-htop shows real-time detail
- Complement each other: real-time detail + daily context

## Output Example

```markdown
# Daily Fleet Ledger — 2026-04-20

## Summary
- **Total spend**: $42.15
- **Token usage**: 8.2M input, 1.4M output
- **Completed runs**: 247
- **Active agents**: 7
- **Errors**: 2 (rate limit, timeout)

## Trends vs. Yesterday
- Spend: ↑ +18% ($42.15 vs $35.70)
- Tokens: ↑ +22% (9.6M vs 7.9M)
- Errors: ↑ +1 (2 vs 1)

## Top 5 Most Expensive
1. DataScraperBot ($18.50) — 12 runs, 3.2M tokens
2. PDFProcessor ($12.40) — 8 runs, 2.1M tokens
3. ReportGenerator ($6.80) — 5 runs, 1.5M tokens
4. SlackBot ($2.45) — 47 runs, 0.8M tokens
5. CodeReviewBot ($2.00) — 18 runs, 0.6M tokens

## Alerts
⚠️ DataScraperBot cost +156% vs yesterday ($18.50 vs $7.20)
  → Possible runaway job; check logs
⚠️ 2 timeout errors detected in API agents
  → Consider increasing timeout threshold

## 7-Day Trend
- Avg daily cost: $31.40
- Today vs avg: $42.15 (+34%)
- Busiest day: 2026-04-17 ($52.30)
- Slowest day: 2026-04-18 ($18.95)

---
Generated: 2026-04-20T00:00:15Z by DailyLedgerAgent v0.3
```

## Caveats

- **One-day latency** — report runs at midnight, so last run of previous day may not be included
- **Aggregation method** — costs are summed hourly; high-precision accounting needs database
- **No historical lookback** — only compares to yesterday, not 30 days (easy to extend)
- **Obsidian optional** — can write to any directory; Obsidian just makes review easier

## Files Generated

Each run creates one file per day:
- **File**: `{VAULT_DIR}/Daily-Ledger-YYYY-MM-DD.md`
- **Size**: ~2-5 KB typically
- **Retention**: Keep indefinitely (good audit trail)

## Next Steps

After 7-14 days of data:
1. Review trends (Is cost growing? Errors increasing?)
2. Adjust thresholds if needed (budget cap, error alerts)
3. Automate alerts (e.g., "if cost > threshold, run BudgetSentinel immediately")
4. Share summaries with stakeholders (email, Slack, dashboard)
