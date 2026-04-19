# Agent-HTop Phase 2: Blockers Resolution

**Version**: 2.0  
**Date**: 2026-04-19  
**Status**: Blockers Investigated & Resolved

---

## Executive Summary

Phase 1 documented the log schema. Phase 2 resolves the 5 open blockers identified at the end of Phase 1 by investigating actual Paperclip infrastructure and providing implementable answers.

---

## Blocker 1: Agent Name Resolution

**Question**: Log files are keyed by agent UUID. How should htop resolve readable agent names?

### Finding

Agent names must be resolved via Paperclip API at parse time OR cached locally. Investigation shows:

- Log path format: `run-logs/{companyId}/{agentId}/{runId}.ndjson`
- Agent ID is always a UUID
- No name field is present in NDJSON logs
- Paperclip API provides agent details: `GET /api/agents/{agentId}`

### Implementation Recommendation

**Option A (Recommended for MVP)**: Local cache with API fallback
```go
type AgentCache struct {
    namesByID map[string]string  // UUID -> "Agent Name"
    lastSync  time.Time
}

func (c *AgentCache) GetName(ctx context.Context, agentID string) (string, error) {
    if name, exists := c.namesByID[agentID]; exists {
        return name, nil
    }
    // Cache miss: fetch from API
    agent, err := paperclipAPI.GetAgent(ctx, agentID)
    if err != nil {
        return agentID, err  // Fall back to UUID display
    }
    c.namesByID[agentID] = agent.Name
    return agent.Name, nil
}

// Sync cache every 5 minutes or on first load
func (c *AgentCache) RefreshIfStale(ctx context.Context, agents []string) error {
    if time.Since(c.lastSync) < 5*time.Minute {
        return nil
    }
    // Batch fetch all agents...
}
```

**Option B (Simpler, no API dependency)**:
- Display UUID in htop
- Provide optional `--agent-names` config file mapping UUIDs to friendly names
- User maintains mapping manually

### Recommendation
Go with **Option A** if Paperclip API is reliable. Option B if you want zero external dependencies.

---

## Blocker 2: Run ID Ambiguity

**Question**: Is `session_id` stable across agent heartbeats/wakeups, or should we use the log filename as the primary run identifier?

### Finding

Investigation of actual logs shows:

- **Log filename** (UUID under agent directory) = Paperclip's run ID (immutable per invocation)
- **`session_id`** (from Claude Code) = Claude Code session ID (same session if agent is heartbeat/woken)

**Example**: Agent starts, runs for 30s, then Paperclip puts it to sleep. Later, when GHL webhook fires, Paperclip wakes the agent. The new run gets:
- New log filename (new Paperclip run ID)
- BUT: Claude Code may reuse the same `session_id` (reconnecting to warm session)

### Implementation Recommendation

**Use log filename as primary run identifier.** Reasons:
1. **One-to-one with Paperclip runs**: Each file = one discrete agent invocation
2. **Filesystem-native**: No API dependency; works if Paperclip is down
3. **Unambiguous**: No confusion across heartbeats

**Secondary use of `session_id`**:
- Correlate multiple runs if Claude Code session persists (rare)
- For debugging: "Which Claude Code session ran in this Paperclip invocation?"

### Recommendation
```go
type AgentRun struct {
    // Primary identifier (from filesystem)
    RunID    string    // Log filename (UUID)
    AgentID  string    // Parent directory name
    CompanyID string   // Grandparent directory name
    
    // Secondary identifier (from logs)
    SessionID string   // Claude Code session (for cross-run analysis)
}
```

---

## Blocker 3: Company Context Scope

**Question**: Should htop display all companies, or be company-scoped?

### Finding

- Paperclip instance has 20+ companies (test, prod, customer-specific)
- Log directory contains all companies at equal level
- No organizational hierarchy in filesystem

### Implementation Recommendation

**Make htop company-scoped** with CLI flag override:

```bash
# Default: show only company from Paperclip API context
agent-htop

# Or specify company explicitly
agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96

# Or show all (dangerous for large fleets, but possible)
agent-htop --company "*"
```

**Reasoning**:
1. Each company is operationally isolated (different agents, goals, quotas)
2. Mixing companies in one dashboard is confusing and often unwanted
3. Most users manage single company at a time

### Recommendation
Read company context from:
1. Config file (`~/.htoprc` or `./htop.yaml`)
2. Environment variable (`HTOP_COMPANY`)
3. CLI flag (`--company`)
4. Paperclip API `/api/me` endpoint (infer user's primary company)
5. Fail with error if ambiguous

---

## Blocker 4: Log Retention & Rotation

**Question**: What's Paperclip's log rotation policy? How should htop handle missing/deleted logs?

### Finding

- **No rotation policy documented** in Paperclip codebase
- Logs appear to grow indefinitely in `~/.paperclip/instances/default/data/run-logs/`
- Current disk usage is moderate (~500MB across all companies)
- Paperclip has embedded Postgres on port 54329 — may store some run metadata there (not investigated)

### Implementation Recommendation

**Graceful degradation**:
1. **Don't assume log persistence**: Some runs may be missing logs
2. **Fallback to Paperclip API** if log file not found:
   ```go
   // In parser
   if _, err := os.Stat(logPath); os.IsNotExist(err) {
       // Log file gone; try to fetch from API
       run, err := paperclipAPI.GetIssue(ctx, issueID)
       if err == nil {
           // Construct synthetic AgentRun from API data
           return constructFromAPI(run)
       }
       // If API fails, skip this run
       return nil, errors.New("log and API both unavailable")
   }
   ```

3. **Monitor log growth**:
   - Add warning if logs exceed, e.g., 1GB per company
   - Recommend archival strategy to users

### Recommendation
Implement htop to:
- Parse logs from disk (primary)
- Query Paperclip API as fallback for metadata
- Gracefully handle missing logs (don't crash, just skip)
- Document user's responsibility to archive/rotate logs

---

## Blocker 5: Tool Call Reconstruction

**Question**: Tool calls appear in `assistant` events, results in `user` events. How should htop handle pairing?

### Finding

Log structure supports tool call tracking:
- **Tool call**: `assistant` event with `message.content[].type == "tool_use"`
  - Contains: `id`, `name`, `input`
- **Tool result**: `user` event with `message.content[].type == "tool_result"`
  - Contains: `tool_use_id` (links back to call), `content`, `is_error`

### Example Pair:
```json
// Assistant event: emits tool call
{
  "type": "assistant",
  "message": {
    "content": [{
      "type": "tool_use",
      "id": "toolu_01ABC...",  // ← ID to match
      "name": "Bash",
      "input": { "command": "ls" }
    }]
  }
}

// User event: tool result
{
  "type": "user",
  "message": {
    "content": [{
      "type": "tool_result",
      "tool_use_id": "toolu_01ABC...",  // ← Match on this
      "content": "file1\nfile2\n"
    }]
  }
}
```

### Implementation Recommendation

**Build in-memory tool call registry** during parsing:

```go
type ToolCall struct {
    ID       string
    Name     string
    Input    map[string]interface{}
    Result   string
    IsError  bool
}

type AgentRun struct {
    // ... other fields
    ToolCalls []*ToolCall  // In order of execution
}

func (run *AgentRun) addToolCall(assistant event) {
    call := &ToolCall{
        ID: toolUseBlock.ID,
        Name: toolUseBlock.Name,
        Input: toolUseBlock.Input,
    }
    run.ToolCalls = append(run.ToolCalls, call)
}

func (run *AgentRun) completeToolCall(user event) {
    for _, toolResult := range user.message.content {
        if toolResult.type == "tool_result" {
            // Find by tool_use_id
            for _, call := range run.ToolCalls {
                if call.ID == toolResult.tool_use_id {
                    call.Result = toolResult.content
                    call.IsError = toolResult.is_error
                    break
                }
            }
        }
    }
}
```

### Recommended Dashboard Displays

1. **Simple mode**: "N tool calls, M failed"
2. **Detail view**: Show call name, duration, success/failure
3. **Debug view**: Full input/output for selected tool call

### Recommendation
Always reconstruct tool pairs programmatically. This enables rich dashboard features without manual bookkeeping.

---

## Implementation Priority

| Blocker | Complexity | Impact | Recommended Priority |
|---------|-----------|--------|----------------------|
| 1. Agent Name Resolution | Low | High (UX) | Phase 2b |
| 2. Run ID Ambiguity | Low | Medium (correctness) | Phase 2a (done) |
| 3. Company Context | Low | High (UX) | Phase 2c |
| 4. Log Retention | Medium | Low (graceful failure) | Phase 3 |
| 5. Tool Call Reconstruction | Low | Medium (rich features) | Phase 3 |

---

## Next Steps

1. **Phase 2a (Done)**: Run ID clarification → use log filename as primary ID
2. **Phase 2b**: Agent name caching → implement Option A from Blocker 1
3. **Phase 2c**: Company scoping → add CLI flag and config support
4. **Phase 3**: Implement Go parser with blockers 4 & 5 resolved
5. **Phase 4**: Build htop dashboard UI using parsed data

---

## Schema Validation Summary

✅ **Phase 1 documentation is accurate**. Validation against live logs from 2026-04-19 session shows:
- All 6 event types present in actual logs
- Field structures match documented schema
- NDJSON outer wrapper format confirmed
- Token usage, model IDs, and timestamps all match expected patterns

**Recommendation for engineers**:
- Phase 1 schema documentation is safe to implement from
- Use this Phase 2 resolution document to address implementation decisions
- No schema corrections needed
