package parser

import (
	"encoding/json"
	"time"
)

// Runtime identifies the execution environment for an agent run.
type Runtime string

const (
	RuntimePaperclip Runtime = "paperclip"
	RuntimeClaude    Runtime = "claude"
	RuntimeCodex     Runtime = "codex"
)

// AgentRun represents a complete agent execution from start to finish.
// It works across multiple runtimes (Paperclip, Claude Code, Codex).
type AgentRun struct {
	// Runtime & Source
	Runtime   Runtime // Which backend generated this run (paperclip|claude|codex)

	// Identifiers
	RunID     string // Log filename (UUID) - primary identifier
	AgentID   string // Parent directory name (Paperclip) or project slug (Claude)
	CompanyID string // Grandparent directory name (Paperclip only)
	SessionID string // Claude Code session UUID (secondary)

	// Timing
	StartTime  time.Time
	EndTime    time.Time
	DurationMS int64

	// Model & Configuration
	Model       string
	WorkDir     string
	Permissions string

	// Execution Status
	Status         string // success | error | cancelled
	Result         string // Output or error message
	TerminalReason string
	IsError        bool

	// Resource Usage
	TotalCostUSD       float64
	TotalInputTokens   int
	TotalOutputTokens  int
	ModelUsage         map[string]*ModelMetrics

	// Activity
	NumTurns      int
	NumToolCalls  int
	ToolCalls     []*ToolCall

	// Rate Limit
	RateLimitStatus  string
	RateLimitResetsAt int64
}

// ModelMetrics tracks token usage and cost per model.
type ModelMetrics struct {
	InputTokens            int
	OutputTokens           int
	CacheCreationTokens    int
	CacheReadTokens        int
	CostUSD                float64
	ContextWindow          int
	MaxOutputTokens        int
}

// ToolCall represents a tool invocation and its result.
type ToolCall struct {
	ID        string
	Name      string
	Input     map[string]interface{}
	Result    string
	IsError   bool
	StartTime time.Time // When the tool was invoked
	EndTime   time.Time // When the result was received
}

// ToolUsage aggregates metrics for a single tool across a session or fleet.
type ToolUsage struct {
	Name        string    // Tool name (e.g., "Read", "Bash", "WebFetch")
	CallCount   int       // Number of times tool was called
	ErrorCount  int       // Number of error results
	SuccessRate float64   // 0-1 indicating success ratio
	P50DurationMS float64 // Median execution time
	P95DurationMS float64 // 95th percentile execution time
	MaxDurationMS float64 // Longest single call
	TotalCostEst  float64 // Estimated cost contribution (rough heuristic)
}

// ToolHeatmap represents tool usage statistics for a fleet or session.
type ToolHeatmap struct {
	Tools      []*ToolUsage
	TotalCalls int       // Sum of all tool calls
	TotalErrors int      // Sum of all errors
	GeneratedAt time.Time
}

// LogLine represents the outer NDJSON wrapper.
// Chunk is stored as json.RawMessage to defer parsing until needed.
type LogLine struct {
	Ts     string          `json:"ts"`
	Stream string          `json:"stream"`
	Chunk  json.RawMessage `json:"chunk"`
}

// Event represents a parsed inner event (after unmarshaling chunk).
type Event struct {
	Type    string                 `json:"type"`
	Subtype string                 `json:"subtype,omitempty"`
	Data    map[string]interface{} `json:"-"`
	Raw     string                 `json:"raw,omitempty"` // For error reporting
}

// CurrentTask represents the current active task for a session.
type CurrentTask struct {
	ToolName    string    `json:"tool,omitempty"`         // e.g., "Read", "Bash", "WebFetch"
	ArgsSummary string    `json:"args_summary,omitempty"` // Human-readable args, e.g., "README.md"
	StartedAt   time.Time `json:"started_at"`             // When this task started
	ElapsedSec  int64     `json:"elapsed_sec"`            // Seconds elapsed since task start
	IsStalled   bool      `json:"is_stalled"`             // True if no activity for 30+ seconds while session running
	LastEventAt time.Time `json:"last_event_at"`          // Timestamp of last log event
}

// TaskHistory represents a historical task entry with completion info.
type TaskHistory struct {
	ToolName    string    `json:"tool"`
	ArgsSummary string    `json:"args_summary"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at"`
	DurationSec int64     `json:"duration_sec"`
	IsError     bool      `json:"is_error"`
	Result      string    `json:"result,omitempty"` // Brief result summary
}
