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
	"github.com/acunningham-ship-it/agent-htop/internal/health"
	"github.com/acunningham-ship-it/agent-htop/internal/parser"
	"github.com/acunningham-ship-it/agent-htop/internal/policy"
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
	CPU     *sysinfo.CPUMetrics
	Memory  *sysinfo.MemoryMetrics
	Network *sysinfo.NetworkMetrics
}

// PolicyEvaluator evaluates policies against session context.
type PolicyEvaluator interface {
	Evaluate(ctx *policy.SessionContext) []*policy.PolicyEvent
	Events() <-chan *policy.PolicyEvent
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
	CurrentTask   *parser.CurrentTask      // Current active task
	TaskHistory   []*parser.TaskHistory    // Historical task entries (last 10)
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
	diskCollector    *sysinfo.DiskCollector
	gpuCollector     *sysinfo.GPUCollector
	networkCollector *sysinfo.NetworkCollector
	processCollector *sysinfo.ProcessCollector
	alertEvaluator   *health.AlertEvaluator
	policyEngine     PolicyEvaluator // Policy evaluator for guardrail actions

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
		diskCollector:    sysinfo.NewDiskCollector(time.Second),
		gpuCollector:     sysinfo.NewGPUCollector(time.Second),
		networkCollector: sysinfo.NewNetworkCollector(time.Second),
		processCollector: sysinfo.NewProcessCollector(time.Second),
		alertEvaluator:   health.NewAlertEvaluator(),
		fleetState:       &FleetState{Agents: make([]*AgentView, 0), HostMetrics: &HostMetrics{}, Processes: make([]*sysinfo.ProcessInfo, 0)},
		costTrackers:     make(map[string]*AgentCostTracker),
		dailyAverages:    make(map[string][]float64),
		runHistory:       make([]*parser.AgentRun, 0),
		stateCh:          make(chan *FleetState, 10),
		stopCh:           make(chan struct{}),
	}
}

// SetPolicyEngine sets the policy engine for this aggregator.
func (a *Aggregator) SetPolicyEngine(engine PolicyEvaluator) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if engine != nil {
		a.policyEngine = engine
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
	if err := a.diskCollector.Start(); err != nil {
		fmt.Printf("[aggregator] Warning: failed to start disk collector: %v\n", err)
	}
	if err := a.gpuCollector.Start(); err != nil {
		fmt.Printf("[aggregator] Warning: failed to start GPU collector: %v\n", err)
	}
	if err := a.networkCollector.Start(); err != nil {
		fmt.Printf("[aggregator] Warning: failed to start network collector: %v\n", err)
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
		CurrentTask:  computeCurrentTask(run, now),
		TaskHistory:  buildTaskHistory(run),
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
	cpuMetrics := a.cpuCollector.Get()
	memMetrics := a.memoryCollector.Get()
	networkMetrics := a.networkCollector.Get()
	a.fleetState.HostMetrics.CPU = cpuMetrics
	a.fleetState.HostMetrics.Memory = memMetrics
	a.fleetState.HostMetrics.Network = networkMetrics

	// Evaluate health alerts
	a.evaluateAlerts(cpuMetrics, memMetrics)

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
		Processes:   make([]*sysinfo.ProcessInfo, len(a.fleetState.Processes)),
		UpdatedAt:   a.fleetState.UpdatedAt,
		HostMetrics: &HostMetrics{},
	}
	for i, agent := range a.fleetState.Agents {
		agentCopy := *agent
		stateCopy.Agents[i] = &agentCopy
	}

	// Copy processes
	for i, proc := range a.fleetState.Processes {
		procCopy := *proc
		stateCopy.Processes[i] = &procCopy
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
		if a.fleetState.HostMetrics.Network != nil {
			networkCopy := *a.fleetState.HostMetrics.Network
			// Deep copy interfaces slice
			if networkCopy.Interfaces != nil {
				interfacesCopy := make([]sysinfo.InterfaceMetrics, len(networkCopy.Interfaces))
				copy(interfacesCopy, networkCopy.Interfaces)
				networkCopy.Interfaces = interfacesCopy
			}
			// Deep copy WiFi if present
			if networkCopy.WiFi != nil {
				wifiCopy := *networkCopy.WiFi
				networkCopy.WiFi = &wifiCopy
			}
			stateCopy.HostMetrics.Network = &networkCopy
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

// GetSessionByID returns a session (AgentRun) by RunID or SessionID.
func (a *Aggregator) GetSessionByID(id string) *parser.AgentRun {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, run := range a.runHistory {
		if run.RunID == id || run.SessionID == id {
			// Return a copy to avoid external mutation
			runCopy := *run
			return &runCopy
		}
	}
	return nil
}

// ListSessions returns all sessions, optionally filtered by runtime and status.
func (a *Aggregator) ListSessions(runtimeFilter *parser.Runtime, statusFilter *string) []*parser.AgentRun {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var result []*parser.AgentRun

	for _, run := range a.runHistory {
		// Check runtime filter
		if runtimeFilter != nil && run.Runtime != *runtimeFilter {
			continue
		}

		// Check status filter
		if statusFilter != nil && run.Status != *statusFilter {
			continue
		}

		// Add copy to result
		runCopy := *run
		result = append(result, &runCopy)
	}

	// Sort by StartTime descending (most recent first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartTime.After(result[j].StartTime)
	})

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
	a.diskCollector.Stop()
	a.gpuCollector.Stop()
	a.networkCollector.Stop()
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

// GetAgentTask returns the current task and history for a given agent ID.
// This is the data structure returned by the get_agent_task() MCP tool.
func (a *Aggregator) GetAgentTask(agentID string) *AgentTaskInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Find the agent in fleet state
	for _, agent := range a.fleetState.Agents {
		if agent.AgentID == agentID {
			return &AgentTaskInfo{
				AgentID:     agentID,
				AgentName:   agent.AgentName,
				Status:      agent.Status,
				CurrentTask: agent.CurrentTask,
				TaskHistory: agent.TaskHistory,
				UpdatedAt:   a.fleetState.UpdatedAt,
			}
		}
	}

	return nil
}

// AgentTaskInfo is the response structure for the get_agent_task() MCP tool.
type AgentTaskInfo struct {
	AgentID     string                `json:"agent_id"`
	AgentName   string                `json:"agent_name"`
	Status      string                `json:"status"`
	CurrentTask *parser.CurrentTask   `json:"current_task,omitempty"`
	TaskHistory []*parser.TaskHistory `json:"task_history,omitempty"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

// GetSystemState returns the complete unified system state snapshot.
// This is used for get_system_state() MCP tool and --full JSON output.
func (a *Aggregator) GetSystemState() *sysinfo.SystemState {
	// Get uptime
	uptime := uint64(0)
	if u, err := sysinfo.GetUptime(); err == nil {
		uptime = u
	}

	now := time.Now()

	// Evaluate alerts against current metrics
	metrics := &systemMetricsWrapper{
		cpu:     a.cpuCollector.Get(),
		mem:     a.memoryCollector.Get(),
		disk:    a.diskCollector.Get(),
		gpu:     a.gpuCollector.Get(),
		network: a.networkCollector.Get(),
	}
	a.alertEvaluator.Evaluate(metrics)

	return &sysinfo.SystemState{
		SchemaVersion: 1,
		Timestamp:     now,
		Host: sysinfo.HostState{
			CPU:        *a.cpuCollector.Get(),
			Memory:     *a.memoryCollector.Get(),
			Disk:       *a.diskCollector.Get(),
			GPU:        *a.gpuCollector.Get(),
			Network:    *a.networkCollector.Get(),
			Alerts:     a.alertEvaluator.GetActiveAlerts(),
			Uptime:     sysinfo.UptimeInfo{Seconds: uptime, UpdatedAt: now},
			UpdatedAt:  now,
		},
		Processes: *a.processCollector.Get(),
	}
}

// buildTaskHistory constructs a task history from completed tool calls in an AgentRun.
// It returns the last 10 completed tasks in chronological order (oldest first).
func buildTaskHistory(run *parser.AgentRun) []*parser.TaskHistory {
	if run == nil || len(run.ToolCalls) == 0 {
		return make([]*parser.TaskHistory, 0)
	}

	history := make([]*parser.TaskHistory, 0)

	// Iterate through tool calls and collect completed ones
	for _, toolCall := range run.ToolCalls {
		// Only include completed tool calls (those with an EndTime)
		if toolCall.EndTime.IsZero() {
			continue
		}

		// Calculate duration
		duration := int64(0)
		if !toolCall.StartTime.IsZero() && !toolCall.EndTime.IsZero() {
			duration = int64(toolCall.EndTime.Sub(toolCall.StartTime).Seconds())
		}

		// Extract argument summary
		argsSummary := extractArgsSummary(toolCall.Name, toolCall.Input)

		// Create task history entry
		taskEntry := &parser.TaskHistory{
			ToolName:    toolCall.Name,
			ArgsSummary: argsSummary,
			StartedAt:   toolCall.StartTime,
			EndedAt:     toolCall.EndTime,
			DurationSec: duration,
			IsError:     toolCall.IsError,
			Result:      toolCall.Result,
		}

		history = append(history, taskEntry)
	}

	// Keep only the last 10 entries
	if len(history) > 10 {
		history = history[len(history)-10:]
	}

	return history
}

// computeCurrentTask extracts the current active task from an AgentRun.
// It returns a CurrentTask describing what the agent is currently executing.
func computeCurrentTask(run *parser.AgentRun, now time.Time) *parser.CurrentTask {
	if run == nil || len(run.ToolCalls) == 0 {
		return nil
	}

	// Get the last tool call
	lastTool := run.ToolCalls[len(run.ToolCalls)-1]

	// Only consider a tool call as "current" if it's still in progress (no EndTime yet)
	// Once it has an EndTime, it's completed and we shouldn't show it as the current task
	if !lastTool.EndTime.IsZero() {
		return nil
	}

	// Compute elapsed time since tool was invoked
	elapsedSec := int64(0)
	if !lastTool.StartTime.IsZero() {
		elapsedSec = int64(now.Sub(lastTool.StartTime).Seconds())
	}

	// Extract a summary of the tool arguments
	argsSummary := extractArgsSummary(lastTool.Name, lastTool.Input)

	// Detect if stalled (tool has been running for more than 30 seconds with no completion)
	// Note: stalled detection also requires the session to still be in "running" state
	isStalled := elapsedSec > 30 && run.Status == "running"

	return &parser.CurrentTask{
		ToolName:    lastTool.Name,
		ArgsSummary: argsSummary,
		StartedAt:   lastTool.StartTime,
		ElapsedSec:  elapsedSec,
		IsStalled:   isStalled,
		LastEventAt: now,
	}
}

// extractArgsSummary creates a human-readable summary of tool arguments.
// For example: "README.md" for file reads, "npm test" for bash commands.
func extractArgsSummary(toolName string, input map[string]interface{}) string {
	if input == nil || len(input) == 0 {
		return ""
	}

	switch toolName {
	case "Read":
		// Extract file path
		if path, ok := input["file_path"].(string); ok {
			return truncateArg(path, 30)
		}
	case "Write":
		// Extract file path
		if path, ok := input["file_path"].(string); ok {
			return truncateArg(path, 30)
		}
	case "Edit":
		// Extract file path
		if path, ok := input["file_path"].(string); ok {
			return truncateArg(path, 30)
		}
	case "Bash":
		// Extract command
		if cmd, ok := input["command"].(string); ok {
			// Take first word or short summary
			parts := strings.Fields(cmd)
			if len(parts) > 0 {
				summary := parts[0]
				if len(parts) > 1 {
					summary += " " + parts[1]
				}
				return truncateArg(summary, 30)
			}
		}
	case "Glob":
		// Extract pattern
		if pattern, ok := input["pattern"].(string); ok {
			return "glob: " + truncateArg(pattern, 25)
		}
	case "Grep":
		// Extract pattern
		if pattern, ok := input["pattern"].(string); ok {
			return "grep: " + truncateArg(pattern, 25)
		}
	case "WebFetch", "WebSearch":
		// Extract URL
		if url, ok := input["url"].(string); ok {
			return truncateArg(url, 30)
		}
	}

	// Default: use first string argument we can find
	for _, v := range input {
		if s, ok := v.(string); ok && len(s) > 0 {
			return truncateArg(s, 30)
		}
	}

	return ""
}

// truncateArg truncates an argument to a maximum length.
func truncateArg(arg string, maxLen int) string {
	if len(arg) <= maxLen {
		return arg
	}
	return arg[:maxLen-3] + "..."
}

// evaluateAlerts evaluates health alerts based on current system metrics.
func (a *Aggregator) evaluateAlerts(cpu *sysinfo.CPUMetrics, mem *sysinfo.MemoryMetrics) {
	// Create a simple metrics wrapper to satisfy the health.SystemMetrics interface
	metrics := &systemMetricsWrapper{
		cpu:     cpu,
		mem:     mem,
		disk:    a.diskCollector.Get(),
		gpu:     a.gpuCollector.Get(),
		network: a.networkCollector.Get(),
	}
	a.alertEvaluator.Evaluate(metrics)
}

// GetActiveAlerts returns the current active health alerts.
func (a *Aggregator) GetActiveAlerts() []*health.Alert {
	return a.alertEvaluator.GetActiveAlerts()
}

// systemMetricsWrapper wraps system metrics to satisfy health.SystemMetrics interface
type systemMetricsWrapper struct {
	cpu     *sysinfo.CPUMetrics
	mem     *sysinfo.MemoryMetrics
	disk    *sysinfo.DiskMetrics
	gpu     *sysinfo.GPUMetrics
	network *sysinfo.NetworkMetrics
}

func (w *systemMetricsWrapper) GetCPULoad1Min() float64 {
	if w.cpu == nil {
		return 0
	}
	return w.cpu.Load1Min
}

func (w *systemMetricsWrapper) GetCPULogicalCores() int {
	if w.cpu == nil {
		return 0
	}
	return w.cpu.LogicalCores
}

func (w *systemMetricsWrapper) GetMemAvailableMB() uint64 {
	if w.mem == nil {
		return 0
	}
	return w.mem.AvailableMB
}

func (w *systemMetricsWrapper) GetMemTotalMB() uint64 {
	if w.mem == nil {
		return 0
	}
	return w.mem.TotalMB
}

func (w *systemMetricsWrapper) GetMemUsedPercent() float64 {
	if w.mem == nil {
		return 0
	}
	return w.mem.UsedPercent
}

func (w *systemMetricsWrapper) GetMemSwapUsedPercent() float64 {
	if w.mem == nil {
		return 0
	}
	return w.mem.SwapUsedPercent
}

func (w *systemMetricsWrapper) GetMemSwapTotalMB() uint64 {
	if w.mem == nil {
		return 0
	}
	return w.mem.SwapTotalMB
}

func (w *systemMetricsWrapper) GetDiskUsedPercent() float64 {
	if w.disk == nil {
		return 0
	}
	return w.disk.UsedPercent
}

func (w *systemMetricsWrapper) GetDiskFreeGB() uint64 {
	if w.disk == nil {
		return 0
	}
	return w.disk.FreeGB
}

func (w *systemMetricsWrapper) GetGPUTempC() float64 {
	if w.gpu == nil {
		return 0
	}
	return w.gpu.TempC
}

func (w *systemMetricsWrapper) GetGPUAvailable() bool {
	if w.gpu == nil {
		return false
	}
	return w.gpu.Available
}

func (w *systemMetricsWrapper) GetNetworkInternetUp() bool {
	if w.network == nil {
		return true // Default to true if unavailable
	}
	return w.network.InternetUp
}
