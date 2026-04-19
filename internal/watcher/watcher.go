package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Event represents a log file watcher event.
type Event struct {
	Type      string // "new" | "append" | "error"
	CompanyID string
	AgentID   string
	RunID     string // Log filename without .ndjson
	FilePath  string
	Error     error
}

// Watcher monitors the Paperclip log directory for new and updated log files.
type Watcher struct {
	watchDir  string
	fsWatcher *fsnotify.Watcher
	eventCh   chan *Event
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// Track seen files to detect "new" vs "append"
	mu        sync.RWMutex
	seenFiles map[string]bool // filepath -> seen
}

// NewWatcher creates a new log file watcher.
// watchDir should be the path to ~/.paperclip/instances/default/data/run-logs/
func NewWatcher(watchDir string) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	w := &Watcher{
		watchDir:  watchDir,
		fsWatcher: fsWatcher,
		eventCh:   make(chan *Event, 100), // Buffered to avoid blocking
		stopCh:    make(chan struct{}),
		seenFiles: make(map[string]bool),
	}

	return w, nil
}

// Start begins watching for log file changes.
func (w *Watcher) Start(ctx context.Context) error {
	// Add watch for the root directory
	if err := w.fsWatcher.Add(w.watchDir); err != nil {
		return fmt.Errorf("failed to add watch: %w", err)
	}

	// Recursively watch subdirectories
	if err := w.watchDirectory(w.watchDir); err != nil {
		return fmt.Errorf("failed to watch directory tree: %w", err)
	}

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// watchDirectory recursively adds watch to all subdirectories.
func (w *Watcher) watchDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subdir := filepath.Join(dir, entry.Name())
			if err := w.fsWatcher.Add(subdir); err != nil {
				// Some subdirs may be inaccessible; continue
				fmt.Printf("Warning: failed to watch %s: %v\n", subdir, err)
				continue
			}

			// Recurse
			if err := w.watchDirectory(subdir); err != nil {
				fmt.Printf("Warning: failed to watch subdirs of %s: %v\n", subdir, err)
			}
		}
	}

	return nil
}

// run is the main event loop.
func (w *Watcher) run(ctx context.Context) {
	defer w.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return

		case event := <-w.fsWatcher.Events:
			w.handleFsnotifyEvent(event)

		case err := <-w.fsWatcher.Errors:
			select {
			case w.eventCh <- &Event{
				Type:  "error",
				Error: err,
			}:
			case <-w.stopCh:
				return
			}
		}
	}
}

// handleFsnotifyEvent processes a filesystem event.
func (w *Watcher) handleFsnotifyEvent(fsEvent fsnotify.Event) {
	// Only care about .ndjson files
	if !strings.HasSuffix(fsEvent.Name, ".ndjson") {
		return
	}

	// Check if file is new or updated
	w.mu.RLock()
	seen := w.seenFiles[fsEvent.Name]
	w.mu.RUnlock()

	eventType := "append"
	if !seen {
		eventType = "new"
		w.mu.Lock()
		w.seenFiles[fsEvent.Name] = true
		w.mu.Unlock()
	}

	// Parse path: {watchDir}/{companyID}/{agentID}/{runID}.ndjson
	rel, err := filepath.Rel(w.watchDir, fsEvent.Name)
	if err != nil {
		select {
		case w.eventCh <- &Event{
			Type:     "error",
			Error:    fmt.Errorf("failed to parse path: %w", err),
			FilePath: fsEvent.Name,
		}:
		case <-w.stopCh:
		}
		return
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 {
		return // Wrong directory structure
	}

	companyID := parts[0]
	agentID := parts[1]
	runID := strings.TrimSuffix(parts[2], ".ndjson")

	// Also watch new directories
	if fsEvent.Op&fsnotify.Create == fsnotify.Create {
		if stat, err := os.Stat(fsEvent.Name); err == nil && stat.IsDir() {
			w.fsWatcher.Add(fsEvent.Name)
		}
	}

	// Send event to channel
	select {
	case w.eventCh <- &Event{
		Type:      eventType,
		CompanyID: companyID,
		AgentID:   agentID,
		RunID:     runID,
		FilePath:  fsEvent.Name,
	}:
	case <-w.stopCh:
	}
}

// Events returns a channel that receives watcher events.
func (w *Watcher) Events() <-chan *Event {
	return w.eventCh
}

// Stop stops the watcher.
func (w *Watcher) Stop() error {
	close(w.stopCh)
	w.wg.Wait()
	return w.fsWatcher.Close()
}
