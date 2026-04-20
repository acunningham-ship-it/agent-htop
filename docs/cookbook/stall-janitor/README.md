# Stall Janitor — Stuck Agent Cleaner

A routine cleanup agent that detects agents stuck in "running" state (no activity for 10+ minutes) and safely pauses them with triage.

## What It Does

Runs every 5 minutes and:
1. Queries the fleet for agents with `status=running` and `heartbeat_age > 10min`
2. Pauses each stalled agent (non-destructive)
3. Files a bug report with session log tail for engineers to investigate
4. Logs recommended recovery action (resume, terminate, or restart)

## Use Case

Agent processes sometimes hang:
- Waiting on external API that never responds
- Deadlock in state management
- Network interruption leaves process orphaned

Without intervention, these block task queues and waste resources. Stall Janitor:
- Detects without human babysitting
- Pauses (doesn't kill) so logs are preserved
- Alerts immediately with context for triage

## Config

```yaml
name: StallJanitor
model: claude-3-5-haiku-20241022
provider: anthropic

# Run every 5 minutes  
schedule:
  frequency: 5m
  timezone: UTC

permissions:
  - agent:read
  - agent:write  # Pause agents
  - issue:write  # File triage reports

compute:
  cpu: 0.1
  memory: 128Mi
```

## Agent Prompt

See `prompt.md`. Key guidance:

- Query `/api/agents` list and filter for `status=running`
- Check `heartbeatAge` — if > 600 seconds (10 min), agent is stalled
- POST `/api/agents/{agentId}/pause` to freeze it
- Fetch last 50 lines of agent's run log (from filesystem)
- File issue: "Stalled agent {agentName}: no heartbeat for {duration}s"
- Include log tail + recommended recovery

## Integration with agent-htop

agent-htop displays real-time heartbeat age in its dashboard:
- Use `HeartbeatAge` field as primary indicator
- If agent shows as "running" in agent-htop but hasn't updated in 10 min, Stall Janitor targets it
- Operators can see which agents were paused and why (via filed issues)

## What Happens Next

**Option 1: Auto-resume** — If paused agent has no errors in logs, auto-resume after 2 min
**Option 2: Manual review** — Default; engineers investigate before resuming
**Option 3: Restart** — If hung process detected, kill + restart agent fresh

## Caveats

- **False positives on slow tasks** — a legitimate 15-minute LLM call looks like a stall
  - Solution: Whitelist slow agents, or increase threshold to 20 minutes
- **Logs may be sparse** — if agent never writes heartbeat, hard to diagnose
  - Solution: Ensure all agents have heartbeat middleware
- **Doesn't restart** — just pauses; assumes human or another agent will restart

## Example

```
[StallJanitor] Checking fleet (11:35 UTC)
Found 2 stalled agents:
  1. ReportGenerator: running 847 seconds (14 min 7 sec) — no heartbeat
  2. ScrapeWorker: running 1203 seconds (20 min 3 sec) — no heartbeat

Paused both agents.
Filed issues for triage:
  - HTO-521: Stalled agent ReportGenerator no heartbeat 847s
  - HTO-522: Stalled agent ScrapeWorker no heartbeat 1203s

[Log tail for ReportGenerator]:
2026-04-20 11:20:15 START task process_report.pdf
2026-04-20 11:20:20 TOOL_CALL_START inference
... no activity since then
```
