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
	"github.com/acunningham-ship-it/agent-htop/internal/parser"
	"github.com/acunningham-ship-it/agent-htop/internal/sysinfo"
	"github.com/acunningham-ship-it/agent-htop/internal/watcher"
)

// FleetState represents the current state of all agents in a company.
type FleetState struct {
	Agents      []*AgentView
	HostMetrics *HostMetrics
	Processes   []*sysinfo.ProcessInfo
	UpdatedAt   time.Time
}

// HostMetrics contains aggregated host system metrics.
type HostMetrics struct {
	CPU    *sysinfo.CPUMetrics
	Memory *sysinfo.MemoryMetrics
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
	Runtime       parser.Runtime            // Which runtime this agent runs on
	Anomalies     []*anomaly.AnomalyEvent // Active anomalies
	Projection    *Projection              // Cost projection for today
}

// Aggregator subscribes to watcher events, parses logs, and maintains fleet state.
type Aggregator struct {
	companyID   string
	logDir      string
	agentNamer  AgentNamer
	watcher     *watcher.Watcher
	detector    *anomaly.Detector
	runtimes    map[parser.Runtime]bool // Which runtimes to include

	cpuCollector     *sysinfo.CPUCollector
	memoryCollector  *sysinfo.MemoryCollector
	processCollector *sysinfo.ProcessCollector

	mu              sync.RWMutex
	fleetState      *FleetState
	costTrackers    map[string]*AgentCostTracker // Per-agent cost tracking
	dailyAverages   map[string][]float64         // Per-agent daily cost history
	runHistory      []*parser.AgentRun           // Complete history of all runs for historical view
	stateCh         chan *FleetState

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewAggregator creates a new fleet state aggregator.
func NewAggregator(companyID, logDir string, agentNamer AgentNamer, w *watcher.Watcher) *Aggregator {
	return NewAggregatorWithRuntimes(companyID, logDir, agentNamer, w, []parser.Runtime{parser.RuntimePaperclip})
}

// NewAggregatorWithRuntimes creates a fleet state aggregator with custom runtimes.
func NewAggregatorWithRuntimes(companyID, logDir string, agentNamer AgentNamer, w *watcher.Watcher, runtimes []parser.Runtime) *Aggregator {
	runtimeMap := make(map[parser.Runtime]bool)
	for _, rt := range runtimes {
		runtimeMap[rt] = true
	}

	return &Aggregator{
		companyID:        companyID,
		logDir:           logDir,
		agentNamer:       agentNamer,
		watcher:          w,
		detector:         anomaly.NewDetector(),
		runtimes:         runtimeMap,
		cpuCollector:     sysinfo.NewCPUCollector(time.Second),
		memoryCollector:  sysinfo.NewMemoryCollector(time.Second),
		processCollector: sysinfo.NewProcessCollector(time.Second),
		fleetState:       &FleetState{Agents: make([]*AgentView, 0), HostMetrics: &HostMetrics{}, Processes: make([]*sysinfo.ProcessInfo, 0)},
		costTrackers:     make(map[string]*AgentCostTracker),
		dailyAverages:    make(map[string][]float64),
		runHistory:       make([]*parser.AgentRun, 0),
		stateCh:          make(chan *FleetState, 10),
		stopCh:           make(chan struct{}),
	}
}

// Start begins aggregating fleet state.
func (a *Aggregator) Start(ctx context.Context) error {
	// Start system metrics collectors
	if err := a.cpuCollector.Start(); err != nil {
		fmt.Printf("[aggregator] Warning: failed to start CPU collector: %v\n", err)
	}
	if err := a.memoryCollector.Start(); err != nil {
		fmt.Printf("[aggregator] Warning: failed to start memory collector: %v\n", err)
	}
	if err := a.processCollector.Start(); err != nil {
		fmt.Printf("[aggregator] Warning: failed to start process collector: %v\n", err)
	}

	// Start background goroutines FIRST so they can drain the anomaly event channel during log loading
	a.wg.Add(2)
	go a.run(ctx)
	go a.handleAnomalies(ctx)

	// Pre-fetch all agent names for this company to avoid repeated API calls during log parsing
	if a.agentNamer != nil {
		fmt.Printf("[aggregator] Pre-fetching agent names for company %s\n", a.companyID)
		if err := a.agentNamer.RefreshAgentCache(ctx, a.companyID); err != nil {
			fmt.Printf("[aggregator] Warning: failed to pre-fetch agents (will fall back to UUIDs): %v\n", err)
			// Continue - we'll fall back to UUIDs
		}
	}

	// Initial load of existing logs (Paperclip + Claude if configured)
	if err := a.loadExistingLogs(ctx); err != nil {
		return fmt.Errorf("failed to load existing logs: %w", err)
	}

	return nil
}

// loadExistingLogs scans the log directory and parses all existing .ndjson files.
func (a *Aggregator) loadExistingLogs(ctx context.Context) error {
	count := 0

	// Load Paperclip logs if included in runtimes
	if a.runtimes[parser.RuntimePaperclip] {
		agentDir := filepath.Join(a.logDir, a.companyID)

		// Check if company directory exists
		if _, err := os.Stat(agentDir); err == nil {
			fmt.Printf("[aggregator] Loading Paperclip logs for company %s from %s\n", a.companyID, agentDir)

			// Walk all agents and runs
			walkCount := 0
			err := filepath.Walk(agentDir, func(path string, info os.FileInfo, err error) error {
				walkCount++
				if err != nil {
					return err
				}

				if !info.IsDir() && filepath.Ext(path) == ".ndjson" {
					count++
					if count%100 == 0 {
						fmt.Printf("[aggregator] Processed %d log files\n", count)
					}
					// Parse the log file
					if err := a.parseAndUpdateLog(ctx, path); err != nil {
						fmt.Printf("Warning: failed to parse %s: %v\n", path, err)
						// Continue processing other logs
					}
				}

				return nil
			})
			if err != nil {
				fmt.Printf("Warning: failed to walk Paperclip logs: %v\n", err)
			}
		}
	}

	// Load Claude Code logs if included in runtimes
	if a.runtimes[parser.RuntimeClaude] {
		home, err := os.UserHomeDir()
		if err == nil {
			claudeProjectsDir := filepath.Join(home, ".claude", "projects")
			fmt.Printf("[aggregator] Loading Claude Code logs from %s\n", claudeProjectsDir)

			if _, err := os.Stat(claudeProjectsDir); err == nil {
				// Walk all projects
				err := filepath.Walk(claudeProjectsDir, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}

					if !info.IsDir() && filepath.Ext(path) == ".jsonl" {
						count++
						// Parse the log file
						if err := a.parseAndUpdateClaudeLog(ctx, path); err != nil {
							fmt.Printf("Warning: failed to parse %s: %v\n", path, err)
							// Continue processing other logs
						}
					}

					return nil
				})
				if err != nil {
					fmt.Printf("Warning: failed to walk Claude logs: %v\n", err)
				}
			}
		}
	}

	fmt.Printf("[aggregator] Loaded %d log files, fleet has %d agents\n", count, len(a.fleetState.Agents))
	return nil
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
	if a.agentNamer != nil {
		if name, err := a.agentNamer.GetAgentName(ctx, run.AgentID); err == nil && name != "" {
			agentName = name
		}
	}

	// Process through anomaly detector
	a.detector.ProcessRun(run, agentName)

	// Update fleet state
	a.updateFleetState(ctx, run)

	return nil
}

// parseAndUpdateClaudeLog parses a Claude Code log file and updates fleet state.
func (a *Aggregator) parseAndUpdateClaudeLog(ctx context.Context, logPath string) error {
	// Extract session ID from path: ~/.claude/projects/PROJECT_NAME/SESSION_ID.jsonl
	rel, err := filepath.Rel(filepath.Join(os.ExpandEnv("$HOME"), ".claude", "projects"), logPath)
	if err != nil {
		return err
	}

	// Normalize to forward slashes and split
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid Claude log path structure: %s", rel)
	}

	projectPath := parts[0]
	sessionID := filepath.Base(logPath)
	sessionID = sessionID[:len(sessionID)-len(".jsonl")] // Remove extension

	// Parse the log file using Claude parser
	file, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("failed to open log: %w", err)
	}
	defer file.Close()

	p := parser.NewClaudeParser(sessionID, projectPath)
	run, err := p.Parse(file)
	if err != nil {
		return fmt.Errorf("failed to parse log: %w", err)
	}

	// For Claude logs, derive agent ID from project path (use last component as agent identifier)
	// This is a heuristic since Claude logs don't have explicit Paperclip agent IDs
	if run.AgentID == "" {
		run.AgentID = filepath.Base(projectPath)
	}

	// Skip this run if it's not for the company we're monitoring
	// Claude logs don't have company ID, so we process all of them
	// (This is a limitation we could improve with log metadata)

	// Get agent name (may fail for Claude agents not in Paperclip)
	agentName := run.AgentID
	if a.agentNamer != nil {
		if name, err := a.agentNamer.GetAgentName(ctx, run.AgentID); err == nil && name != "" {
			agentName = name
		}
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

	// Add to run history for historical view
	a.runHistory = append(a.runHistory, run)

	now := time.Now()

	// Get or create cost tracker for this agent
	tracker, exists := a.costTrackers[run.AgentID]
	if !exists {
		tracker = NewAgentCostTracker(run.AgentID)
		a.costTrackers[run.AgentID] = tracker
	}

	// Add cost sample to tracker
	tracker.AddSample(run.TotalCostUSD, now)

	// Calculate daily average for this agent
	dailyAvg := RollingDailyAverage(a.dailyAverages[run.AgentID])

	// Calculate projection
	projection := tracker.CalculateProjection(now, dailyAvg)

	// Get agent name (with fallback to UUID)
	agentName := run.AgentID
	if a.agentNamer != nil {
		if name, err := a.agentNamer.GetAgentName(ctx, run.AgentID); err == nil && name != "" {
			agentName = name
		}
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
		Runtime:      run.Runtime,
		Projection:   projection,
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

	// Update host metrics
	if a.fleetState.HostMetrics == nil {
		a.fleetState.HostMetrics = &HostMetrics{}
	}
	a.fleetState.HostMetrics.CPU = a.cpuCollector.Get()
	a.fleetState.HostMetrics.Memory = a.memoryCollector.Get()

	// Update process list
	procList := a.processCollector.Get()
	a.fleetState.Processes = make([]*sysinfo.ProcessInfo, len(procList.Processes))
	copy(a.fleetState.Processes, procList.Processes)

	// Broadcast updated state (non-blocking - skip if nobody is listening)
	select {
	case a.stateCh <- a.fleetState:
	case <-a.stopCh:
	default:
		// If nobody is reading, skip this broadcast
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
	stateCopy := &FleetState{
		Agents:      make([]*AgentView, len(a.fleetState.Agents)),
		UpdatedAt:   a.fleetState.UpdatedAt,
		HostMetrics: &HostMetrics{},
	}
	for i, agent := range a.fleetState.Agents {
		agentCopy := *agent
		stateCopy.Agents[i] = &agentCopy
	}

	// Copy host metrics if available
	if a.fleetState.HostMetrics != nil {
		if a.fleetState.HostMetrics.CPU != nil {
			cpuCopy := *a.fleetState.HostMetrics.CPU
			if cpuCopy.PercentPerCore != nil {
				percentCopy := make([]float64, len(cpuCopy.PercentPerCore))
				copy(percentCopy, cpuCopy.PercentPerCore)
				cpuCopy.PercentPerCore = percentCopy
			}
			stateCopy.HostMetrics.CPU = &cpuCopy
		}
		if a.fleetState.HostMetrics.Memory != nil {
			memoryCopy := *a.fleetState.HostMetrics.Memory
			stateCopy.HostMetrics.Memory = &memoryCopy
		}
	}

	return stateCopy
}

// StateUpdates returns a channel that receives fleet state updates.
func (a *Aggregator) StateUpdates() <-chan *FleetState {
	return a.stateCh
}

// GetDetector returns the anomaly detector (used by notifiers).
func (a *Aggregator) GetDetector() *anomaly.Detector {
	return a.detector
}

// GetActiveRuntimes returns a list of runtimes that are currently active.
func (a *Aggregator) GetActiveRuntimes() []parser.Runtime {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var active []parser.Runtime
	// Return in a consistent order: paperclip, claude, codex
	if a.runtimes[parser.RuntimePaperclip] {
		active = append(active, parser.RuntimePaperclip)
	}
	if a.runtimes[parser.RuntimeClaude] {
		active = append(active, parser.RuntimeClaude)
	}
	if a.runtimes[parser.RuntimeCodex] {
		active = append(active, parser.RuntimeCodex)
	}
	return active
}

// GetRecentRunsForAgent returns the most recent N runs for a given agent ID.
func (a *Aggregator) GetRecentRunsForAgent(agentID string, limit int) []*parser.AgentRun {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var result []*parser.AgentRun
	// Iterate from the end of runHistory (most recent first)
	for i := len(a.runHistory) - 1; i >= 0 && len(result) < limit; i-- {
		if a.runHistory[i].AgentID == agentID {
			result = append(result, a.runHistory[i])
		}
	}

	return result
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

			// Broadcast updated state (non-blocking - skip if nobody is listening)
			select {
			case a.stateCh <- a.fleetState:
			case <-a.stopCh:
				return
			default:
				// If nobody is reading, skip this broadcast
			}
		}
	}
}

// Stop stops the aggregator.
func (a *Aggregator) Stop() {
	close(a.stopCh)
	a.detector.Stop()
	a.cpuCollector.Stop()
	a.memoryCollector.Stop()
	a.processCollector.Stop()
	a.wg.Wait()
}

// GetHistoricalView returns the 7-day historical view of aggregated metrics.
func (a *Aggregator) GetHistoricalView() *HistoricalView {
	a.mu.RLock()
	runHistory := make([]*parser.AgentRun, len(a.runHistory))
	copy(runHistory, a.runHistory)
	a.mu.RUnlock()

	now := time.Now()
	view := NewHistoricalView()

	// Map to collect runs by day
	dayMap := make(map[time.Time]*DayBucket)

	// Bucket all runs by day
	for _, run := range runHistory {
		dayKey := dayBucketKey(run.StartTime)

		if _, exists := dayMap[dayKey]; !exists {
			dayMap[dayKey] = &DayBucket{
				Date:           dayKey,
				SessionCount:   0,
				TotalCostUSD:   0,
				TotalInputTokens: 0,
				TotalOutputTokens: 0,
				TopModel:       "-",
				ErrorCount:     0,
				ModelBreakdown: make(map[string]int),
			}
		}

		bucket := dayMap[dayKey]
		bucket.SessionCount++
		bucket.TotalCostUSD += run.TotalCostUSD
		bucket.TotalInputTokens += int64(run.TotalInputTokens)
		bucket.TotalOutputTokens += int64(run.TotalOutputTokens)

		if run.IsError {
			bucket.ErrorCount++
		}

		// Track model usage
		if run.Model != "" {
			bucket.ModelBreakdown[run.Model]++
		}
	}

	// Calculate top model per day
	for _, bucket := range dayMap {
		if len(bucket.ModelBreakdown) > 0 {
			var topModel string
			var maxCount int
			for model, count := range bucket.ModelBreakdown {
				if count > maxCount {
					maxCount = count
					topModel = model
				}
			}
			bucket.TopModel = topModel
		}
	}

	// Convert map to sorted slice (oldest first)
	var days []*DayBucket
	for _, bucket := range dayMap {
		days = append(days, bucket)
	}

	sort.Slice(days, func(i, j int) bool {
		return days[i].Date.Before(days[j].Date)
	})

	for _, day := range days {
		// Only include days within last 7 days
		if daysBetween(day.Date, now) <= 6 {
			view.AddDay(day)
		}
	}

	// Pad to 7 days (fill empty days if needed)
	view.PadToSevenDays(now)
	view.GeneratedAt = time.Now()
	return view
}

// GetToolHeatmap returns a tool usage heatmap for all runs (or filtered by agent).
func (a *Aggregator) GetToolHeatmap(filterAgentID string) *parser.ToolHeatmap {
	a.mu.RLock()
	runHistory := make([]*parser.AgentRun, len(a.runHistory))
	copy(runHistory, a.runHistory)
	a.mu.RUnlock()

	return ComputeToolHeatmap(runHistory, filterAgentID)
}
