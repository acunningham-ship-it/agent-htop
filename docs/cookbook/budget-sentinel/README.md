# Budget Sentinel — Cost Monitoring Agent

A lightweight Haiku agent that monitors Paperclip fleet costs in real-time and kills runaway agents before they exceed budget.

## What It Does

Runs every 15 minutes and:
1. Fetches cost projections for all agents in the fleet
2. Identifies top spenders and their projected daily cost
3. If any agent is projected to exceed budget threshold, terminates it
4. Files a bug report issue for triage

## Use Case

Runaway LLM calls (infinite loops, token leaks) can cost $50-500/hour. This agent catches them before they spiral:
- Sets a budget cap (e.g., $20/day per agent)
- Monitors projection every 15 min
- Acts fast — kills + alerts instead of waiting for humans

## Config

Create a Paperclip agent with these settings:

```yaml
name: BudgetSentinel
model: claude-3-5-haiku-20241022  # Fast, cheap Haiku model
provider: anthropic

# Run every 15 minutes
schedule:
  frequency: 15m
  timezone: UTC

# Grant access to Paperclip API
permissions:
  - agent:read
  - agent:write  # Needed to terminate agents
  - issue:write  # File bug reports

# Minimal resources — this agent is lightweight
compute:
  cpu: 0.1
  memory: 128Mi
```

## Agent Prompt

See `prompt.md` for the full system prompt. Key guidance:

- Query `/api/companies/{companyId}/agents` for agent list
- For each agent, calculate `projected_daily_cost = current_cost_usd / (elapsed_hours or 0.25) * 24`
- If projected > $50/day (threshold), POST to `/api/agents/{agentId}/terminate`
- File issue with title "Cost alert: {agent_name} projected at ${amount}/day"

## Integration with agent-htop

This agent uses agent-htop's JSON output format as reference:
- Monitor the same `TotalCostUSD` field that agent-htop displays
- Use the same `ElapsedMS` to calculate hourly burn rate
- Files issues that agent-htop operators can review in their dashboard

## Caveats

- **Doesn't refund** — terminating doesn't claw back already-spent tokens
- **Grace period** — consider allowing agents 5-10 min of high spend before killing (to avoid false positives)
- **Alert-first** — in production, consider pausing instead of terminating; let humans decide
