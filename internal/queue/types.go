package queue

import (
	"encoding/json"
	"time"
)

// QueueDef represents a queue definition from config
type QueueDef struct {
	Name         string `toml:"name"`
	MaxRetries   int    `toml:"max_retries"`
	RetentionDays int    `toml:"retention_days"`
}

// Task represents a queued task
type Task struct {
	ID         string          `json:"id"`
	QueueID    string          `json:"queue_id"`
	QueueName  string          `json:"queue_name"`
	Spec       json.RawMessage `json:"spec"`
	Status     string          `json:"status"` // pending, claimed, completed, failed
	ClaimedBy  string          `json:"claimed_by,omitempty"`
	ClaimedAt  *time.Time      `json:"claimed_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	RetryCount int             `json:"retry_count"`
	MaxRetries int             `json:"max_retries"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// TaskRun represents an execution of a task
type TaskRun struct {
	ID         string          `json:"id"`
	TaskID     string          `json:"task_id"`
	AgentID    string          `json:"agent_id,omitempty"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	EndedAt    *time.Time      `json:"ended_at,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Status     string          `json:"status"` // running, success, failed
	ErrorReason string         `json:"error_reason,omitempty"`
}

// QueueStats represents statistics for a queue
type QueueStats struct {
	Name      string `json:"name"`
	Pending   int    `json:"pending"`
	Claimed   int    `json:"claimed"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	AvgTimeMs int64  `json:"avg_time_ms"`
}

// QueueSnapshot represents the state of all queues at a point in time
type QueueSnapshot struct {
	Queues     []QueueStats `json:"queues"`
	TotalTasks int          `json:"total_tasks"`
	UpdatedAt  time.Time    `json:"updated_at"`
}
