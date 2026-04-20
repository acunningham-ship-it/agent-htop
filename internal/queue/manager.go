package queue

import (
	"encoding/json"
	"fmt"
	"math/rand"
)

// Manager provides queue operations
type Manager struct {
	db     *DB
	queues map[string]string // queueName -> queueID
}

// NewManager creates a new queue manager
func NewManager(db *DB, queueDefs []QueueDef) (*Manager, error) {
	m := &Manager{
		db:     db,
		queues: make(map[string]string),
	}

	// Create queues from definitions
	for _, def := range queueDefs {
		queueID := fmt.Sprintf("queue-%x", rand.Uint64())
		if err := db.CreateQueue(queueID, def.Name, def.MaxRetries, def.RetentionDays); err != nil {
			return nil, fmt.Errorf("failed to create queue %s: %w", def.Name, err)
		}
		m.queues[def.Name] = queueID
	}

	return m, nil
}

// Enqueue adds a task to a queue
func (m *Manager) Enqueue(queueName string, taskSpec json.RawMessage) (string, error) {
	queueID, ok := m.queues[queueName]
	if !ok {
		return "", fmt.Errorf("queue not found: %s", queueName)
	}

	taskID := fmt.Sprintf("task-%x", rand.Uint64())
	maxRetries := 3 // Default; could come from config

	if err := m.db.Enqueue(taskID, queueID, taskSpec, maxRetries); err != nil {
		return "", err
	}

	return taskID, nil
}

// ClaimTask claims the next pending task from a queue
func (m *Manager) ClaimTask(queueName, agentID string) (*Task, error) {
	queueID, ok := m.queues[queueName]
	if !ok {
		return nil, fmt.Errorf("queue not found: %s", queueName)
	}

	return m.db.ClaimTask(queueID, agentID)
}

// CompleteTask marks a task as completed
func (m *Manager) CompleteTask(taskID string, result json.RawMessage) error {
	return m.db.CompleteTask(taskID, result)
}

// FailTask handles task failure with automatic retry or dead-letter
func (m *Manager) FailTask(taskID, reason string) error {
	return m.db.FailTask(taskID, reason)
}

// GetTask fetches a task by ID
func (m *Manager) GetTask(taskID string) (*Task, error) {
	return m.db.GetTask(taskID)
}

// ListTasks lists tasks in a queue
func (m *Manager) ListTasks(queueName, status string, limit int) ([]*Task, error) {
	queueID, ok := m.queues[queueName]
	if !ok {
		return nil, fmt.Errorf("queue not found: %s", queueName)
	}

	return m.db.ListTasks(queueID, status, limit)
}

// GetStats returns stats for a queue
func (m *Manager) GetStats(queueName string) (QueueStats, error) {
	queueID, ok := m.queues[queueName]
	if !ok {
		return QueueStats{}, fmt.Errorf("queue not found: %s", queueName)
	}

	return m.db.GetQueueStats(queueID, queueName)
}

// GetAllStats returns stats for all queues
func (m *Manager) GetAllStats() (QueueSnapshot, error) {
	stats, err := m.db.GetAllQueueStats()
	if err != nil {
		return QueueSnapshot{}, err
	}

	totalTasks := 0
	for _, s := range stats {
		totalTasks += s.Pending + s.Claimed + s.Completed + s.Failed
	}

	return QueueSnapshot{
		Queues:     stats,
		TotalTasks: totalTasks,
	}, nil
}

// GetQueueNames returns all queue names
func (m *Manager) GetQueueNames() []string {
	names := make([]string, 0, len(m.queues))
	for name := range m.queues {
		names = append(names, name)
	}
	return names
}
