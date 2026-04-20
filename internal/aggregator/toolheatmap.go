package aggregator

import (
	"sort"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/parser"
)

// ComputeToolHeatmap computes tool usage statistics from a slice of agent runs.
// Filters runs based on optional agentID (empty = all agents).
// Returns a ToolHeatmap sorted by call count (descending).
func ComputeToolHeatmap(runs []*parser.AgentRun, filterAgentID string) *parser.ToolHeatmap {
	heatmap := &parser.ToolHeatmap{
		Tools:       make([]*parser.ToolUsage, 0),
		TotalCalls:  0,
		TotalErrors: 0,
		GeneratedAt: time.Now(),
	}

	// Aggregate tool metrics by tool name
	toolMetrics := make(map[string]*ToolAggregator)

	for _, run := range runs {
		// Skip if filtering by agent ID
		if filterAgentID != "" && run.AgentID != filterAgentID {
			continue
		}

		for _, toolCall := range run.ToolCalls {
			if toolCall.Name == "" {
				continue
			}

			if _, exists := toolMetrics[toolCall.Name]; !exists {
				toolMetrics[toolCall.Name] = NewToolAggregator(toolCall.Name)
			}

			toolMetrics[toolCall.Name].AddCall(toolCall)
			heatmap.TotalCalls++
			if toolCall.IsError {
				heatmap.TotalErrors++
			}
		}
	}

	// Convert aggregators to ToolUsage objects
	for _, agg := range toolMetrics {
		heatmap.Tools = append(heatmap.Tools, agg.Finalize())
	}

	// Sort by call count (descending)
	sort.Slice(heatmap.Tools, func(i, j int) bool {
		return heatmap.Tools[i].CallCount > heatmap.Tools[j].CallCount
	})

	return heatmap
}

// ToolAggregator accumulates statistics for a single tool.
type ToolAggregator struct {
	Name      string
	CallCount int
	ErrorCount int
	Durations []float64 // Duration in ms for each call
	CostEst   float64   // Estimated cost based on tool type
}

// NewToolAggregator creates a new tool aggregator.
func NewToolAggregator(name string) *ToolAggregator {
	return &ToolAggregator{
		Name:      name,
		Durations: make([]float64, 0),
		CostEst:   estimateToolCost(name),
	}
}

// AddCall adds a tool call to the aggregator.
func (ta *ToolAggregator) AddCall(call *parser.ToolCall) {
	ta.CallCount++
	if call.IsError {
		ta.ErrorCount++
	}

	// Calculate duration if both times are set
	if !call.StartTime.IsZero() && !call.EndTime.IsZero() {
		duration := call.EndTime.Sub(call.StartTime).Milliseconds()
		ta.Durations = append(ta.Durations, float64(duration))
	}
}

// Finalize converts the aggregator to a ToolUsage object with computed percentiles.
func (ta *ToolAggregator) Finalize() *parser.ToolUsage {
	usage := &parser.ToolUsage{
		Name:       ta.Name,
		CallCount:  ta.CallCount,
		ErrorCount: ta.ErrorCount,
		TotalCostEst: ta.CostEst * float64(ta.CallCount), // Rough multiplier
	}

	// Calculate success rate
	if ta.CallCount > 0 {
		usage.SuccessRate = float64(ta.CallCount-ta.ErrorCount) / float64(ta.CallCount)
	}

	// Calculate percentiles (P50, P95, Max)
	if len(ta.Durations) > 0 {
		sorted := make([]float64, len(ta.Durations))
		copy(sorted, ta.Durations)
		sort.Float64s(sorted)

		usage.MaxDurationMS = sorted[len(sorted)-1]

		// P50 (median)
		usage.P50DurationMS = percentile(sorted, 0.5)

		// P95
		usage.P95DurationMS = percentile(sorted, 0.95)
	}

	return usage
}

// percentile returns the value at a given percentile (0-1) of a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	index := float64(len(sorted)-1) * p
	lower := int(index)
	upper := lower + 1

	if upper >= len(sorted) {
		return sorted[lower]
	}

	// Linear interpolation
	fraction := index - float64(lower)
	return sorted[lower]*(1-fraction) + sorted[upper]*fraction
}

// estimateToolCost returns a rough cost estimate per call for known tools.
// This is a heuristic; actual costs depend on API usage and parameters.
func estimateToolCost(toolName string) float64 {
	// Estimate per-call cost in USD for common tools
	// These are rough guesses based on API pricing
	switch toolName {
	case "WebFetch", "WebSearch":
		return 0.0005 // Network I/O
	case "Read", "Write", "Edit":
		return 0.0001 // File I/O
	case "Bash", "Glob", "Grep":
		return 0.0001 // Search/local operations
	case "Agent":
		return 0.01 // Spawns new agent session
	case "Skill":
		return 0.005 // External skill invocation
	default:
		return 0.0001 // Default estimate for unknown tools
	}
}
