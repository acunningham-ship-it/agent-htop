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
}

// New creates a new TUI model.
func New(agg *aggregator.Aggregator, client *api.Client, companyID string) *Model {
	// Get active runtimes from aggregator
	activeRuntimes := agg.GetActiveRuntimes()
	runtimeStrs := make([]string, len(activeRuntimes))
	for i, rt := range activeRuntimes {
		runtimeStrs[i] = string(rt)
	}

	m := &Model{
		aggregator:  agg,
		apiClient:   client,
		fleet:       &aggregator.FleetState{},
		actionFlash: make(map[string]time.Time),
		filterMode:  FilterAll,
		sortMode:    SortName,
		searchQuery: "",
		searchMode:  false,
		companyID:   companyID,
		runtimes:    runtimeStrs,
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
		// TODO(HTO-35): Once Runtime field is added to AgentView, check if the selected
		// agent's runtime supports killing (e.g., CapabilitiesByRuntime[selected.Runtime].CanKill)
		// and skip this action for Claude Code sessions.
		filtered := m.filterAgents(m.fleet.Agents)
		if len(filtered) > 0 && m.selectedRow < len(filtered) {
			m.confirmKill = true
			m.confirmAgent = filtered[m.selectedRow].AgentID
		}
	case "y":
		// Confirm kill
		if m.confirmKill && m.confirmAgent != "" {
			m.confirmKill = false
			agentID := m.confirmAgent
			m.confirmAgent = ""
			return m, terminateAgent(m.apiClient, agentID)
		}
	case "n", "esc":
		// Cancel confirmation
		m.confirmKill = false
		m.confirmAgent = ""
	case "P":
		// Shift+P to pause selected agent
		filtered := m.filterAgents(m.fleet.Agents)
		if len(filtered) > 0 && m.selectedRow < len(filtered) {
			agentID := filtered[m.selectedRow].AgentID
			return m, pauseAgent(m.apiClient, agentID)
		}
	case "R":
		// Shift+R to resume selected agent
		filtered := m.filterAgents(m.fleet.Agents)
		if len(filtered) > 0 && m.selectedRow < len(filtered) {
			agentID := filtered[m.selectedRow].AgentID
			return m, resumeAgent(m.apiClient, agentID)
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

	header := m.renderHeader()

	var tableView string
	if m.searchMode {
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
	// TODO(HTO-35): Once Runtime field is added to AgentView, make [K]ill hint conditional:
	// grey it out or replace with "(kill: n/a for claude)" when a Claude session is selected.
	header := fmt.Sprintf(
		"agent-htop v0.2.0 | %s | %d sessions | updated %s ago   %s  %s%s  [q]uit [K]ill [P]ause [R]esume [/]search\n",
		runtimeStr,
		len(m.fleet.Agents),
		formatSinceUpdate(m.fleet.UpdatedAt),
		filterStr,
		sortStr,
		searchStr,
	)

	// Optional second line: show company ID if --company was set
	var companyLine string
	if m.companyID != "" {
		companyLine = fmt.Sprintf("  Paperclip: %s\n", m.companyID)
		// Style the company line as dimmed
		companyLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Render(companyLine)
	}

	// Style the header
	mainHeader := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Render(header)

	return mainHeader + companyLine
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
