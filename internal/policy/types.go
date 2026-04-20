package policy

import (
	"time"
)

// Policy defines a declarative rule that triggers actions based on conditions.
type Policy struct {
	Name        string       `toml:"name"`
	Description string       `toml:"description"`
	When        string       `toml:"when"`           // CEL-like expression: "session.cost > 5.00"
	Action      ActionType   `toml:"action"`         // kill | pause | alert | log
	Reason      string       `toml:"reason"`         // Explanation for the action
	Channel     string       `toml:"channel"`        // For alert action: "discord"
	DryRunTTL   string       `toml:"dry_run_ttl"`    // Duration string (e.g., "24h"), default "24h"
	Enabled     bool         `toml:"enabled"`        // Can be disabled
}

// ActionType identifies what action a policy triggers.
type ActionType string

const (
	ActionKill   ActionType = "kill"
	ActionPause  ActionType = "pause"
	ActionAlert  ActionType = "alert"
	ActionLog    ActionType = "log"
	ActionNone   ActionType = ""
)

// PolicyEvent tracks when a policy is evaluated and executed.
type PolicyEvent struct {
	PolicyName    string
	AgentID       string
	AgentName     string
	Action        ActionType
	Reason        string
	ExprValue     interface{} // The evaluated value from the `when` expression
	DryRun        bool
	Timestamp     time.Time
	Error         string // Empty if successful
	Message       string // Human-readable result
}

// SessionContext represents the session/agent state available for policy evaluation.
// This is passed to the expression evaluator.
type SessionContext struct {
	AgentID           string
	AgentName         string
	Status            string  // "running", "idle", "error", etc.
	Cost              float64 // Total cost USD
	CostLastHour      float64 // Cost in last hour
	ErrorsLast10Min   int     // Error count in last 10 minutes
	HeartbeatAgeSec   int64   // Seconds since last heartbeat
	RuntimeType       string  // "claude", "paperclip", "codex"
	SupportsKill      bool    // Can this runtime be killed?
	SupportsPause     bool    // Can this runtime be paused?
}

// PolicyState tracks the dry-run status of each policy per agent.
type PolicyState struct {
	EnabledAt       time.Time     // When dry-run started
	DryRunUntil     time.Time     // When dry-run expires
	TriggeredCount  int           // How many times this policy triggered
	LastTriggeredAt time.Time     // Last trigger time
}
