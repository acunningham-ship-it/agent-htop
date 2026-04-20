package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/acunningham-ship-it/agent-htop/internal/aggregator"
	"github.com/acunningham-ship-it/agent-htop/internal/api"
)

// ViewMode represents the current view state
type ViewMode string

const (
	ViewModeLive       ViewMode = "live"
	ViewModeHistorical ViewMode = "historical"
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

	// Runtime detection results
	runtimeStatus map[string]bool // runtime name -> detected (true/false)
}

// New creates a new TUI model.
func New(agg *aggregator.Aggregator, client *api.Client, companyID string) *Model {
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
		{Title: "LAST TOOL", Width: 20},
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
		} else {
			m.viewMode = ViewModeLive
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
	if m.viewMode == ViewModeHistorical {
		tableView = m.renderHistoricalView()
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
		"agent-htop v0.2.0 | %s | %d sessions | updated %s ago   %s  %s%s  [q]uit [K]ill [P]ause [R]esume [H]istory [/]search\n",
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

	// Cost projection line (shown when any agent has projection data)
	projectionLine := m.renderProjectionLine()

	return mainHeader + companyLine + projectionLine
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

	// Last 5 tool calls
	if len(run.ToolCalls) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Recent Tool Calls:")
		// Show up to 5 most recent tool calls
		start := len(run.ToolCalls) - 5
		if start < 0 {
			start = 0
		}
		for i := start; i < len(run.ToolCalls); i++ {
			tc := run.ToolCalls[i]
			timeStr := ""
			if !tc.StartTime.IsZero() {
				timeStr = tc.StartTime.Format("15:04:05")
			}
			lines = append(lines, fmt.Sprintf("  [%s] %s", timeStr, truncate(tc.Name, 30)))
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

// StateUpdateMsg is a message containing a state update from the aggregator.
type StateUpdateMsg struct {
	State *aggregator.FleetState
}

// ActionResultMsg is a message containing the result of an action (kill, pause, resume).
type ActionResultMsg struct {
	AgentID string
	Error   string
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
