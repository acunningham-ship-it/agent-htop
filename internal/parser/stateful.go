package parser

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// FileState tracks parsing progress for a single file.
type FileState struct {
	LastOffset   int64
	LastModTime  time.Time
	LastRunID    string // Track runID to detect rotation
	LastFileSize int64  // Track file size to detect rotation
}

// StatefulParser wraps Parser to provide incremental parsing with per-file offset tracking.
// It tracks byte offsets for each file and only reads new bytes on append events.
type StatefulParser struct {
	companyID string
	agentID   string

	mu    sync.RWMutex
	state map[string]*FileState // filepath -> FileState
}

// NewStatefulParser creates a new stateful parser for incremental file parsing.
func NewStatefulParser(companyID, agentID string) *StatefulParser {
	return &StatefulParser{
		companyID: companyID,
		agentID:   agentID,
		state:     make(map[string]*FileState),
	}
}

// ParseIncremental parses a file incrementally, reading only new bytes since the last parse.
// On the first call for a file (or after rotation), reads the entire file.
// On subsequent calls, seeks to the last known offset and reads only new lines.
// Returns the merged/new AgentRun data and whether this was a cold (full) parse.
func (sp *StatefulParser) ParseIncremental(filePath, runID string, file *os.File) (*AgentRun, bool, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Get file info for rotation detection
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("failed to stat file: %w", err)
	}

	currentSize := fileInfo.Size()
	currentModTime := fileInfo.ModTime()

	// Check if file exists in our state
	state, exists := sp.state[filePath]

	// Detect file rotation (size shrunk or runID changed)
	isColdParse := !exists || currentSize < state.LastFileSize || runID != state.LastRunID

	if isColdParse {
		// Full parse: read entire file from start
		if _, err := file.Seek(0, 0); err != nil {
			return nil, false, fmt.Errorf("failed to seek to start: %w", err)
		}

		parser := NewParser(sp.companyID, sp.agentID, runID)
		run, err := parser.Parse(file)
		if err != nil {
			return nil, false, err
		}

		// Update state with new offset
		sp.state[filePath] = &FileState{
			LastOffset:   currentSize,
			LastModTime:  currentModTime,
			LastRunID:    runID,
			LastFileSize: currentSize,
		}

		return run, true, nil
	}

	// Hot parse: read only new bytes
	if _, err := file.Seek(state.LastOffset, 0); err != nil {
		return nil, false, fmt.Errorf("failed to seek to offset %d: %w", state.LastOffset, err)
	}

	parser := NewParser(sp.companyID, sp.agentID, runID)
	run, err := parser.Parse(file)
	if err != nil {
		return nil, false, err
	}

	// Update state
	sp.state[filePath] = &FileState{
		LastOffset:   currentSize,
		LastModTime:  currentModTime,
		LastRunID:    runID,
		LastFileSize: currentSize,
	}

	return run, false, nil
}

// ParseFull performs a full parse of a file (used for cold starts or manual resets).
// Reads the entire file from offset 0.
func (sp *StatefulParser) ParseFull(filePath, runID string, file *os.File) (*AgentRun, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if _, err := file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to seek to start: %w", err)
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	parser := NewParser(sp.companyID, sp.agentID, runID)
	run, err := parser.Parse(file)
	if err != nil {
		return nil, err
	}

	// Update state
	sp.state[filePath] = &FileState{
		LastOffset:   fileInfo.Size(),
		LastModTime:  fileInfo.ModTime(),
		LastRunID:    runID,
		LastFileSize: fileInfo.Size(),
	}

	return run, nil
}

// ParseReader parses from an io.Reader without state tracking (used for testing).
// This is for compatibility with existing code that passes io.Reader.
func (sp *StatefulParser) ParseReader(runID string, reader io.Reader) (*AgentRun, error) {
	parser := NewParser(sp.companyID, sp.agentID, runID)
	return parser.Parse(reader)
}

// ForgetFile removes state tracking for a file (call when file is deleted).
func (sp *StatefulParser) ForgetFile(filePath string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	delete(sp.state, filePath)
}

// Reset clears all tracked state. Useful for testing or after major changes.
func (sp *StatefulParser) Reset() {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.state = make(map[string]*FileState)
}

// GetState returns a copy of the current state for a file (for testing/debugging).
func (sp *StatefulParser) GetState(filePath string) *FileState {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if state, exists := sp.state[filePath]; exists {
		stateCopy := *state
		return &stateCopy
	}
	return nil
}
