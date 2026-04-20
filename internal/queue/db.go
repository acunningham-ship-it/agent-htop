package queue

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB manages SQLite queue database
type DB struct {
	conn *sql.DB
}

// NewDB opens or creates the queue database
func NewDB(configDir string) (*DB, error) {
	// Ensure directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config dir: %w", err)
	}

	dbPath := filepath.Join(configDir, "queue.db")
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open queue database: %w", err)
	}

	// Test connection
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping queue database: %w", err)
	}

	db := &DB{conn: conn}

	// Initialize schema if needed
	if err := db.initSchema(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

// initSchema creates tables if they don't exist
func (db *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS queues (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		max_retries INTEGER DEFAULT 3,
		retention_days INTEGER DEFAULT 7,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		queue_id TEXT NOT NULL,
		spec TEXT NOT NULL,
		status TEXT DEFAULT 'pending',
		claimed_by TEXT,
		claimed_at TIMESTAMP,
		completed_at TIMESTAMP,
		retry_count INTEGER DEFAULT 0,
		max_retries INTEGER DEFAULT 3,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (queue_id) REFERENCES queues(id)
	);

	CREATE TABLE IF NOT EXISTS task_runs (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		agent_id TEXT,
		started_at TIMESTAMP,
		ended_at TIMESTAMP,
		result TEXT,
		status TEXT,
		error_reason TEXT,
		FOREIGN KEY (task_id) REFERENCES tasks(id)
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_queue_status ON tasks(queue_id, status);
	CREATE INDEX IF NOT EXISTS idx_tasks_claimed_by ON tasks(claimed_by);
	CREATE INDEX IF NOT EXISTS idx_task_runs_task_id ON task_runs(task_id);
	`

	if _, err := db.conn.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// GetQueueID returns the ID for a queue by name, or empty string if not found
func (db *DB) GetQueueID(name string) (string, error) {
	var id string
	err := db.conn.QueryRow("SELECT id FROM queues WHERE name = ?", name).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// CreateQueue creates a new queue definition
func (db *DB) CreateQueue(queueID, name string, maxRetries, retentionDays int) error {
	_, err := db.conn.Exec(
		"INSERT OR IGNORE INTO queues (id, name, max_retries, retention_days) VALUES (?, ?, ?, ?)",
		queueID, name, maxRetries, retentionDays,
	)
	return err
}

// Enqueue adds a task to a queue
func (db *DB) Enqueue(taskID, queueID string, spec json.RawMessage, maxRetries int) error {
	_, err := db.conn.Exec(
		"INSERT INTO tasks (id, queue_id, spec, status, max_retries) VALUES (?, ?, ?, 'pending', ?)",
		taskID, queueID, spec, maxRetries,
	)
	return err
}

// ClaimTask atomically claims the next pending task from a queue
func (db *DB) ClaimTask(queueID, agentID string) (*Task, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Find oldest pending task
	var taskID, spec string
	err = tx.QueryRow(
		"SELECT id, spec FROM tasks WHERE queue_id = ? AND status = 'pending' ORDER BY created_at ASC LIMIT 1",
		queueID,
	).Scan(&taskID, &spec)
	if err == sql.ErrNoRows {
		return nil, nil // No pending tasks
	}
	if err != nil {
		return nil, err
	}

	// Claim it
	now := time.Now()
	_, err = tx.Exec(
		"UPDATE tasks SET status = 'claimed', claimed_by = ?, claimed_at = ?, updated_at = ? WHERE id = ?",
		agentID, now, now, taskID,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	task := &Task{
		ID:        taskID,
		QueueID:   queueID,
		Spec:      json.RawMessage(spec),
		Status:    "claimed",
		ClaimedBy: agentID,
	}
	task.ClaimedAt = &now
	return task, nil
}

// CompleteTask marks a task as completed
func (db *DB) CompleteTask(taskID string, result json.RawMessage) error {
	now := time.Now()
	_, err := db.conn.Exec(
		"UPDATE tasks SET status = 'completed', completed_at = ?, updated_at = ? WHERE id = ?",
		now, now, taskID,
	)
	return err
}

// FailTask handles task failure with retry logic
func (db *DB) FailTask(taskID, reason string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get current retry count and max retries
	var retryCount, maxRetries int
	err = tx.QueryRow(
		"SELECT retry_count, max_retries FROM tasks WHERE id = ?",
		taskID,
	).Scan(&retryCount, &maxRetries)
	if err != nil {
		return err
	}

	now := time.Now()

	// Check if we should retry
	if retryCount < maxRetries {
		// Reschedule as pending
		_, err = tx.Exec(
			"UPDATE tasks SET status = 'pending', claimed_by = NULL, claimed_at = NULL, retry_count = ?, updated_at = ? WHERE id = ?",
			retryCount+1, now, taskID,
		)
	} else {
		// Park in failed status
		_, err = tx.Exec(
			"UPDATE tasks SET status = 'failed', claimed_by = NULL, claimed_at = NULL, updated_at = ? WHERE id = ?",
			now, taskID,
		)
	}

	if err != nil {
		return err
	}

	// Record task run
	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	_, err = tx.Exec(
		"INSERT INTO task_runs (id, task_id, status, error_reason) VALUES (?, ?, 'failed', ?)",
		runID, taskID, reason,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetTask fetches a task by ID
func (db *DB) GetTask(taskID string) (*Task, error) {
	task := &Task{}
	var claimedAt, completedAt sql.NullTime

	err := db.conn.QueryRow(
		"SELECT t.id, t.queue_id, q.name, t.spec, t.status, t.claimed_by, t.claimed_at, t.completed_at, t.retry_count, t.max_retries, t.created_at, t.updated_at FROM tasks t LEFT JOIN queues q ON t.queue_id = q.id WHERE t.id = ?",
		taskID,
	).Scan(&task.ID, &task.QueueID, &task.QueueName, &task.Spec, &task.Status, &task.ClaimedBy, &claimedAt, &completedAt, &task.RetryCount, &task.MaxRetries, &task.CreatedAt, &task.UpdatedAt)

	if err != nil {
		return nil, err
	}

	if claimedAt.Valid {
		task.ClaimedAt = &claimedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}

	return task, nil
}

// ListTasks lists tasks in a queue by status
func (db *DB) ListTasks(queueID, status string, limit int) ([]*Task, error) {
	var rows *sql.Rows
	var err error

	if status == "" {
		rows, err = db.conn.Query(
			"SELECT id, queue_id, spec, status, claimed_by, claimed_at, completed_at, retry_count, max_retries, created_at, updated_at FROM tasks WHERE queue_id = ? ORDER BY created_at DESC LIMIT ?",
			queueID, limit,
		)
	} else {
		rows, err = db.conn.Query(
			"SELECT id, queue_id, spec, status, claimed_by, claimed_at, completed_at, retry_count, max_retries, created_at, updated_at FROM tasks WHERE queue_id = ? AND status = ? ORDER BY created_at DESC LIMIT ?",
			queueID, status, limit,
		)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		task := &Task{}
		var claimedAt, completedAt sql.NullTime
		err := rows.Scan(&task.ID, &task.QueueID, &task.Spec, &task.Status, &task.ClaimedBy, &claimedAt, &completedAt, &task.RetryCount, &task.MaxRetries, &task.CreatedAt, &task.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if claimedAt.Valid {
			task.ClaimedAt = &claimedAt.Time
		}
		if completedAt.Valid {
			task.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

// GetQueueStats returns statistics for a queue
func (db *DB) GetQueueStats(queueID, queueName string) (QueueStats, error) {
	stats := QueueStats{Name: queueName}

	// Get counts by status
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM tasks WHERE queue_id = ? AND status = 'pending'",
		queueID,
	).Scan(&stats.Pending)
	if err != nil {
		return stats, err
	}

	err = db.conn.QueryRow(
		"SELECT COUNT(*) FROM tasks WHERE queue_id = ? AND status = 'claimed'",
		queueID,
	).Scan(&stats.Claimed)
	if err != nil {
		return stats, err
	}

	err = db.conn.QueryRow(
		"SELECT COUNT(*) FROM tasks WHERE queue_id = ? AND status = 'completed'",
		queueID,
	).Scan(&stats.Completed)
	if err != nil {
		return stats, err
	}

	err = db.conn.QueryRow(
		"SELECT COUNT(*) FROM tasks WHERE queue_id = ? AND status = 'failed'",
		queueID,
	).Scan(&stats.Failed)
	if err != nil {
		return stats, err
	}

	// Get average completion time (completed_at - created_at) in ms
	var avgMs sql.NullInt64
	err = db.conn.QueryRow(
		"SELECT AVG(CAST((julianday(completed_at) - julianday(created_at)) * 86400000 AS INTEGER)) FROM tasks WHERE queue_id = ? AND status = 'completed'",
		queueID,
	).Scan(&avgMs)
	if err != nil {
		return stats, err
	}
	if avgMs.Valid {
		stats.AvgTimeMs = avgMs.Int64
	}

	return stats, nil
}

// GetAllQueueStats returns statistics for all queues
func (db *DB) GetAllQueueStats() ([]QueueStats, error) {
	rows, err := db.conn.Query(
		"SELECT id, name FROM queues ORDER BY name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []QueueStats
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}

		qstats, err := db.GetQueueStats(id, name)
		if err != nil {
			return nil, err
		}
		stats = append(stats, qstats)
	}

	return stats, rows.Err()
}
