package action

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Action represents a recorded action in the audit trail.
type Action struct {
	Timestamp       time.Time `json:"timestamp"`
	CallerAgentID   string    `json:"caller_agent_id"`
	ActionType      string    `json:"action_type"` // "kill", "pause"
	TargetSessionID string    `json:"target_session_id"`
	Reason          string    `json:"reason"`
}

// ActionLog is a file-based append-only action audit trail.
type ActionLog struct {
	filePath string
	file     *os.File
	mu       sync.Mutex
}

// NewActionLog creates or opens the action log file.
func NewActionLog() (*ActionLog, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	logPath := filepath.Join(home, ".config", "agent-htop", "actions.log")

	// Ensure directory exists
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open file in append mode
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open action log: %w", err)
	}

	return &ActionLog{
		filePath: logPath,
		file:     file,
		mu:       sync.Mutex{},
	}, nil
}

// Record appends an action to the audit trail as a JSON line.
func (al *ActionLog) Record(action *Action) error {
	if al == nil || al.file == nil {
		// If log is not initialized, skip silently
		return nil
	}

	al.mu.Lock()
	defer al.mu.Unlock()

	data, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("failed to marshal action: %w", err)
	}

	_, err = al.file.WriteString(string(data) + "\n")
	if err != nil {
		return fmt.Errorf("failed to write action log: %w", err)
	}

	return al.file.Sync()
}

// Close closes the action log file.
func (al *ActionLog) Close() error {
	if al.file == nil {
		return nil
	}
	return al.file.Close()
}
