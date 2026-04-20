# Error Triage Agent — Automated Issue Diagnosis

An intelligent agent that detects error patterns in the fleet, analyzes root causes, and drafts fix suggestions for engineers.

## What It Does

Triggered when agent-htop detects anomalies (`get_anomalies` API call):
1. Fetches agents currently in error state
2. Reads their recent session logs
3. Identifies error pattern (timeout, out-of-memory, API error, etc.)
4. Drafts a fix suggestion based on the pattern
5. Files a ticket with diagnosis + recommendations

## Use Case

Errors happen. Instead of waiting for humans to notice and investigate:
- Automatically detect error streaks (3+ agents hitting same error)
- Surface common patterns (e.g., all timeout on same external API)
- Suggest fixes: "reduce timeout", "batch requests", "retry logic"
- Let engineers focus on complex problems; automate simple triage

## Config

```yaml
name: ErrorTriageAgent
model: claude-opus-4-6  # Use latest for reasoning
provider: anthropic

# Triggered by anomaly detector (not scheduled)
# Runs when agent-htop detects error patterns
permissions:
  - agent:read
  - issue:write
  - issue:read

compute:
  cpu: 0.5
  memory: 512Mi
```

## API Integration

This agent works closely with agent-htop's anomaly detector:
- agent-htop exports `AnomalyEvent` data (error type, affected agents, count)
- ErrorTriageAgent receives this as input context
- Uses Paperclip API to fetch detailed logs for diagnosis

**Example anomaly event:**
```json
{
  "type": "error_streak",
  "error_type": "timeout",
  "affected_agents": ["agent-123", "agent-456", "agent-789"],
  "count": 3,
  "detected_at": "2026-04-20T14:32:15Z"
}
```

## Agent Prompt

See `prompt.md`. Key capabilities:

- Receives anomaly event with list of affected agents
- Fetches their session logs (`~/.paperclip/instances/default/data/run-logs/`)
- Parses error messages and stack traces
- Classifies: timeout, OOM, API error, rate-limit, auth, unknown
- Drafts fix with severity (critical, high, medium, low)
- Files issue: "Error pattern: {type} in {N} agents"

## Fix Suggestions

Agent learns from common patterns:

| Error Pattern | Root Cause | Fix |
|---|---|---|
| `timeout: external_api` | API slow or unresponsive | Increase timeout, add retry, use fallback |
| `out of memory` | Large batch job | Reduce batch size, stream data, use pagination |
| `rate_limit_exceeded` | Too many requests | Add exponential backoff, token bucket limiter |
| `authentication_failed` | Token expired or wrong | Refresh credentials, check secrets |
| `connection_reset_by_peer` | Network flake | Add circuit breaker, retry logic |

## Integration with agent-htop

- agent-htop feeds anomalies to this agent
- This agent reads agent-htop's log directory
- Both reference same agent states and errors
- Closes the loop: detection → diagnosis → fix suggestion

## Caveats

- **Early detection** — agent-htop must flag anomalies quickly
- **Log availability** — agent must access same log files as agent-htop
- **False positives** — single error != streak; needs clustering
- **Suggestions not patches** — fixes are recommendations, not code changes

## Example Workflow

```
09:15 UTC: agent-htop detects 3 agents with timeout errors
09:15 UTC: ErrorTriageAgent triggered with anomaly event
09:16 UTC: Fetches logs, identifies pattern: "API timeout waiting on /sync endpoint"
09:17 UTC: Files issue: "Error pattern: API timeout in 3 agents — recommend increase timeout from 10s to 30s"
09:18 UTC: Engineer reviews ticket, increases timeout in agent config
09:20 UTC: Agents resume, no more timeouts
```
