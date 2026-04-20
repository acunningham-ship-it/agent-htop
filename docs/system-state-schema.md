# SystemState Schema (v1)

**Version**: 1  
**Date**: 2026-04-20  
**Status**: Stable

## Overview

The **SystemState** is the unified, complete snapshot of system and fleet information. It is the response format for:

1. **MCP Tool**: `get_system_state()` — agents call this to get everything in one query
2. **CLI**: `agent-htop --once --json --full` — outputs the exact same structure

The goal is to provide a single query that gives agents the complete picture: CPU, memory, processes, uptime, and (in future versions) network, disk, GPU, task queues, and policy metrics.

---

## SystemState Structure

```json
{
  "schemaVersion": 1,
  "timestamp": "2026-04-20T20:00:00.000Z",
  "host": {
    "cpu": {
      "percentPerCore": [15.2, 22.5, 18.3, 12.1],
      "averagePercent": 17.0,
      "load1Min": 1.2,
      "load5Min": 1.5,
      "load15Min": 1.3,
      "logicalCores": 4,
      "physicalCores": 2,
      "uptime": 86400,
      "updatedAt": "2026-04-20T19:59:50.000Z"
    },
    "memory": {
      "totalMB": 16384,
      "usedMB": 8192,
      "freeMB": 8192,
      "availableMB": 12000,
      "usedPercent": 50.0,
      "swapTotalMB": 2048,
      "swapUsedMB": 256,
      "swapFreeMB": 1792,
      "swapUsedPercent": 12.5,
      "cacheMB": 2048,
      "updatedAt": "2026-04-20T19:59:50.000Z"
    },
    "uptime": {
      "seconds": 86400,
      "updatedAt": "2026-04-20T19:59:50.000Z"
    },
    "alerts": [
      {
        "rule": "oom_risk",
        "severity": "critical",
        "message": "Available RAM critically low: 150MB / 16384MB (99.1% free)",
        "since": "2026-04-20T19:55:00.000Z",
        "updatedAt": "2026-04-20T19:59:50.000Z"
      }
    ],
    "updatedAt": "2026-04-20T19:59:50.000Z"
  },
  "processes": {
    "processes": [
      {
        "pid": 1234,
        "ppid": 1,
        "name": "agent-runner",
        "cmdline": "/usr/bin/agent-runner --config /etc/agent.yaml",
        "user": "agent",
        "status": "S",
        "cpu_percent": 5.2,
        "mem_percent": 2.3,
        "mem_mb": 380,
        "create_time": 1713607200,
        "updatedAt": "2026-04-20T19:59:50.000Z"
      }
    ],
    "updatedAt": "2026-04-20T19:59:50.000Z"
  }
}
```

---

## Field Definitions

### Root Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schemaVersion` | int | ✓ | Schema version (currently 1) |
| `timestamp` | ISO8601 | ✓ | UTC timestamp when this snapshot was generated |
| `host` | HostState | ✓ | Host-level system metrics |
| `processes` | ProcessList | ✓ | List of processes on the system |

### HostState

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cpu` | CPUMetrics | ✓ | CPU usage and load statistics |
| `memory` | MemoryMetrics | ✓ | Memory usage statistics |
| `uptime` | UptimeInfo | ✓ | System uptime information |
| `alerts` | Alert[] | ✗ | Active health alerts (omitted if empty) |
| `updatedAt` | ISO8601 | ✓ | When this host state was last collected |

### CPUMetrics

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `percentPerCore` | number[] | ✓ | Per-core CPU percentage (0-100) |
| `averagePercent` | number | ✓ | Average CPU percentage across all cores |
| `load1Min` | number | ✓ | 1-minute load average |
| `load5Min` | number | ✓ | 5-minute load average |
| `load15Min` | number | ✓ | 15-minute load average |
| `logicalCores` | int | ✓ | Number of logical CPU cores |
| `physicalCores` | int | ✓ | Number of physical CPU cores |
| `uptime` | uint64 | ✓ | System uptime in seconds |
| `updatedAt` | ISO8601 | ✓ | When this metric was collected |

### MemoryMetrics

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `totalMB` | uint64 | ✓ | Total RAM in MB |
| `usedMB` | uint64 | ✓ | Used RAM in MB |
| `freeMB` | uint64 | ✓ | Free RAM in MB |
| `availableMB` | uint64 | ✓ | Available RAM in MB (includes cached) |
| `usedPercent` | number | ✓ | Used percentage (0-100) |
| `swapTotalMB` | uint64 | ✓ | Total swap in MB |
| `swapUsedMB` | uint64 | ✓ | Used swap in MB |
| `swapFreeMB` | uint64 | ✓ | Free swap in MB |
| `swapUsedPercent` | number | ✓ | Swap used percentage (0-100) |
| `cacheMB` | uint64 | ✓ | OS cache in MB |
| `updatedAt` | ISO8601 | ✓ | When this metric was collected |

### UptimeInfo

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `seconds` | uint64 | ✓ | System uptime in seconds |
| `updatedAt` | ISO8601 | ✓ | When this metric was collected |

### ProcessList

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `processes` | ProcessInfo[] | ✓ | List of all running processes |
| `updatedAt` | ISO8601 | ✓ | When this list was collected |

### ProcessInfo

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `pid` | int32 | ✓ | Process ID |
| `ppid` | int32 | ✓ | Parent process ID |
| `name` | string | ✓ | Process name |
| `cmdline` | string | ✓ | Full command line |
| `user` | string | ✓ | User running the process |
| `status` | string | ✓ | Process status (S/R/Z/etc) |
| `cpu_percent` | number | ✓ | CPU percentage (0-100) |
| `mem_percent` | number | ✓ | Memory percentage (0-100) |
| `mem_mb` | uint64 | ✓ | Memory used in MB (RSS) |
| `create_time` | int64 | ✓ | Process creation time (Unix seconds) |
| `updatedAt` | ISO8601 | ✓ | When this process info was collected |

---

## Usage Examples

### Get System State (JSON Output)

```bash
# Full system state snapshot
agent-htop --once --json --full

# Just fleet state (agents + CPU/memory summary)
agent-htop --once --json
```

### MCP Tool Integration

In future versions, agents will call:

```python
# Pseudocode for Claude Code agent
result = mcp.call_tool("get_system_state", {})
# Returns complete SystemState struct as JSON
```

This replaces the need for multiple targeted queries:
- ~~`get_host_metrics()`~~ → Now included in `get_system_state()`
- ~~`list_processes()`~~ → Now included in `get_system_state()`
- ~~`get_uptime()`~~ → Now included in `get_system_state()`

---

## Future Extensions

The schema is versioned to allow safe extensions:

### v2 Planned Additions
- `network` — Network interface stats (bandwidth, dropped packets, errors)
- `disk` — Disk space per mount point
- `gpu` — GPU utilization (if available)
- `agentSessions` — Paperclip agent session info
- `taskQueues` — Task queue depths per agent
- `anomalies` — Detected anomalies from detector
- `policyTriggers` — Policy engine triggers

### Backwards Compatibility

- New fields will be added to root level or nested objects
- Existing fields will never change meaning or type
- Old clients can safely ignore unknown fields
- Version 2 schema will be announced with sufficient lead time

---

## Performance Requirements

- **Response time**: <100ms on a typical laptop
- **Cache TTL**: ~1 second (parallel queries within 1s see same snapshot)
- **Typical size**: 2-5 KB JSON (compressed)

---

## Stability Guarantees

This schema is **stable** and can be relied upon by agents and tools:

- `schemaVersion` will increment only on breaking changes
- Field names and types are frozen for v1
- New optional fields may be added within v1 (see Future Extensions)
- The schema will be versioned in git tags and docs

---

## Agent Task Information

The **AgentTaskInfo** is returned by the `get_agent_task(agent_id)` MCP tool. It provides detailed task tracking for a specific agent.

```json
{
  "agent_id": "8341cbd0-bab0-4fab-98fa-47b512f34b9c",
  "agent_name": "Backend-Orch",
  "status": "running",
  "current_task": {
    "tool": "Read",
    "args_summary": "README.md",
    "started_at": "2026-04-20T20:35:47.821Z",
    "elapsed_sec": 4,
    "is_stalled": false,
    "last_event_at": "2026-04-20T20:35:51.821Z"
  },
  "task_history": [
    {
      "tool": "Bash",
      "args_summary": "git status",
      "started_at": "2026-04-20T20:35:30.000Z",
      "ended_at": "2026-04-20T20:35:35.000Z",
      "duration_sec": 5,
      "is_error": false,
      "result": "On branch main"
    }
  ],
  "updated_at": "2026-04-20T20:35:51.821Z"
}
```

### CurrentTask Structure

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `tool` | string | ✓ | Tool name (Read, Bash, WebFetch, etc) |
| `args_summary` | string | ✗ | Human-readable argument summary (e.g., "README.md", "npm test") |
| `started_at` | ISO8601 | ✓ | When the tool invocation started |
| `elapsed_sec` | int64 | ✓ | Seconds elapsed since task start |
| `is_stalled` | bool | ✓ | True if tool has run >30s with no completion |
| `last_event_at` | ISO8601 | ✓ | Timestamp of last log event for this task |

### TaskHistory Structure

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `tool` | string | ✓ | Tool name |
| `args_summary` | string | ✗ | Argument summary |
| `started_at` | ISO8601 | ✓ | Task start time |
| `ended_at` | ISO8601 | ✓ | Task completion time |
| `duration_sec` | int64 | ✓ | Total duration in seconds |
| `is_error` | bool | ✓ | Whether the task errored |
| `result` | string | ✗ | Brief result summary |

### Usage

Supervisor agents call this MCP tool to check if another agent is making progress:

```python
# Check what CRMOpsManager is doing
task_info = mcp.call_tool("get_agent_task", {
  "agent_id": "aa397f6f-agent-id"
})

if task_info.current_task.is_stalled:
    print(f"WARNING: {task_info.agent_name} stalled on {task_info.current_task.tool}")
else:
    print(f"{task_info.agent_name}: {task_info.current_task.tool} ({task_info.current_task.elapsed_sec}s)")
```

---

## See Also

- [Log Schema](log-schema.md) — Paperclip/Claude Code agent run logs
- [Main README](../README.md) — agent-htop project overview
