# Agent-HTop Log Schema Specification

**Version**: 1.0  
**Date**: 2026-04-19  
**Status**: Phase 1 Research Complete

---

## Overview

This document specifies the unified log schema for `agent-htop`, a terminal dashboard for monitoring AI agent fleets running in Paperclip. The schema consolidates logs from two sources:

1. **Paperclip NDJSON Run Logs** — Claude Code agent execution logs stored in `~/.paperclip/instances/default/data/run-logs/`
2. **Claude Code Session Metadata** — Session tracking and configuration from `~/.claude/`

All Paperclip logs are written as **newline-delimited JSON (NDJSON)**, with each line being a complete JSON object. Claude Code logs are embedded within Paperclip's NDJSON output.

---

## Log File Format

### Outer Wrapper Structure

Every line in a Paperclip NDJSON log file follows this structure:

```json
{
  "ts": "2026-04-19T17:05:27.699Z",
  "stream": "stdout" | "stderr",
  "chunk": "{...JSON string...}"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ts` | ISO8601 string | ✓ | Timestamp when the log line was written |
| `stream` | "stdout" \| "stderr" | ✓ | Output stream (stdout for normal events, stderr for errors) |
| `chunk` | JSON string | ✓ | Escaped JSON string containing the actual event data |

**Note**: The `chunk` field is a JSON-encoded string that must be parsed to extract the inner event. Parse with `JSON.parse(chunk)`.

---

## Event Types

Paperclip generates **6 main event types**, identified by the `type` field in the parsed `chunk`:

1. **system** — Session initialization and configuration
2. **assistant** — Claude model responses and tool calls
3. **user** — User inputs and tool results
4. **rate_limit_event** — API rate limit status
5. **result** — Run completion summary
6. **system/api_retry** — API retry attempts (rare)

---

### Event Type: `system`

Emitted at the start of each Claude Code session.

**Subtype**: `init` (always)

**Schema**:

```json
{
  "type": "system",
  "subtype": "init",
  "cwd": "/path/to/working/directory",
  "session_id": "uuid string (e.g., '2b2a89f8-e5d5-4a7c-8b42-7cd168ad43cb')",
  "uuid": "unique event id",
  "model": "claude-sonnet-4-6",
  "claude_code_version": "2.1.97",
  "permissionMode": "bypassPermissions" | "limitedPermissions",
  "fast_mode_state": "off" | "on",
  "apiKeySource": "none" | "credentials",
  "output_style": "default",
  "tools": ["array", "of", "available", "tool", "names"],
  "mcp_servers": [
    {
      "name": "paperclip",
      "status": "connected" | "failed" | "needs-auth"
    }
  ],
  "slash_commands": ["array", "of", "command", "names"],
  "agents": ["general-purpose", "statusline-setup", "Explore", "Plan"],
  "skills": ["array", "of", "skill", "names"],
  "plugins": []
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | "system" | ✓ | Event type identifier |
| `subtype` | "init" | ✓ | Event subtype |
| `cwd` | string (path) | ✓ | Current working directory for the session |
| `session_id` | UUID string | ✓ | Unique session ID, correlates all events in this run |
| `uuid` | UUID string | ✓ | Unique ID for this specific event |
| `model` | string | ✓ | Model used (e.g., "claude-sonnet-4-6", "claude-haiku-4-5-20251001") |
| `claude_code_version` | string | ✓ | Version of Claude Code harness |
| `permissionMode` | string | ✓ | Permission enforcement mode |
| `fast_mode_state` | "off" \| "on" | ✓ | Whether fast mode is enabled |
| `apiKeySource` | string | ✓ | Source of API key |
| `output_style` | string | ✗ | Output formatting style |
| `tools` | string[] | ✓ | List of available tools for this session |
| `mcp_servers` | object[] | ✗ | Status of MCP servers |
| `slash_commands` | string[] | ✗ | Available slash commands |
| `agents` | string[] | ✗ | Available agent types |
| `skills` | string[] | ✗ | Available skills |
| `plugins` | object[] | ✗ | Available plugins |

**Use Case (for htop)**:
- Extract `model` for model display
- Use `session_id` to group all events in a run
- Check `cwd` to identify project/workspace context

---

### Event Type: `assistant`

Emitted when Claude generates a response (text, thinking blocks, tool use).

**Subtype**: (none)

**Schema**:

```json
{
  "type": "assistant",
  "session_id": "uuid string",
  "uuid": "unique event id",
  "parent_tool_use_id": null | "string",
  "message": {
    "model": "claude-sonnet-4-6",
    "id": "msg_01SiWsA77F2UZbwf3ofyvNs4",
    "type": "message",
    "role": "assistant",
    "content": [
      {
        "type": "thinking" | "text" | "tool_use",
        // Content varies by type
      }
    ],
    "stop_reason": "end_turn" | "tool_use" | null,
    "stop_sequence": null | "string",
    "stop_details": null | "object",
    "usage": {
      "input_tokens": 3,
      "output_tokens": 97,
      "cache_creation_input_tokens": 11152,
      "cache_read_input_tokens": 11036,
      "service_tier": "standard",
      "cache_creation": {
        "ephemeral_1h_input_tokens": 11152,
        "ephemeral_5m_input_tokens": 0
      },
      "inference_geo": "not_available"
    },
    "context_management": null
  }
}
```

**Key Fields**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | "assistant" | ✓ | Event type identifier |
| `session_id` | UUID string | ✓ | Session ID (correlate with system/init) |
| `uuid` | UUID string | ✓ | Unique event ID |
| `parent_tool_use_id` | string \| null | ✗ | ID of parent tool use (if this is a tool result response) |
| `message.model` | string | ✓ | Model that generated this response |
| `message.id` | string | ✓ | Anthropic message ID |
| `message.content` | object[] | ✓ | Response content blocks (thinking, text, tool_use) |
| `message.stop_reason` | string \| null | ✓ | Why Claude stopped (end_turn, tool_use, etc.) |
| `message.usage` | object | ✓ | Token usage details |

**Content Block Types**:

- **thinking**: Contains `thinking` field with reasoning (usually signed)
- **text**: Contains `text` field with plain text output
- **tool_use**: Contains `type`, `id`, `name`, `input` for tool invocation

**Use Case (for htop)**:
- Track token usage from `message.usage`
- Identify tool calls from content blocks with `type: "tool_use"`
- Monitor model performance via `message.id`

---

### Event Type: `user`

Emitted when the user provides input or when a tool returns a result.

**Subtype**: (none)

**Schema**:

```json
{
  "type": "user",
  "session_id": "uuid string",
  "uuid": "unique event id",
  "timestamp": "2026-04-19T17:10:28.337Z",
  "isSynthetic": false | true,
  "parent_tool_use_id": null | "string",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "text" | "tool_result",
        // Content varies by type
      }
    ]
  },
  "tool_use_result": null | {
    "status": "success" | "error",
    "output": "any"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | "user" | ✓ | Event type identifier |
| `session_id` | UUID string | ✓ | Session ID |
| `uuid` | UUID string | ✓ | Unique event ID |
| `timestamp` | ISO8601 string | ✓ | When this user input occurred |
| `isSynthetic` | boolean | ✗ | True if generated internally (not from human) |
| `parent_tool_use_id` | string \| null | ✗ | Tool ID this result belongs to |
| `message` | object | ✓ | User message structure with content blocks |
| `tool_use_result` | object \| null | ✗ | Result from a tool execution |

**Use Case (for htop)**:
- Track input/output interactions
- Identify tool result success/error status
- Correlate with assistant messages via tool IDs

---

### Event Type: `rate_limit_event`

Emitted periodically to report API rate limit status.

**Subtype**: (none)

**Schema**:

```json
{
  "type": "rate_limit_event",
  "session_id": "uuid string",
  "uuid": "unique event id",
  "rate_limit_info": {
    "status": "allowed" | "rate_limited",
    "rateLimitType": "five_hour" | "minute" | string,
    "resetsAt": 1776632400,
    "isUsingOverage": false,
    "overageStatus": "allowed" | "rate_limited",
    "overageResetsAt": 1777593600
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | "rate_limit_event" | ✓ | Event type identifier |
| `session_id` | UUID string | ✓ | Session ID |
| `uuid` | UUID string | ✓ | Unique event ID |
| `rate_limit_info.status` | string | ✓ | Current rate limit status |
| `rate_limit_info.rateLimitType` | string | ✓ | Type of rate limit (five_hour, minute, etc.) |
| `rate_limit_info.resetsAt` | Unix timestamp | ✓ | When the rate limit resets (seconds since epoch) |
| `rate_limit_info.isUsingOverage` | boolean | ✓ | Whether overage quota is being used |
| `rate_limit_info.overageStatus` | string | ✗ | Status of overage quota |
| `rate_limit_info.overageResetsAt` | Unix timestamp | ✗ | When overage quota resets |

**Use Case (for htop)**:
- Alert users when rate limits are approaching
- Track API quota usage across fleet

---

### Event Type: `result`

Emitted once at the end of a run to summarize execution.

**Subtype**: `success` | `error` | `cancelled`

**Schema** (success):

```json
{
  "type": "result",
  "subtype": "success",
  "session_id": "uuid string",
  "uuid": "unique event id",
  "is_error": false,
  "result": "Final output text",
  "duration_ms": 6991,
  "duration_api_ms": 6853,
  "num_turns": 2,
  "stop_reason": "end_turn",
  "total_cost_usd": 0.02281795,
  "terminal_reason": "completed",
  "fast_mode_state": "off",
  "usage": {
    "input_tokens": 18,
    "output_tokens": 481,
    "cache_creation_input_tokens": 11283,
    "cache_read_input_tokens": 62912,
    "server_tool_use": {
      "web_search_requests": 0,
      "web_fetch_requests": 0
    },
    "service_tier": "standard",
    "cache_creation": {
      "ephemeral_1h_input_tokens": 11283,
      "ephemeral_5m_input_tokens": 0
    },
    "iterations": [],
    "speed": "standard"
  },
  "modelUsage": {
    "claude-haiku-4-5-20251001": {
      "inputTokens": 18,
      "outputTokens": 481,
      "cacheReadInputTokens": 62912,
      "cacheCreationInputTokens": 11283,
      "webSearchRequests": 0,
      "costUSD": 0.02281795,
      "contextWindow": 200000,
      "maxOutputTokens": 32000
    }
  },
  "permission_denials": [],
  "terminal_reason": "completed"
}
```

**Key Fields**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | "result" | ✓ | Event type identifier |
| `subtype` | "success" \| "error" \| "cancelled" | ✓ | Result status |
| `session_id` | UUID string | ✓ | Session ID |
| `uuid` | UUID string | ✓ | Unique event ID |
| `is_error` | boolean | ✓ | True if the run failed |
| `result` | string | ✓ | Final output or error message |
| `duration_ms` | number | ✓ | Total execution time in milliseconds |
| `duration_api_ms` | number | ✓ | Time spent in API calls (ms) |
| `num_turns` | number | ✓ | Number of conversation turns |
| `stop_reason` | string | ✓ | Why Claude stopped (end_turn, tool_use, etc.) |
| `total_cost_usd` | number | ✓ | Total cost of this run in USD |
| `terminal_reason` | string | ✓ | Final terminal state (completed, error, cancelled, etc.) |
| `usage` | object | ✓ | Aggregated token usage |
| `modelUsage` | object | ✓ | Per-model breakdown of usage and cost |
| `permission_denials` | object[] | ✗ | Any permission denials that occurred |

**Use Case (for htop)**:
- Primary data source for run summary display
- Extract `duration_ms`, `total_cost_usd`, `usage.output_tokens` for dashboard metrics
- Use `is_error` and `terminal_reason` to indicate run status visually
- Track performance across multiple runs

---

### Event Type: `system/api_retry` (Rare)

Emitted when Claude Code encounters an API error and retries.

**Subtype**: `api_retry`

**Schema**:

```json
{
  "type": "system",
  "subtype": "api_retry",
  "session_id": "uuid string",
  "uuid": "unique event id",
  "attempt": 1,
  "max_retries": 3,
  "retry_delay_ms": 1000,
  "error_status": 429,
  "error": "Rate limited"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | "system" | ✓ | Event type identifier |
| `subtype` | "api_retry" | ✓ | Subtype |
| `session_id` | UUID string | ✓ | Session ID |
| `uuid` | UUID string | ✓ | Unique event ID |
| `attempt` | number | ✓ | Current retry attempt (1-indexed) |
| `max_retries` | number | ✓ | Maximum retry attempts configured |
| `retry_delay_ms` | number | ✓ | Delay before next retry in ms |
| `error_status` | number | ✗ | HTTP status code (if applicable) |
| `error` | string | ✗ | Error message |

**Use Case (for htop)**:
- Alert on API issues
- Track transient failures separately from terminal errors

---

## Data Source Mapping

### Paperclip ← → Claude Code

All log data originates from **Claude Code executions within Paperclip agents**. The mapping is:

| Information | Source | Field Path |
|-------------|--------|------------|
| Agent Identity | Paperclip API | Log filename (UUID under company directory) |
| Run ID | Paperclip + Claude Code | `session_id` in events |
| Start Time | Claude Code | `system/init` event `ts` |
| End Time | Claude Code | `result` event `ts` |
| Model Used | Claude Code | `system/init.model` + `assistant.message.model` |
| Token Usage | Claude Code API | `result.usage` + `result.modelUsage` |
| Cost | Claude Code API | `result.total_cost_usd` + `result.modelUsage[model].costUSD` |
| Execution Status | Claude Code | `result.subtype` + `result.terminal_reason` |
| Tool Calls | Claude Code | `assistant.message.content[].type == "tool_use"` |
| Rate Limits | Claude Code API | `rate_limit_event.rate_limit_info` |

---

## Recommended Normalization Layer (for Go Parser)

### Internal Agent Run Structure

For `agent-htop` to display metrics uniformly across tools, the Go parser should normalize all logs into this internal structure:

```go
type AgentRun struct {
  // Identifiers
  AgentID      string    // Paperclip agent UUID
  SessionID    string    // Claude Code session UUID
  RunID        string    // Same as SessionID
  
  // Timing
  StartTime    time.Time // Parsed from first "system/init" ts
  EndTime      time.Time // Parsed from final "result" ts
  DurationMS   int64     // result.duration_ms
  
  // Model & Configuration
  Model        string    // system/init.model (primary)
  WorkDir      string    // system/init.cwd
  Permissions  string    // system/init.permissionMode
  
  // Execution Status
  Status       string    // result.subtype (success|error|cancelled)
  Result       string    // result.result (output or error msg)
  TerminalReason string  // result.terminal_reason
  IsError      bool      // result.is_error
  
  // Resource Usage
  TotalCostUSD float64   // result.total_cost_usd
  TotalInputTokens int   // Sum of all input_tokens
  TotalOutputTokens int  // Sum of all output_tokens
  
  // Breakdown by Model
  ModelUsage   map[string]ModelMetrics
  
  // Activity
  NumTurns     int       // result.num_turns
  NumToolCalls int       // Count of assistant events with tool_use content
  
  // Rate Limit Snapshot (if available)
  RateLimitStatus string // rate_limit_event.status (last seen)
  RateLimitResetsAt int64 // Unix timestamp from rate_limit_event
}

type ModelMetrics struct {
  InputTokens            int
  OutputTokens           int
  CacheCreationTokens    int
  CacheReadTokens        int
  CostUSD                float64
  ContextWindow          int
  MaxOutputTokens        int
}
```

### Event Processing Pipeline

The parser should process NDJSON lines in this order:

1. **Parse outer wrapper**: Extract `ts`, `stream`, decode `chunk`
2. **Route by event type**:
   - `system/init` → Initialize AgentRun, set WorkDir, Model, StartTime
   - `assistant` → Accumulate token usage, count tool calls
   - `user` → Track input sequence
   - `rate_limit_event` → Update RateLimitStatus and ResetsAt
   - `result` → Finalize AgentRun, set EndTime, Status, Cost
3. **Aggregate**: Sum tokens across all models in `modelUsage` map
4. **Output**: Emit completed AgentRun struct for dashboard rendering

---

## Open Questions & Blockers

1. **Agent Name Resolution**: Log files are keyed by agent UUID (e.g., `836058b7-8633-403d-ab20-29b657cf1d8e`). To display readable agent names in htop, the Go parser must:
   - Query Paperclip API: `/api/agents/{id}` to get `name` field
   - Cache agent names locally to avoid per-run API calls
   - **Decision needed**: Should this be built into the parser, or handled upstream by Paperclip export?

2. **Run ID Ambiguity**: `session_id` identifies a Claude Code session, but Paperclip may track a separate run ID. Need to clarify:
   - Is `session_id` stable across heartbeats/wakeups?
   - Or should we use the log filename (UUID) as the primary run identifier?

3. **Company Context**: Logs are stored under company UUIDs. Should htop:
   - Display all companies, or
   - Be scoped to a single company (configurable)?

4. **Log Retention**: No information available on Paperclip's log rotation policy. htop may need to handle:
   - Streaming new logs from disk
   - Detecting log file rotation
   - Handling missing/deleted logs gracefully

5. **Tool Call Details**: Tool calls are visible in `assistant.message.content[]` with type `tool_use`, but tool results appear in `user` events. Should htop:
   - Reconstruct tool → result pairs programmatically?
   - Or just count tool invocations for a simple view?

---

## Files & Paths Reference

| Component | Path | Format | Notes |
|-----------|------|--------|-------|
| Paperclip logs | `~/.paperclip/instances/default/data/run-logs/{companyId}/{agentId}/{runId}.ndjson` | NDJSON | One file per agent run |
| Claude Code sessions | `~/.claude/sessions/{pid}.json` | JSON | Session metadata only; actual logs are in Paperclip |
| Claude Code history | `~/.claude/history.jsonl` | JSONL | Command history; not useful for htop |

---

## Implementation Notes for Engineers

### Parsing Strategy

```go
// Pseudo-code for NDJSON parser
scanner := bufio.NewScanner(logFile)
for scanner.Scan() {
    line := scanner.Text()
    
    // Unmarshal outer wrapper
    var outer LogLine // { ts, stream, chunk }
    json.Unmarshal([]byte(line), &outer)
    
    // Parse inner event from chunk
    var event map[string]interface{}
    json.Unmarshal([]byte(outer.Chunk), &event)
    
    // Route by event type
    switch event["type"] {
    case "system":
        handleSystem(event)
    case "assistant":
        handleAssistant(event)
    // ... etc
    }
}
```

### Token Usage Aggregation

Token counts appear in two places; **add both**:

1. `result.usage` — Aggregate usage across all models
2. `result.modelUsage[modelName]` — Per-model breakdown

For example, if using Claude Haiku + Claude Sonnet in one run:

```json
"usage": {
  "input_tokens": 1000,
  "output_tokens": 500,
  ...
},
"modelUsage": {
  "claude-haiku-4-5-20251001": {
    "inputTokens": 600,
    "outputTokens": 300,
    "costUSD": 0.005
  },
  "claude-sonnet-4-6": {
    "inputTokens": 400,
    "outputTokens": 200,
    "costUSD": 0.050
  }
}
```

### Cost Calculation

- **Total cost** is in `result.total_cost_usd` (always authoritative)
- Per-model costs are in `result.modelUsage[model].costUSD`
- Sum of per-model costs should match total (with rounding)

### Timestamp Handling

- All timestamps are ISO8601 strings in `ts` fields
- Rate limit reset times are Unix timestamps (seconds) in `resetsAt` and `overageResetsAt`
- Remember: rate limit resets are in the future; compare with current time to show countdown

---

## Summary

| Aspect | Detail |
|--------|--------|
| **Log Format** | Newline-delimited JSON (NDJSON) with double-wrapped JSON (outer wrapper + chunk) |
| **Event Types** | 6 main types: system/init, assistant, user, rate_limit_event, result/*, system/api_retry |
| **Data Sources** | 100% from Paperclip/Claude Code; no external data required |
| **Key Metrics** | duration_ms, tokens, cost, status, model, agent_id, run_id |
| **Dashboard Use** | Filter by status (success/error), group by agent, sort by cost/duration, show active rate limits |
| **Parser Goal** | Normalize NDJSON → AgentRun struct for uniform display |

