package aggregator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/anomaly"
	"github.com/acunningham-ship-it/agent-htop/internal/api"
	"github.com/acunningham-ship-it/agent-htop/internal/parser"
	"github.com/acunningham-ship-it/agent-htop/internal/watcher"
)

// FleetState represents the current state of all agents in a company.
type FleetState struct {
	Agents    []*AgentView
	UpdatedAt time.Time
}

// AgentView represents a single agent's current state for dashboard display.
type AgentView struct {
	AgentID       string
	AgentName     string
	Status        string  // running | idle | error
	Model         string
	ElapsedMS     int64
	TotalCostUSD  float64
	InputTokens   int64
	OutputTokens  int64
	LastTool      string // Last tool_use name from logs
	IsError       bool
	Anomalies     []*anomaly.AnomalyEvent // Active anomalies
}

// Aggregator subscribes to watcher events, parses logs, and maintains fleet state.
type Aggregator struct {
	companyID  string
	logDir     string
	apiClient  *api.Client
	watcher    *watcher.Watcher
	detector   *anomaly.Detector

	mu        sync.RWMutex
	fleetState *FleetState
	stateCh   chan *FleetState

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewAggregator creates a new fleet state aggregator.
func NewAggregator(companyID, logDir string, apiClient *api.Client, w *watcher.Watcher) *Aggregator {
	return &Aggregator{
		companyID:  companyID,
		logDir:     logDir,
		apiClient:  apiClient,
		watcher:    w,
		detector:   anomaly.NewDetector(),
		fleetState: &FleetState{Agents: make([]*AgentView, 0)},
		stateCh:    make(chan *FleetState, 10),
		stopCh:     make(chan struct{}),
	}
}

// Start begins aggregating fleet state.
func (a *Aggregator) Start(ctx context.Context) error {
	// Pre-fetch all agent names for this company to avoid repeated API calls during log parsing
	fmt.Printf("[aggregator] Pre-fetching agent names for company %s\n", a.companyID)
	if err := a.apiClient.RefreshAgentCache(ctx, a.companyID); err != nil {
		fmt.Printf("[aggregator] Warning: failed to pre-fetch agents (will fall back to UUIDs): %v\n", err)
		// Continue - we'll fall back to UUIDs
	}

	// Initial load of existing logs
	if err := a.loadExistingLogs(ctx); err != nil {
		return fmt.Errorf("failed to load existing logs: %w", err)
	}

	// Subscribe to watcher events and anomaly events
	a.wg.Add(2)
	go a.run(ctx)
	go a.handleAnomalies(ctx)

	return nil
}

// loadExistingLogs scans the log directory and parses all existing .ndjson files.
func (a *Aggregator) loadExistingLogs(ctx context.Context) error {
	agentDir := filepath.Join(a.logDir, a.companyID)

	// Check if company directory exists
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		// No logs yet for this company
		fmt.Printf("[aggregator] No logs found for company %s\n", a.companyID)
		return nil
	}

	fmt.Printf("[aggregator] Loading logs for company %s from %s\n", a.companyID, agentDir)
	count := 0

	// Walk all agents and runs
	err := filepath.Walk(agentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && filepath.Ext(path) == ".ndjson" {
			count++
			// Parse the log file
			if err := a.parseAndUpdateLog(ctx, path); err != nil {
				fmt.Printf("Warning: failed to parse %s: %v\n", path, err)
				// Continue processing other logs
			}
		}

		return nil
	})

	fmt.Printf("[aggregator] Loaded %d log files, fleet has %d agents\n", count, len(a.fleetState.Agents))
	return err
}

// parseAndUpdateLog parses a single log file and updates fleet state.
func (a *Aggregator) parseAndUpdateLog(ctx context.Context, logPath string) error {
	// Extract company, agent, run IDs from path: company/agent/runID.ndjson
	rel, err := filepath.Rel(a.logDir, logPath)
	if err != nil {
		return err
	}

	// Normalize to forward slashes and split
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return fmt.Errorf("invalid log path structure: %s", rel)
	}

	companyID := parts[len(parts)-3]
	agentID := parts[len(parts)-2]
	runID := parts[len(parts)-1]
	runID = runID[:len(runID)-len(".ndjson")] // Remove extension

	// Parse the log file
	file, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("failed to open log: %w", err)
	}
	defer file.Close()

	p := parser.NewParser(companyID, agentID, runID)
	run, err := p.Parse(file)
	if err != nil {
		return fmt.Errorf("failed to parse log: %w", err)
	}

	// Get agent name for detector
	agentName := run.AgentID
	if name, err := a.apiClient.GetAgentName(ctx, run.AgentID); err == nil {
		agentName = name
	}

	// Process through anomaly detector
	a.detector.ProcessRun(run, agentName)

	// Update fleet state
	a.updateFleetState(ctx, run)

	return nil
}

// updateFleetState updates the fleet state with a new or updated agent run.
func (a *Aggregator) updateFleetState(ctx context.Context, run *parser.AgentRun) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Get agent name (with fallback to UUID)
	agentName := run.AgentID
	if name, err := a.apiClient.GetAgentName(ctx, run.AgentID); err == nil {
		agentName = name
	}

	// Create or update agent view
	agentView := &AgentView{
		AgentID:      run.AgentID,
		AgentName:    agentName,
		Model:        run.Model,
		TotalCostUSD: run.TotalCostUSD,
		InputTokens:  int64(run.TotalInputTokens),
		OutputTokens: int64(run.TotalOutputTokens),
		IsError:      run.IsError,
		ElapsedMS:    run.DurationMS,
	}

	// Determine status
	if run.IsError {
		agentView.Status = "error"
	} else if run.Status == "success" {
		agentView.Status = "idle"
	} else {
		agentView.Status = "running"
	}

	// Get last tool call name
	if len(run.ToolCalls) > 0 {
		lastTool := run.ToolCalls[len(run.ToolCalls)-1]
		agentView.LastTool = lastTool.Name
	}

	// Find and replace existing agent view or append new
	found := false
	for i, existing := range a.fleetState.Agents {
		if existing.AgentID == run.AgentID {
			a.fleetState.Agents[i] = agentView
			found = true
			break
		}
	}
	if !found {
		a.fleetState.Agents = append(a.fleetState.Agents, agentView)
	}

	// Sort agents: by status (error, running, idle) then by name
	a.sortAgents()
	a.fleetState.UpdatedAt = time.Now()

	// Broadcast updated state
	select {
	case a.stateCh <- a.fleetState:
	case <-a.stopCh:
	}
}

// sortAgents sorts the agent list for consistent display.
func (a *Aggregator) sortAgents() {
	sort.Slice(a.fleetState.Agents, func(i, j int) bool {
		ai, aj := a.fleetState.Agents[i], a.fleetState.Agents[j]

		// Primary sort: status (error > running > idle)
		statusOrder := map[string]int{"error": 0, "running": 1, "idle": 2}
		if statusOrder[ai.Status] != statusOrder[aj.Status] {
			return statusOrder[ai.Status] < statusOrder[aj.Status]
		}

		// Secondary sort: by name
		return ai.AgentName < aj.AgentName
	})
}

// run is the main event loop that subscribes to watcher events.
func (a *Aggregator) run(ctx context.Context) {
	defer a.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return

		case event := <-a.watcher.Events():
			if event == nil {
				return
			}

			if event.Type == "error" {
				fmt.Printf("Watcher error: %v\n", event.Error)
				continue
			}

			// Build full path
			logPath := filepath.Join(a.logDir, event.CompanyID, event.AgentID, event.RunID+".ndjson")

			// Parse and update
			if err := a.parseAndUpdateLog(ctx, logPath); err != nil {
				fmt.Printf("Warning: failed to parse %s: %v\n", logPath, err)
			}
		}
	}
}

// FleetState returns the current fleet state.
func (a *Aggregator) GetFleetState() *FleetState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Return a copy to avoid external mutation
	copy := &FleetState{
		Agents:    make([]*AgentView, len(a.fleetState.Agents)),
		UpdatedAt: a.fleetState.UpdatedAt,
	}
	for i, agent := range a.fleetState.Agents {
		agentCopy := *agent
		copy.Agents[i] = &agentCopy
	}

	return copy
}

// StateUpdates returns a channel that receives fleet state updates.
func (a *Aggregator) StateUpdates() <-chan *FleetState {
	return a.stateCh
}

// GetDetector returns the anomaly detector (used by notifiers).
func (a *Aggregator) GetDetector() *anomaly.Detector {
	return a.detector
}

// handleAnomalies listens to anomaly events and updates agent views.
func (a *Aggregator) handleAnomalies(ctx context.Context) {
	defer a.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case event := <-a.detector.Events():
			if event == nil {
				return
			}

			a.mu.Lock()
			// Find agent and add anomaly to their view
			for _, agent := range a.fleetState.Agents {
				if agent.AgentID == event.AgentID {
					// Check if this anomaly type already exists, update or append
					found := false
					for i, anom := range agent.Anomalies {
						if anom.AnomalyType == event.AnomalyType {
							agent.Anomalies[i] = event
							found = true
							break
						}
					}
					if !found {
						agent.Anomalies = append(agent.Anomalies, event)
					}
					break
				}
			}
			a.mu.Unlock()

			// Broadcast updated state
			select {
			case a.stateCh <- a.fleetState:
			case <-a.stopCh:
				return
			}
		}
	}
}

// Stop stops the aggregator.
func (a *Aggregator) Stop() {
	close(a.stopCh)
	a.detector.Stop()
	a.wg.Wait()
}
