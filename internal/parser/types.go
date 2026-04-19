package parser

import "time"

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
	ID      string
	Name    string
	Input   map[string]interface{}
	Result  string
	IsError bool
}

// LogLine represents the outer NDJSON wrapper.
type LogLine struct {
	Ts     string `json:"ts"`
	Stream string `json:"stream"`
	Chunk  string `json:"chunk"`
}

// Event represents a parsed inner event (after unmarshaling chunk).
type Event struct {
	Type    string                 `json:"type"`
	Subtype string                 `json:"subtype,omitempty"`
	Data    map[string]interface{} `json:"-"`
	Raw     string                 `json:"raw,omitempty"` // For error reporting
}
