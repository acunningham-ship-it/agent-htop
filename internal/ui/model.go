package ui

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/acunningham-ship-it/agent-htop/internal/aggregator"
	"github.com/acunningham-ship-it/agent-htop/internal/api"
	"github.com/acunningham-ship-it/agent-htop/internal/parser"
	"github.com/acunningham-ship-it/agent-htop/internal/queue"
	"github.com/acunningham-ship-it/agent-htop/internal/sysinfo"
)

// ViewMode represents the current view state
type ViewMode string

const (
	ViewModeLive       ViewMode = "live"
	ViewModeHistorical ViewMode = "historical"
	ViewModeTools      ViewMode = "tools"
	ViewModeQueue      ViewMode = "queue"
	ViewModeProcesses  ViewMode = "processes"
)

// FilterMode represents the current filter state
type FilterMode string

const (
	FilterAll     FilterMode = "all"
	FilterErrored FilterMode = "errored"
	FilterRunning FilterMode = "running"
	FilterIdle    FilterMode = "idle"
	FilterPaused  FilterMode = "paused"
)

// SortMode represents the current sort state
type SortMode string

const (
	SortName        SortMode = "name"
	SortCostDesc    SortMode = "cost_desc"
	SortHeartbeat   SortMode = "heartbeat"
	SortSpendRate   SortMode = "spend_rate"
)

// Model is the Bubble Tea model for the agent-htop TUI.
type Model struct {
	fleet        *aggregator.FleetState
	table        table.Model
	quitting     bool
	selectedRow  int
	aggregator   *aggregator.Aggregator
	apiClient    *api.Client
	confirmKill  bool
	confirmAgent string
	killError    string
	killErrorTime time.Time
	actionFlash  map[string]time.Time // agentID -> time of action for 1s flash
	windowHeight int
	windowWidth  int

	// Configuration
	companyID  string // Empty if no company specified (Claude Code-first mode)
	runtimes   []string // Active runtimes (claude, paperclip, codex)

	// Search and filter state
	searchQuery  string
	searchMode   bool
	filterMode   FilterMode
	sortMode     SortMode

	// View mode (live vs historical)
	viewMode   ViewMode
	historical *aggregator.HistoricalView

	// Help overlay
	showHelp bool

	// Detail drawer
	showDetail    bool
	detailAgentID string

	// Tool heatmap
	toolHeatmap       *parser.ToolHeatmap
	toolFilterAgentID string // Empty for fleet-wide, or agent ID for filtering

	// Runtime detection results
	runtimeStatus map[string]bool // runtime name -> detected (true/false)

	// Alert state
	lastAlertTime    time.Time
	alertFlashCycle  int // 0-2 for flashing effect (0 = show, 1 = dim, 2 = show, repeat)

	// Process view state
	processSortMode    string // Sort mode for processes (cpu, mem, age, name, user, pid)
	processPageSize    int     // Items per page for process view
	processPage        int     // Current page number for process view
	processFilterType  string // Filter type: "", "user", "cmd"
	processFilterValue string // Filter value
	processConfirmKill bool   // Confirm kill for selected process
	processConfirmPID  int32  // PID to kill after confirmation
	processSelectedRow int    // Selected row in process view

	// Queue view state
	queueManager *queue.Manager
}

// New creates a new TUI model.
func New(agg *aggregator.Aggregator, client *api.Client, companyID string, queueManager *queue.Manager) *Model {
	// Get active runtimes from aggregator
	activeRuntimes := agg.GetActiveRuntimes()
	runtimeStrs := make([]string, len(activeRuntimes))
	for i, rt := range activeRuntimes {
		runtimeStrs[i] = string(rt)
	}

	// Build runtime status (what was detected)
	runtimeStatus := make(map[string]bool)
	for _, rt := range activeRuntimes {
		runtimeStatus[string(rt)] = true
	}

	m := &Model{
		aggregator:    agg,
		apiClient:     client,
		fleet:         &aggregator.FleetState{},
		actionFlash:   make(map[string]time.Time),
		filterMode:    FilterAll,
		sortMode:      SortName,
		searchQuery:   "",
		searchMode:    false,
		viewMode:      ViewModeLive,
		historical:    nil,
		showHelp:      false,
		companyID:     companyID,
		runtimes:      runtimeStrs,
		runtimeStatus: runtimeStatus,
		processSortMode:    "cpu",
		processPageSize:    20,
		processPage:        0,
		processFilterType:  "",
		processFilterValue: "",
		processConfirmKill: false,
		processConfirmPID:  0,
		processSelectedRow: 0,
		queueManager:       queueManager,
	}
	state := agg.GetFleetState()
	if state != nil {
		m.fleet = state
	}
	m.initTable()
	return m
}

// initTable initializes the table with columns and styling.
func (m *Model) initTable() {
	columns := []table.Column{
		{Title: "NAME", Width: 20},
		{Title: "STATUS", Width: 10},
		{Title: "MODEL", Width: 15},
		{Title: "TOKENS IN", Width: 12},
		{Title: "TOKENS OUT", Width: 12},
		{Title: "COST", Width: 10},
		{Title: "ELAPSED", Width: 10},
		{Title: "TASK", Width: 35},
	}

	rows := m.buildTableRows()
	m.table = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	m.table.SetStyles(s)
}

// buildTableRows converts FleetState agents to table rows with color coding, filtering, and sorting.
func (m *Model) buildTableRows() []table.Row {
	// Start with filtered agents
	filtered := m.filterAgents(m.fleet.Agents)

	// Sort the filtered agents
	filtered = m.sortAgents(filtered)

	var rows []table.Row
	for _, agent := range filtered {
		status := m.colorStatus(agent.Status, agent.IsError)
		model := truncate(agent.Model, 15)
		elapsed := formatElapsed(agent.ElapsedMS, agent.Status)
		task := formatCurrentTask(agent.CurrentTask)
		cost := fmt.Sprintf("$%.4f", agent.TotalCostUSD)

		rows = append(rows, table.Row{
			truncate(agent.AgentName, 20),
			status,
			model,
			fmt.Sprintf("%d", agent.InputTokens),
			fmt.Sprintf("%d", agent.OutputTokens),
			cost,
			elapsed,
			task,
		})
	}
	return rows
}

// filterAgents applies search query and filter mode to agents
func (m *Model) filterAgents(agents []*aggregator.AgentView) []*aggregator.AgentView {
	var result []*aggregator.AgentView

	for _, agent := range agents {
		// Apply filter mode
		if !m.passesFilter(agent) {
			continue
		}

		// Apply search query
		if m.searchQuery != "" && !m.fuzzyMatch(agent, m.searchQuery) {
			continue
		}

		result = append(result, agent)
	}

	return result
}

// passesFilter checks if an agent passes the current filter mode
func (m *Model) passesFilter(agent *aggregator.AgentView) bool {
	switch m.filterMode {
	case FilterAll:
		return true
	case FilterErrored:
		return agent.Status == "error"
	case FilterRunning:
		return agent.Status == "running"
	case FilterIdle:
		return agent.Status == "idle"
	case FilterPaused:
		// Note: paused status may not exist yet, but included per spec
		return agent.Status == "paused"
	default:
		return true
	}
}

// fuzzyMatch performs a simple fuzzy search on agent name, status, and model
func (m *Model) fuzzyMatch(agent *aggregator.AgentView, query string) bool {
	query = strings.ToLower(query)
	searchIn := strings.ToLower(agent.AgentName + " " + agent.Status + " " + agent.Model)
	return strings.Contains(searchIn, query)
}

// sortAgents applies the current sort mode to agents
func (m *Model) sortAgents(agents []*aggregator.AgentView) []*aggregator.AgentView {
	// Make a copy to avoid modifying the original
	sorted := make([]*aggregator.AgentView, len(agents))
	copy(sorted, agents)

	switch m.sortMode {
	case SortName:
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].AgentName < sorted[j].AgentName
		})
	case SortCostDesc:
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].TotalCostUSD > sorted[j].TotalCostUSD
		})
	case SortHeartbeat:
		sort.SliceStable(sorted, func(i, j int) bool {
			// Later ElapsedMS (more recent) comes first
			return sorted[i].ElapsedMS > sorted[j].ElapsedMS
		})
	case SortSpendRate:
		// Spend rate = cost / elapsed time (cost per second of execution)
		sort.SliceStable(sorted, func(i, j int) bool {
			iRate := calculateSpendRate(sorted[i])
			jRate := calculateSpendRate(sorted[j])
			return iRate > jRate
		})
	}

	return sorted
}

// calculateSpendRate calculates cost per second of execution
func calculateSpendRate(agent *aggregator.AgentView) float64 {
	if agent.ElapsedMS == 0 {
		return 0
	}
	seconds := float64(agent.ElapsedMS) / 1000.0
	return agent.TotalCostUSD / seconds
}

// colorStatus returns a colored status string.
func (m *Model) colorStatus(status string, isError bool) string {
	switch status {
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("22")).Render(status)
	case "idle":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(status)
	case "error", "hung":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("160")).Render(status)
	default:
		return status
	}
}

// formatElapsed formats elapsed time as "Xm Ys" or "-" for idle agents.
func formatElapsed(ms int64, status string) string {
	if status != "running" || ms == 0 {
		return "-"
	}
	seconds := ms / 1000
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%dm %ds", minutes, secs)
}

// formatSinceUpdate formats the time since last update as "Xs" or "Xm Ys".
func formatSinceUpdate(updatedAt time.Time) string {
	if updatedAt.IsZero() {
		return "-"
	}
	elapsed := time.Since(updatedAt)
	seconds := int64(elapsed.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%dm %ds", minutes, secs)
}

// formatCurrentTask formats the current task for display in the TUI table.
// Returns strings like "Reading README.md (4s)" or "STALLED: Bash (30s+)".
func formatCurrentTask(task *parser.CurrentTask) string {
	if task == nil {
		return "-"
	}

	if task.IsStalled {
		return fmt.Sprintf("STALLED: %s (%ds+)", task.ToolName, task.ElapsedSec)
	}

	// Format: "ToolName: args (Xs)"
	var display string
	if task.ArgsSummary != "" {
		display = fmt.Sprintf("%s: %s (%ds)", task.ToolName, task.ArgsSummary, task.ElapsedSec)
	} else {
		display = fmt.Sprintf("%s (%ds)", task.ToolName, task.ElapsedSec)
	}

	return truncate(display, 35)
}

// truncate truncates a string to max length.
func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-1] + "…"
	}
	return s
}

// Init initializes the model and returns initial commands.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		subscribeToStateUpdates(m.aggregator),
	)
}

// Update processes incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case StateUpdateMsg:
		return m.handleStateUpdate(msg)
	case ActionResultMsg:
		if msg.Error != "" {
			m.killError = msg.Error
			m.killErrorTime = time.Now()
		} else if msg.AgentID != "" {
			m.actionFlash[msg.AgentID] = time.Now()
		}
		return m, nil
	case ProcessKillMsg:
		if msg.Error != "" {
			m.killError = msg.Error
			m.killErrorTime = time.Now()
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.windowHeight = msg.Height
		m.windowWidth = msg.Width
		m.table.SetHeight(msg.Height - 4) // Account for header and footer
		return m, nil
	}
	return m, nil
}

// handleKeyMsg processes keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle process view input first
	if m.viewMode == ViewModeProcesses {
		switch msg.String() {
		case "p":
			// Back to live view
			m.viewMode = ViewModeLive
			m.processFilterType = ""
			m.processFilterValue = ""
			m.processSelectedRow = 0
			return m, nil
		case "s":
			// Cycle sort mode in process view
			m.processSortMode = m.nextProcessSortMode()
			return m, nil
		case "f":
			// Cycle filter in process view (user, cmd, all)
			switch m.processFilterType {
			case "":
				m.processFilterType = "user"
			case "user":
				m.processFilterType = "cmd"
			case "cmd":
				m.processFilterType = ""
			}
			m.processFilterValue = ""
			m.processSelectedRow = 0
			return m, nil
		case "up", "k":
			if m.processSelectedRow > 0 {
				m.processSelectedRow--
			}
			return m, nil
		case "down", "j":
			if m.fleet != nil && len(m.fleet.Processes) > 0 {
				// Count visible processes
				procs := m.getVisibleProcesses()
				if m.processSelectedRow < len(procs)-1 {
					m.processSelectedRow++
				}
			}
			return m, nil
		case "f9":
			// Kill process with confirmation
			if m.fleet != nil && len(m.fleet.Processes) > 0 {
				procs := m.getVisibleProcesses()
				if m.processSelectedRow < len(procs) {
					m.processConfirmKill = true
					m.processConfirmPID = procs[m.processSelectedRow].PID
				}
			}
			return m, nil
		case "shift+f9":
			// Kill process with SIGKILL (shift+f9)
			if m.fleet != nil && len(m.fleet.Processes) > 0 {
				procs := m.getVisibleProcesses()
				if m.processSelectedRow < len(procs) {
					pid := procs[m.processSelectedRow].PID
					return m, killProcessCmd(pid, "KILL")
				}
			}
			return m, nil
		case "y":
			// Confirm kill with SIGTERM
			if m.processConfirmKill && m.processConfirmPID > 0 {
				m.processConfirmKill = false
				pid := m.processConfirmPID
				m.processConfirmPID = 0
				return m, killProcessCmd(pid, "TERM")
			}
			return m, nil
		case "n":
			// Cancel kill
			m.processConfirmKill = false
			m.processConfirmPID = 0
			return m, nil
		case "esc":
			// Exit process view
			m.viewMode = ViewModeLive
			m.processFilterType = ""
			m.processFilterValue = ""
			m.processSelectedRow = 0
			m.processConfirmKill = false
			return m, nil
		}
		return m, nil
	}

	// Handle search mode input
	if m.searchMode {
		switch msg.String() {
		case "esc", "ctrl+c":
			m.searchMode = false
			m.selectedRow = 0
			m.initTable()
			return m, nil
		case "enter":
			m.searchMode = false
			m.selectedRow = 0
			m.initTable()
			return m, nil
		case "backspace", "ctrl+h":
			if len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.selectedRow = 0
				m.initTable()
			}
			return m, nil
		default:
			if len(msg.String()) == 1 {
				m.searchQuery += msg.String()
				m.selectedRow = 0
				m.initTable()
			}
			return m, nil
		}
	}

	// Handle normal mode input
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "?":
		// Toggle help overlay
		m.showHelp = !m.showHelp
		return m, nil
	case "enter":
		// Toggle detail drawer for selected agent
		if m.showDetail {
			m.showDetail = false
			m.detailAgentID = ""
		} else {
			filtered := m.filterAgents(m.fleet.Agents)
			if len(filtered) > 0 && m.selectedRow < len(filtered) {
				m.showDetail = true
				m.detailAgentID = filtered[m.selectedRow].AgentID
			}
		}
		return m, nil
	case "esc":
		// Close detail drawer if open
		if m.showDetail {
			m.showDetail = false
			m.detailAgentID = ""
			return m, nil
		}
	case "H":
		// Shift+H to toggle historical view
		if m.viewMode == ViewModeLive {
			m.viewMode = ViewModeHistorical
			// Load historical data if not already loaded
			if m.historical == nil {
				m.historical = m.aggregator.GetHistoricalView()
			}
		} else if m.viewMode == ViewModeHistorical {
			m.viewMode = ViewModeLive
		}
		return m, nil
	case "T":
		// Shift+T to toggle tool heatmap view
		if m.viewMode == ViewModeLive {
			m.viewMode = ViewModeTools
			m.loadToolHeatmap()
		} else if m.viewMode == ViewModeTools {
			m.viewMode = ViewModeLive
		}
		return m, nil
	case "Q":
		// Shift+Q to toggle queue view
		if m.viewMode == ViewModeLive {
			m.viewMode = ViewModeQueue
		} else if m.viewMode == ViewModeQueue {
			m.viewMode = ViewModeLive
		}
		return m, nil
	case "p":
		// Shift+P would be 'P' which is already used for pause, so use lowercase 'p' for processes
		// Actually, wait - the spec says 'P' (uppercase) toggles processes
		// But Shift+P is used for pause. Let me use lowercase 'p' for processes
		if m.viewMode == ViewModeLive {
			m.viewMode = ViewModeProcesses
			m.processSelectedRow = 0
		} else if m.viewMode == ViewModeProcesses {
			m.viewMode = ViewModeLive
			m.processSelectedRow = 0
		}
		return m, nil
	case "e":
		// Export tool heatmap to CSV if in tools view
		if m.viewMode == ViewModeTools && m.toolHeatmap != nil {
			m.exportToolHeatmapCSV()
		}
		return m, nil
	case "/":
		// Enter search mode
		m.searchMode = true
		m.searchQuery = ""
		m.selectedRow = 0
		m.initTable()
		return m, nil
	case "f":
		// Cycle filter mode
		m.filterMode = m.nextFilterMode()
		m.selectedRow = 0
		m.initTable()
		return m, nil
	case "s":
		// Cycle sort mode
		m.sortMode = m.nextSortMode()
		m.initTable()
		return m, nil
	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
			m.table.SetCursor(m.selectedRow)
		}
	case "down", "j":
		filtered := m.filterAgents(m.fleet.Agents)
		if m.selectedRow < len(filtered)-1 {
			m.selectedRow++
			m.table.SetCursor(m.selectedRow)
		}
	case "K":
		// Shift+K to kill selected agent - show confirmation
		// Check if the runtime supports killing
		filtered := m.filterAgents(m.fleet.Agents)
		if len(filtered) > 0 && m.selectedRow < len(filtered) {
			selected := filtered[m.selectedRow]
			caps := aggregator.GetCapabilities(selected.Runtime)
			if caps.CanKill {
				m.confirmKill = true
				m.confirmAgent = selected.AgentID
			}
		}
	case "y":
		// Confirm kill
		if m.confirmKill && m.confirmAgent != "" {
			m.confirmKill = false
			agentID := m.confirmAgent
			m.confirmAgent = ""
			return m, terminateAgent(m.apiClient, agentID)
		}
	case "n":
		// Cancel confirmation
		m.confirmKill = false
		m.confirmAgent = ""
	case "P":
		// Shift+P to pause selected agent
		filtered := m.filterAgents(m.fleet.Agents)
		if len(filtered) > 0 && m.selectedRow < len(filtered) {
			selected := filtered[m.selectedRow]
			caps := aggregator.GetCapabilities(selected.Runtime)
			if caps.CanPause {
				return m, pauseAgent(m.apiClient, selected.AgentID)
			}
		}
	case "R":
		// Shift+R to resume selected agent
		filtered := m.filterAgents(m.fleet.Agents)
		if len(filtered) > 0 && m.selectedRow < len(filtered) {
			selected := filtered[m.selectedRow]
			caps := aggregator.GetCapabilities(selected.Runtime)
			if caps.CanPause { // Resume requires pause capability
				return m, resumeAgent(m.apiClient, selected.AgentID)
			}
		}
	}
	return m, nil
}

// nextFilterMode cycles to the next filter mode
func (m *Model) nextFilterMode() FilterMode {
	switch m.filterMode {
	case FilterAll:
		return FilterErrored
	case FilterErrored:
		return FilterRunning
	case FilterRunning:
		return FilterIdle
	case FilterIdle:
		return FilterPaused
	case FilterPaused:
		return FilterAll
	default:
		return FilterAll
	}
}

// nextSortMode cycles to the next sort mode
func (m *Model) nextSortMode() SortMode {
	switch m.sortMode {
	case SortName:
		return SortCostDesc
	case SortCostDesc:
		return SortHeartbeat
	case SortHeartbeat:
		return SortSpendRate
	case SortSpendRate:
		return SortName
	default:
		return SortName
	}
}

// nextProcessSortMode cycles to the next process sort mode
func (m *Model) nextProcessSortMode() string {
	switch m.processSortMode {
	case "cpu":
		return "mem"
	case "mem":
		return "age"
	case "age":
		return "name"
	case "name":
		return "user"
	case "user":
		return "pid"
	case "pid":
		return "cpu"
	default:
		return "cpu"
	}
}

// getVisibleProcesses returns the filtered and sorted process list
func (m *Model) getVisibleProcesses() []*sysinfo.ProcessInfo {
	if m.fleet == nil || len(m.fleet.Processes) == 0 {
		return []*sysinfo.ProcessInfo{}
	}

	// Copy processes
	procs := make([]*sysinfo.ProcessInfo, len(m.fleet.Processes))
	copy(procs, m.fleet.Processes)

	pl := &sysinfo.ProcessList{Processes: procs}

	// Apply filter if set
	if m.processFilterType != "" {
		procs = pl.FilterBy(m.processFilterType, m.processFilterValue)
		pl.Processes = procs
	}

	// Sort
	pl.SortBy(m.processSortMode)

	return pl.Processes
}

// handleStateUpdate processes a state update from the aggregator.
func (m *Model) handleStateUpdate(msg StateUpdateMsg) (tea.Model, tea.Cmd) {
	m.fleet = msg.State
	m.initTable()
	return m, subscribeToStateUpdates(m.aggregator)
}

// View renders the TUI.
func (m *Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	// Show help overlay if requested
	if m.showHelp {
		return m.renderHelpOverlay()
	}

	// Show detail drawer if requested
	if m.showDetail {
		return m.renderDetailDrawer()
	}

	// Show empty state if no agents detected
	if len(m.fleet.Agents) == 0 {
		return m.renderEmptyState()
	}

	header := m.renderHeader()

	var tableView string
	if m.viewMode == ViewModeQueue {
		tableView = m.renderQueueView()
	} else if m.viewMode == ViewModeTools {
		tableView = m.renderToolHeatmap()
	} else if m.viewMode == ViewModeHistorical {
		tableView = m.renderHistoricalView()
	} else if m.viewMode == ViewModeProcesses {
		tableView = m.renderProcessesView()
	} else if m.searchMode {
		tableView = m.renderSearchMode()
	} else {
		tableView = m.table.View()
	}

	var confirmation string
	if m.confirmKill {
		confirmation = fmt.Sprintf("\n⚠️  Kill agent %s? (y/n)", truncate(m.confirmAgent, 20))
	}

	var errorMsg string
	if m.killError != "" && time.Since(m.killErrorTime) < 3*time.Second {
		errorMsg = fmt.Sprintf("\n❌ %s", m.killError)
	} else if m.killError != "" {
		m.killError = ""
	}

	// Show success flash for recent actions
	var successMsg string
	for agentID, flashTime := range m.actionFlash {
		if time.Since(flashTime) < 1*time.Second {
			agentName := ""
			for _, agent := range m.fleet.Agents {
				if agent.AgentID == agentID {
					agentName = truncate(agent.AgentName, 20)
					break
				}
			}
			successMsg = fmt.Sprintf("\n✓ Action successful for %s", agentName)
			break
		}
	}
	// Clean up old flashes
	for agentID, flashTime := range m.actionFlash {
		if time.Since(flashTime) >= 1*time.Second {
			delete(m.actionFlash, agentID)
		}
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		tableView,
		confirmation,
		successMsg,
		errorMsg,
	)
}

// renderProcessesView renders the process list view
func (m *Model) renderProcessesView() string {
	if m.fleet == nil || len(m.fleet.Processes) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No processes available\n")
	}

	// Build header
	header := fmt.Sprintf("System Processes (%d total) - Sort: %s\n", len(m.fleet.Processes), m.processSortMode)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styledHeader := headerStyle.Render(header)

	// Sort processes by current sort mode
	procs := make([]*sysinfo.ProcessInfo, len(m.fleet.Processes))
	copy(procs, m.fleet.Processes)

	sorted := &sysinfo.ProcessList{Processes: procs}
	sorted.SortBy(m.processSortMode)

	// Pagination
	totalPages := (len(sorted.Processes) + m.processPageSize - 1) / m.processPageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if m.processPage >= totalPages {
		m.processPage = totalPages - 1
	}

	startIdx := m.processPage * m.processPageSize
	endIdx := startIdx + m.processPageSize
	if endIdx > len(sorted.Processes) {
		endIdx = len(sorted.Processes)
	}

	// Build table
	columns := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "NAME", Width: 20},
		{Title: "CPU%", Width: 8},
		{Title: "MEM%", Width: 8},
		{Title: "MEM_MB", Width: 10},
		{Title: "USER", Width: 12},
		{Title: "COMMAND", Width: 35},
	}

	var rows []table.Row
	for _, proc := range sorted.Processes[startIdx:endIdx] {
		cmdLine := truncate(proc.CmdLine, 35)
		if cmdLine == "" {
			cmdLine = proc.Name
		}
		row := table.Row{
			fmt.Sprintf("%d", proc.PID),
			truncate(proc.Name, 20),
			fmt.Sprintf("%.1f%%", proc.CPUPercent),
			fmt.Sprintf("%.1f%%", proc.MemPercent),
			fmt.Sprintf("%d MB", proc.MemMB),
			truncate(proc.User, 12),
			cmdLine,
		}
		rows = append(rows, row)
	}

	procTable := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Bold(false)
	procTable.SetStyles(s)

	// Pagination info
	paginationInfo := fmt.Sprintf("\n  Page %d of %d  |  Showing %d-%d of %d processes",
		m.processPage+1, totalPages, startIdx+1, endIdx, len(sorted.Processes))
	paginationStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styledPagination := paginationStyle.Render(paginationInfo)

	// Help text
	helpText := "\n  [p]back to live view  [s]cycle sort  [↑/↓]page"
	styledHelp := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(helpText)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		styledHeader,
		procTable.View(),
		styledPagination,
		styledHelp,
	)
}

// renderSearchMode renders the search input interface
func (m *Model) renderSearchMode() string {
	searchBox := fmt.Sprintf("  search: %s_", m.searchQuery)
	styled := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Render(searchBox)

	filtered := m.filterAgents(m.fleet.Agents)
	filtered = m.sortAgents(filtered)

	var rows []table.Row
	for _, agent := range filtered {
		status := m.colorStatus(agent.Status, agent.IsError)
		model := truncate(agent.Model, 15)
		elapsed := formatElapsed(agent.ElapsedMS, agent.Status)
		lastTool := truncate(agent.LastTool, 20)
		cost := fmt.Sprintf("$%.4f", agent.TotalCostUSD)

		rows = append(rows, table.Row{
			truncate(agent.AgentName, 20),
			status,
			model,
			fmt.Sprintf("%d", agent.InputTokens),
			fmt.Sprintf("%d", agent.OutputTokens),
			cost,
			elapsed,
			lastTool,
		})
	}

	// Create a temporary table for search results
	columns := []table.Column{
		{Title: "NAME", Width: 20},
		{Title: "STATUS", Width: 10},
		{Title: "MODEL", Width: 15},
		{Title: "TOKENS IN", Width: 12},
		{Title: "TOKENS OUT", Width: 12},
		{Title: "COST", Width: 10},
		{Title: "ELAPSED", Width: 10},
		{Title: "LAST TOOL", Width: 20},
	}

	searchTable := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Bold(false)
	searchTable.SetStyles(s)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		styled,
		searchTable.View(),
	)
}

// renderHistoricalView renders the 7-day historical view with bar chart.
func (m *Model) renderHistoricalView() string {
	if m.historical == nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("Loading historical data...\n")
	}

	// Refresh historical data on every render (keep it current)
	m.historical = m.aggregator.GetHistoricalView()

	// Build table header
	header := "Last 7 Days Historical View\n"
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styledHeader := headerStyle.Render(header)

	// Find max cost for scaling
	maxCost := m.historical.MaxCostForChart()
	if maxCost == 0 {
		maxCost = 1 // Avoid division by zero
	}

	// Render each day as a row with bar chart
	var lines []string
	for i, day := range m.historical.Days {
		daysAgo := len(m.historical.Days) - i - 1
		dateStr := day.Date.Format("Mon 01/02")
		if daysAgo == 0 {
			dateStr = "Today"
		} else if daysAgo == 1 {
			dateStr = "Yesterday"
		}

		// Build bar (using block characters)
		barWidth := 30
		filledWidth := int((day.TotalCostUSD / maxCost) * float64(barWidth))
		if filledWidth > barWidth {
			filledWidth = barWidth
		}
		if filledWidth < 1 && day.TotalCostUSD > 0 {
			filledWidth = 1
		}

		bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)

		// Determine color based on cost
		var barColor lipgloss.Color
		avgCost := maxCost / 7
		if day.TotalCostUSD <= avgCost {
			barColor = lipgloss.Color("2") // green
		} else if day.TotalCostUSD <= avgCost*2 {
			barColor = lipgloss.Color("3") // yellow
		} else {
			barColor = lipgloss.Color("1") // red
		}

		styledBar := lipgloss.NewStyle().Foreground(barColor).Render(bar)

		// Summary stats
		line := fmt.Sprintf(
			"  %s  %s  $%7.2f  %3d sessions  %s  errors:%d",
			dateStr,
			styledBar,
			day.TotalCostUSD,
			day.SessionCount,
			day.TopModel,
			day.ErrorCount,
		)
		lines = append(lines, line)
	}

	// Add summary footer
	totalCost := 0.0
	totalSessions := 0
	totalErrors := 0
	for _, day := range m.historical.Days {
		totalCost += day.TotalCostUSD
		totalSessions += day.SessionCount
		totalErrors += day.ErrorCount
	}
	avgDailyCost := totalCost / 7

	summaryLine := fmt.Sprintf(
		"\n  7-day Total: $%.2f  Avg/day: $%.2f  Sessions: %d  Errors: %d",
		totalCost, avgDailyCost, totalSessions, totalErrors,
	)
	summaryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styledSummary := summaryStyle.Render(summaryLine)

	// Add help text
	helpText := "\n  [H]back to live view"
	styledHelp := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(helpText)

	allLines := append(lines, styledSummary, styledHelp)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		styledHeader,
		strings.Join(allLines, "\n"),
	)
}

// renderHeader renders the header with fleet summary.
func (m *Model) renderHeader() string {
	running := 0
	for _, agent := range m.fleet.Agents {
		if agent.Status == "running" {
			running++
		}
	}

	totalCost := 0.0
	for _, agent := range m.fleet.Agents {
		totalCost += agent.TotalCostUSD
	}

	// Build runtime badges
	runtimeStr := strings.Join(m.runtimes, " ")

	// Build filter and sort indicators
	filterStr := fmt.Sprintf("[f]%s", m.filterMode)
	sortStr := fmt.Sprintf("[s]%s", m.sortMode)

	// Add search indicator if active
	var searchStr string
	if m.searchMode {
		searchStr = " [/]search active"
	} else if m.searchQuery != "" {
		searchStr = fmt.Sprintf(" [/]search: '%s'", m.searchQuery)
	}

	// Main header line: agent-htop v0.2.0 | <runtime badges> | N sessions | updated Xs ago
	// Note: [K]ill, [P]ause, [R]esume are only available for Paperclip agents
	header := fmt.Sprintf(
		"agent-htop v0.2.0 | %s | %d sessions | updated %s ago   %s  %s%s  [q]uit [K]ill [P]ause [R]esume [H]istory [T]ools [/]search\n",
		runtimeStr,
		len(m.fleet.Agents),
		formatSinceUpdate(m.fleet.UpdatedAt),
		filterStr,
		sortStr,
		searchStr,
	)

	// Style the header
	mainHeader := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Render(header)

	// Optional second line: show company ID if --company was set
	var companyLine string
	if m.companyID != "" {
		companyLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Render(fmt.Sprintf("  Paperclip: %s\n", m.companyID))
	}

	// Alert banner (shown when health alerts are active)
	alertBanner := m.renderAlertBanner()

	// Cost projection line (shown when any agent has projection data)
	projectionLine := m.renderProjectionLine()

	// System metrics line (CPU, RAM, load)
	systemLine := m.renderSystemMetrics()

	return mainHeader + companyLine + alertBanner + systemLine + projectionLine
}

// renderProjectionLine builds the fleet-wide cost projection footer line.
// Format: "  today: $0.42 → projected $2.80 by midnight (at 3.0× daily avg)"
// Color: green ≤1×, yellow 1-2×, red >2× daily average.
func (m *Model) renderProjectionLine() string {
	var spentToday, projectedToday, dailyAvgTotal float64
	hasProjection := false

	for _, agent := range m.fleet.Agents {
		if agent.Projection == nil {
			continue
		}
		hasProjection = true
		spentToday += agent.Projection.SpentToday
		projectedToday += agent.Projection.ProjectedToday
		dailyAvgTotal += agent.Projection.DailyAverage
	}

	if !hasProjection {
		// No projection data yet — show bare today cost
		line := fmt.Sprintf("  today: $%.4f\n", 0.0)
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(line)
	}

	// Determine color based on projected vs daily average
	var colorCode lipgloss.Color
	var multiplier float64
	if dailyAvgTotal > 0 {
		multiplier = projectedToday / dailyAvgTotal
	}
	switch {
	case dailyAvgTotal == 0 || multiplier <= 1.0:
		colorCode = lipgloss.Color("2") // green
	case multiplier <= 2.0:
		colorCode = lipgloss.Color("3") // yellow
	default:
		colorCode = lipgloss.Color("1") // red
	}

	var line string
	if dailyAvgTotal > 0 {
		line = fmt.Sprintf("  today: $%.4f → projected $%.4f by midnight (at %.1f× daily avg)\n",
			spentToday, projectedToday, multiplier)
	} else {
		line = fmt.Sprintf("  today: $%.4f → projected $%.4f by midnight\n",
			spentToday, projectedToday)
	}

	return lipgloss.NewStyle().Foreground(colorCode).Render(line)
}

// renderSystemMetrics builds a line showing host CPU, RAM, load, network, and GPU metrics.
// Format: "  CPU 34.2% | RAM 6.2/16.0 GB | Load 1.2 2.3 3.1 | eth0 ↑4.2MB/s ↓12.1MB/s | GPU0 45% 62°C"
func (m *Model) renderSystemMetrics() string {
	if m.fleet == nil || m.fleet.HostMetrics == nil {
		return ""
	}

	cpu := m.fleet.HostMetrics.CPU
	mem := m.fleet.HostMetrics.Memory
	if cpu == nil || mem == nil {
		return ""
	}

	cpuStr := fmt.Sprintf("CPU %.1f%%", cpu.AveragePercent)
	ramStr := fmt.Sprintf("RAM %.1f/%.0f GB", float64(mem.UsedMB)/1024.0, float64(mem.TotalMB)/1024.0)
	loadStr := fmt.Sprintf("Load %.1f %.1f %.1f", cpu.Load1Min, cpu.Load5Min, cpu.Load15Min)

	// Add network metrics if available
	parts := []string{cpuStr, ramStr, loadStr}
	if m.fleet.HostMetrics.Network != nil {
		netStr := formatNetworkMetrics(m.fleet.HostMetrics.Network)
		if netStr != "" {
			parts = append(parts, netStr)
		}
	}

	// Add disk metrics if available
	if m.fleet.HostMetrics.Disks != nil && len(m.fleet.HostMetrics.Disks.Mounts) > 0 {
		diskStr := formatDiskMetrics(m.fleet.HostMetrics.Disks)
		if diskStr != "" {
			parts = append(parts, diskStr)
		}
	}

	// Add GPU metrics if available
	if m.fleet.HostMetrics.GPU != nil && len(m.fleet.HostMetrics.GPU.GPUs) > 0 {
		gpuStr := formatGPUMetrics(m.fleet.HostMetrics.GPU)
		if gpuStr != "" {
			parts = append(parts, gpuStr)
		}
	}

	line := fmt.Sprintf("  %s\n", strings.Join(parts, " | "))
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(line)
}

// formatNetworkMetrics formats network metrics into a compact display string.
// Example: "eth0 ↑4.2MB/s ↓12.1MB/s | wifi: MyNet -62dBm"
func formatNetworkMetrics(net *sysinfo.NetworkMetrics) string {
	var parts []string

	// Find primary interface (highest throughput)
	var primaryIface *sysinfo.InterfaceMetrics
	var maxThroughput float64
	for i := range net.Interfaces {
		throughput := net.Interfaces[i].ThroughputUp + net.Interfaces[i].ThroughputDn
		if throughput > maxThroughput && net.Interfaces[i].State == "up" {
			maxThroughput = throughput
			primaryIface = &net.Interfaces[i]
		}
	}

	// Format primary interface
	if primaryIface != nil {
		upMB := primaryIface.ThroughputUp / 1024.0 / 1024.0
		dnMB := primaryIface.ThroughputDn / 1024.0 / 1024.0
		ifaceStr := fmt.Sprintf("%s ↑%.1fMB/s ↓%.1fMB/s", primaryIface.Name, upMB, dnMB)
		parts = append(parts, ifaceStr)
	}

	// Add WiFi info if available
	if net.WiFi != nil && net.WiFi.Connected {
		wifiStr := fmt.Sprintf("wifi: %s %ddBm", net.WiFi.SSID, net.WiFi.SignalDBm)
		parts = append(parts, wifiStr)
	}

	return strings.Join(parts, " | ")
}

// formatGPUMetrics formats GPU metrics into a compact display string.
// Example: "GPU0 45% 62°C" or "GPU0 45% 7.2/8.0GB 62°C 65W" for detailed view
func formatGPUMetrics(gpuMetrics *sysinfo.GPUMetrics) string {
	if gpuMetrics == nil || len(gpuMetrics.GPUs) == 0 {
		return ""
	}

	var parts []string
	for _, gpu := range gpuMetrics.GPUs {
		if gpu == nil {
			continue
		}

		// Build a concise GPU string: "GPU0 45% 62°C"
		// Extended: "GPU0 45% 7.2/8.0GB 62°C 65W"
		gpuStr := fmt.Sprintf("GPU%d %.0f%%", gpu.Index, gpu.UtilPct)

		// Add VRAM info if available
		if gpu.VRAMTotal > 0 {
			vramUsedGB := float64(gpu.VRAMUsed) / 1024.0
			vramTotalGB := float64(gpu.VRAMTotal) / 1024.0
			gpuStr += fmt.Sprintf(" %.1f/%.1fGB", vramUsedGB, vramTotalGB)
		}

		// Add temperature
		if gpu.TempC > 0 {
			gpuStr += fmt.Sprintf(" %.0f°C", gpu.TempC)
		}

		// Add power draw if available
		if gpu.PowerDraw > 0 {
			gpuStr += fmt.Sprintf(" %.0fW", gpu.PowerDraw)
		}

		parts = append(parts, gpuStr)
	}

	return strings.Join(parts, " | ")
}

// formatDiskMetrics formats disk metrics into a compact display string.
// Example: "/ 68% [████░░░░]" or "/ 68% [████░░░░] ⚠" for warnings
func formatDiskMetrics(disks *sysinfo.DiskList) string {
	if disks == nil || len(disks.Mounts) == 0 {
		return ""
	}

	// Find the root filesystem (or highest usage if root not found)
	var primaryMount *sysinfo.DiskMetrics
	var maxUsage float64
	for i := range disks.Mounts {
		if disks.Mounts[i].MountPoint == "/" {
			primaryMount = disks.Mounts[i]
			break
		}
		if disks.Mounts[i].UsedPercent > maxUsage {
			maxUsage = disks.Mounts[i].UsedPercent
			primaryMount = disks.Mounts[i]
		}
	}

	if primaryMount == nil {
		return ""
	}

	// Build usage bar: 10 characters
	usedBlocks := int(primaryMount.UsedPercent / 10.0)
	if usedBlocks > 10 {
		usedBlocks = 10
	}
	bar := strings.Repeat("█", usedBlocks) + strings.Repeat("░", 10-usedBlocks)

	diskStr := fmt.Sprintf("%s %.0f%% [%s]", primaryMount.MountPoint, primaryMount.UsedPercent, bar)

	// Add warning indicator if needed
	if primaryMount.UsedPercent > 95 {
		diskStr += " 🚨"
	} else if primaryMount.UsedPercent > 90 {
		diskStr += " ⚠"
	}

	return diskStr
}

// renderAlertBanner renders active health alerts with flashing effect for critical alerts.
func (m *Model) renderAlertBanner() string {
	alerts := m.aggregator.GetActiveAlerts()
	if len(alerts) == 0 {
		return ""
	}

	// Update flash cycle for animation (flashes every 300ms)
	now := time.Now()
	if m.lastAlertTime.IsZero() {
		m.lastAlertTime = now
	}
	elapsed := now.Sub(m.lastAlertTime)
	m.alertFlashCycle = int((elapsed.Milliseconds() / 300) % 3)

	// Build alert message from highest severity alert
	var criticalAlert, highAlert, mediumAlert *string
	for _, alert := range alerts {
		msg := alert.Message
		switch alert.Severity {
		case "critical":
			if criticalAlert == nil {
				criticalAlert = &msg
			}
		case "high":
			if highAlert == nil {
				highAlert = &msg
			}
		case "medium":
			if mediumAlert == nil {
				mediumAlert = &msg
			}
		}
	}

	var severity, message string
	if criticalAlert != nil {
		severity = "critical"
		message = *criticalAlert
	} else if highAlert != nil {
		severity = "high"
		message = *highAlert
	} else if mediumAlert != nil {
		severity = "medium"
		message = *mediumAlert
	} else {
		return ""
	}

	// Build the alert banner with icon
	icon := "[!]"
	if severity == "critical" {
		// Flash effect for critical alerts
		if m.alertFlashCycle == 0 {
			icon = "[!!!]"
		} else {
			icon = "     "
		}
	}

	alertMsg := fmt.Sprintf("%s %s: %s", icon, strings.ToUpper(severity), message)

	// Color based on severity
	var style lipgloss.Style
	switch severity {
	case "critical":
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("196"))
	case "high":
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("226"))
	default:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("16")).
			Background(lipgloss.Color("226"))
	}

	return style.Render(fmt.Sprintf("  %s\n", alertMsg))
}

// renderHelpOverlay renders the help screen with keybindings and runtime detection status.
func (m *Model) renderHelpOverlay() string {
	// Build runtime detection status line
	runtimeBadge := m.renderRuntimeBadge()

	// Build help content
	helpText := strings.Join([]string{
		"",
		"  KEYBINDINGS",
		"  ============",
		"  q             Quit agent-htop",
		"  ?             Toggle this help screen",
		"  [↑/k] [↓/j]   Navigate agents",
		"  [/]           Search agents",
		"  [f]           Cycle filter (all/errored/running/idle)",
		"  [s]           Cycle sort (name/cost/heartbeat/spend-rate)",
		"  [K]           Kill selected agent (Paperclip only)",
		"  [P]           Pause selected agent (Paperclip only)",
		"  [R]           Resume selected agent (Paperclip only)",
		"  [H]           Toggle historical 7-day view",
		"  [T]           Toggle tool usage heatmap",
		"  [e]           Export tool heatmap to CSV",
		"",
		"  RUNTIME DETECTION",
		"  =================",
		runtimeBadge,
		"",
		"  CONFIGURATION",
		"  ==============",
		"  Config file:        ~/.config/agent-htop/config.toml",
		"  Environment vars:   AGENT_HTOP_* (see config file for details)",
		"",
		"  Press [?] to close this help screen",
		"",
	}, "\n")

	boxedHelp := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("4")).
		Padding(1).
		Render(helpText)

	return boxedHelp
}

// renderEmptyState renders a friendly onboarding message when no agents are detected.
func (m *Model) renderEmptyState() string {
	runtimeBadge := m.renderRuntimeBadge()

	emptyText := strings.Join([]string{
		"",
		"  No AI agents detected yet 🤔",
		"",
		"  RUNTIME DETECTION STATUS",
		"  ========================",
		runtimeBadge,
		"",
		"  GETTING STARTED",
		"  ===============",
		"  1. Claude Code: Sessions are auto-detected from ~/.claude/projects/",
		"  2. Paperclip:   Start with: agent-htop --company <company-id>",
		"  3. Codex:       Sessions auto-detected from ~/.codex/",
		"",
		"  CONFIGURATION",
		"  ==============",
		fmt.Sprintf("  Config file: ~/.config/agent-htop/config.toml"),
		"  Edit this file to customize refresh rate, API URL, and alerts.",
		"",
		"  HELP",
		"  ====",
		"  Press [?] for keybindings and configuration help",
		"  Press [q] to quit",
		"",
	}, "\n")

	boxedEmpty := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("3")).
		Padding(1).
		Render(emptyText)

	return boxedEmpty
}

// renderRuntimeBadge renders a line showing which runtimes are detected.
// Shows green ✓ for detected, gray ✗ for missing.
func (m *Model) renderRuntimeBadge() string {
	runtimes := []string{"claude", "codex", "paperclip"}
	var badges []string

	for _, rt := range runtimes {
		detected := m.runtimeStatus[rt]
		var badge string
		if detected {
			badge = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2")).
				Render(fmt.Sprintf("%s ✓", rt))
		} else {
			badge = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Render(fmt.Sprintf("%s ✗", rt))
		}
		badges = append(badges, badge)
	}

	return "  " + strings.Join(badges, "  ")
}

// renderDetailDrawer renders a detailed view of the selected agent (right-side drawer, ~40% width).
func (m *Model) renderDetailDrawer() string {
	// Find the agent by ID
	var agent *aggregator.AgentView
	for _, a := range m.fleet.Agents {
		if a.AgentID == m.detailAgentID {
			agent = a
			break
		}
	}

	if agent == nil {
		return "Agent not found\n"
	}

	// Get recent runs for this agent (up to 5)
	runs := m.aggregator.GetRecentRunsForAgent(m.detailAgentID, 5)
	if len(runs) == 0 {
		return fmt.Sprintf("No runs found for agent %s\n", agent.AgentName)
	}

	// Use the most recent run for details
	run := runs[0]

	// Build the detail content
	var lines []string
	lines = append(lines, fmt.Sprintf("Agent: %s", agent.AgentName))
	lines = append(lines, fmt.Sprintf("Runtime: %s", run.Runtime))
	lines = append(lines, fmt.Sprintf("Model: %s", run.Model))

	// Session ID or Run ID
	if run.SessionID != "" {
		lines = append(lines, fmt.Sprintf("Session ID: %s", truncate(run.SessionID, 40)))
	}
	lines = append(lines, fmt.Sprintf("Run ID: %s", truncate(run.RunID, 40)))

	// Working directory
	if run.WorkDir != "" {
		lines = append(lines, fmt.Sprintf("Working Dir: %s", truncate(run.WorkDir, 40)))
	}

	// Timing
	lines = append(lines, fmt.Sprintf("Start Time: %s", run.StartTime.Format("2006-01-02 15:04:05")))
	if !run.EndTime.IsZero() {
		lines = append(lines, fmt.Sprintf("Duration: %dms", run.DurationMS))
	} else {
		lines = append(lines, fmt.Sprintf("Elapsed: %s", formatElapsed(run.DurationMS, "running")))
	}

	// Token counts
	lines = append(lines, fmt.Sprintf("Input Tokens: %d", run.TotalInputTokens))
	lines = append(lines, fmt.Sprintf("Output Tokens: %d", run.TotalOutputTokens))
	lines = append(lines, fmt.Sprintf("Total Cost: $%.4f", run.TotalCostUSD))

	// Status
	statusColor := m.colorStatus(run.Status, run.IsError)
	lines = append(lines, fmt.Sprintf("Status: %s", statusColor))

	// Task history from agent view (with full details)
	if agent.TaskHistory != nil && len(agent.TaskHistory) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Task History:")
		for i, task := range agent.TaskHistory {
			taskNum := i + 1
			status := "✓"
			if task.IsError {
				status = "✗"
			}

			// Format: "1. ✓ Tool: args (5s)" or "1. ✗ Tool: args (12s) [ERROR]"
			taskLine := fmt.Sprintf("  #%d %s %s: %s (%ds)",
				taskNum, status, task.ToolName, truncate(task.ArgsSummary, 25), task.DurationSec)

			if task.IsError && task.Result != "" {
				taskLine += " [" + truncate(task.Result, 20) + "]"
			}
			lines = append(lines, taskLine)
		}
	}

	// Last assistant message (truncated to 500 chars)
	if run.Result != "" {
		lines = append(lines, "")
		lines = append(lines, "Last Message:")
		msg := run.Result
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		// Word-wrap the message
		for _, line := range strings.Split(msg, "\n") {
			for len(line) > 70 {
				lines = append(lines, "  "+line[:70])
				line = line[70:]
			}
			if line != "" {
				lines = append(lines, "  "+line)
			}
		}
	}

	// Join all lines
	content := strings.Join(lines, "\n")

	// Build the drawer with border
	drawer := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(1).
		Width(80).
		Render(content)

	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Render("Press [Enter] or [Esc] to close")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		drawer,
		footer,
	)
}

// renderToolHeatmap renders the tool usage heatmap view.
func (m *Model) renderToolHeatmap() string {
	if m.toolHeatmap == nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("Loading tool heatmap...\n")
	}

	// Refresh heatmap on every render (keep it current)
	m.loadToolHeatmap()

	// Build header
	header := "Tool Usage Heatmap\n"
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styledHeader := headerStyle.Render(header)

	// Build table
	columns := []table.Column{
		{Title: "TOOL", Width: 20},
		{Title: "CALLS", Width: 8},
		{Title: "ERRORS", Width: 8},
		{Title: "P50MS", Width: 8},
		{Title: "P95MS", Width: 8},
		{Title: "MAXMS", Width: 8},
		{Title: "SUCCESS%", Width: 10},
		{Title: "EST.COST", Width: 12},
	}

	var rows []table.Row
	for _, tool := range m.toolHeatmap.Tools {
		successPct := tool.SuccessRate * 100
		row := table.Row{
			truncate(tool.Name, 20),
			fmt.Sprintf("%d", tool.CallCount),
			fmt.Sprintf("%d", tool.ErrorCount),
			fmt.Sprintf("%.1f", tool.P50DurationMS),
			fmt.Sprintf("%.1f", tool.P95DurationMS),
			fmt.Sprintf("%.1f", tool.MaxDurationMS),
			fmt.Sprintf("%.1f%%", successPct),
			fmt.Sprintf("$%.4f", tool.TotalCostEst),
		}
		rows = append(rows, row)
	}

	heatmapTable := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Bold(false)
	heatmapTable.SetStyles(s)

	// Summary stats
	totalCostEst := 0.0
	for _, tool := range m.toolHeatmap.Tools {
		totalCostEst += tool.TotalCostEst
	}

	summaryLine := fmt.Sprintf("\n  Total Calls: %d  Total Errors: %d  Total Cost Est: $%.4f",
		m.toolHeatmap.TotalCalls, m.toolHeatmap.TotalErrors, totalCostEst)
	summaryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styledSummary := summaryStyle.Render(summaryLine)

	// Help text
	helpText := "\n  [T]back to live view  [e]export CSV"
	styledHelp := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(helpText)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		styledHeader,
		heatmapTable.View(),
		styledSummary,
		styledHelp,
	)
}

// loadToolHeatmap loads tool heatmap data from the aggregator.
func (m *Model) loadToolHeatmap() {
	m.toolHeatmap = m.aggregator.GetToolHeatmap(m.toolFilterAgentID)
}

// exportToolHeatmapCSV exports the current tool heatmap to a CSV file.
func (m *Model) exportToolHeatmapCSV() {
	if m.toolHeatmap == nil || len(m.toolHeatmap.Tools) == 0 {
		return
	}

	// Create filename with timestamp
	timestamp := time.Now().Format("2006-01-02_150405")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	filename := filepath.Join(homeDir, fmt.Sprintf("tool-heatmap-%s.csv", timestamp))

	// Create file
	file, err := os.Create(filename)
	if err != nil {
		return
	}
	defer file.Close()

	// Write CSV
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"Tool", "CallCount", "ErrorCount", "SuccessRate", "P50DurationMS", "P95DurationMS", "MaxDurationMS", "TotalCostEst"}
	writer.Write(header)

	// Write rows
	for _, tool := range m.toolHeatmap.Tools {
		row := []string{
			tool.Name,
			fmt.Sprintf("%d", tool.CallCount),
			fmt.Sprintf("%d", tool.ErrorCount),
			fmt.Sprintf("%.4f", tool.SuccessRate),
			fmt.Sprintf("%.2f", tool.P50DurationMS),
			fmt.Sprintf("%.2f", tool.P95DurationMS),
			fmt.Sprintf("%.2f", tool.MaxDurationMS),
			fmt.Sprintf("%.4f", tool.TotalCostEst),
		}
		writer.Write(row)
	}
}

// StateUpdateMsg is a message containing a state update from the aggregator.
type StateUpdateMsg struct {
	State *aggregator.FleetState
}

// ActionResultMsg is a message containing the result of an action (kill, pause, resume).
type ActionResultMsg struct {
	AgentID string
	Error   string
}

// ProcessKillMsg is a message containing the result of killing a process.
type ProcessKillMsg struct {
	PID   int32
	Error string
}

// subscribeToStateUpdates subscribes to state updates from the aggregator.
func subscribeToStateUpdates(agg *aggregator.Aggregator) tea.Cmd {
	return func() tea.Msg {
		state := <-agg.StateUpdates()
		return StateUpdateMsg{State: state}
	}
}

// terminateAgent sends a terminate signal to the Paperclip API.
func terminateAgent(client *api.Client, agentID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := client.TerminateAgent(ctx, agentID)
		if err != nil {
			return ActionResultMsg{AgentID: agentID, Error: err.Error()}
		}

		return ActionResultMsg{AgentID: agentID, Error: ""}
	}
}

// pauseAgent pauses an agent via the Paperclip API.
func pauseAgent(client *api.Client, agentID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := client.PauseAgent(ctx, agentID)
		if err != nil {
			return ActionResultMsg{AgentID: agentID, Error: err.Error()}
		}

		return ActionResultMsg{AgentID: agentID, Error: ""}
	}
}

// killProcessCmd kills a system process.
func killProcessCmd(pid int32, signal string) tea.Cmd {
	return func() tea.Msg {
		err := sysinfo.KillProcess(pid, signal)
		if err != nil {
			return ProcessKillMsg{PID: pid, Error: fmt.Sprintf("Failed to kill PID %d: %v", pid, err)}
		}

		return ProcessKillMsg{PID: pid, Error: ""}
	}
}

// renderQueueView renders the task queue view.
func (m *Model) renderQueueView() string {
	if m.queueManager == nil {
		return "Queue manager not initialized\n"
	}

	// Get queue stats
	snapshot, err := m.queueManager.GetAllStats()
	if err != nil {
		return fmt.Sprintf("Error loading queue stats: %v\n", err)
	}

	// Build queue summary table
	var queueHeader = "QUEUE              PENDING  CLAIMED  COMPLETE  FAILED  AVG TIME\n"
	var queueRows string
	for _, stats := range snapshot.Queues {
		avgTime := "-"
		if stats.AvgTimeMs > 0 {
			seconds := stats.AvgTimeMs / 1000
			ms := stats.AvgTimeMs % 1000
			if seconds > 0 {
				avgTime = fmt.Sprintf("%.1fs", float64(stats.AvgTimeMs)/1000)
			} else {
				avgTime = fmt.Sprintf("%dms", ms)
			}
		}
		queueRows += fmt.Sprintf("%-18s %7d  %7d  %8d  %6d  %s\n",
			truncate(stats.Name, 18),
			stats.Pending,
			stats.Claimed,
			stats.Completed,
			stats.Failed,
			avgTime,
		)
	}

	// Get recent tasks from all queues
	var recentTasks string
	recentTasksHeader := "RECENT TASKS:\n"
	recentTasksHeader += "TASK ID          QUEUE              AGENT               STATUS     CREATED\n"

	queueNames := m.queueManager.GetQueueNames()
	var allTasks []*queue.Task
	for _, qName := range queueNames {
		tasks, _ := m.queueManager.ListTasks(qName, "", 5) // Get 5 most recent from each queue
		allTasks = append(allTasks, tasks...)
	}

	// Sort by created_at descending and take top 20
	if len(allTasks) > 20 {
		allTasks = allTasks[:20]
	}

	for _, task := range allTasks {
		created := task.CreatedAt.Format("15:04:05")
		recentTasks += fmt.Sprintf("%-16s  %-18s  %-19s  %-10s  %s\n",
			truncate(task.ID, 16),
			truncate(task.QueueName, 18),
			truncate(task.ClaimedBy, 19),
			truncate(task.Status, 10),
			created,
		)
	}

	// Combine sections
	view := "Task Queues  (Press Q to toggle | H for history | T for tools | ? for help)\n\n"
	view += queueHeader
	view += queueRows
	view += "\n" + recentTasksHeader + recentTasks

	return view
}

// resumeAgent resumes an agent via the Paperclip API.
func resumeAgent(client *api.Client, agentID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := client.ResumeAgent(ctx, agentID)
		if err != nil {
			return ActionResultMsg{AgentID: agentID, Error: err.Error()}
		}

		return ActionResultMsg{AgentID: agentID, Error: ""}
	}
}
