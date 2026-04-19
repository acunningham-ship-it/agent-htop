package anomaly

import (
	"fmt"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/parser"
)

// HighSpendRule detects agents spending >$0.50/hr sustained over 15 min window.
type HighSpendRule struct{}

func (r *HighSpendRule) Name() string {
	return "HighSpendRule"
}

func (r *HighSpendRule) Detect(agentID, agentName string, runs []*parser.AgentRun) []*AnomalyEvent {
	var events []*AnomalyEvent

	if len(runs) == 0 {
		return events
	}

	// Check the most recent 15 minutes of runs
	now := time.Now()
	fifteenMinAgo := now.Add(-15 * time.Minute)

	var recentRuns []*parser.AgentRun
	var totalCost float64
	var totalDuration float64

	for _, run := range runs {
		if run.EndTime.After(fifteenMinAgo) {
			recentRuns = append(recentRuns, run)
			totalCost += run.TotalCostUSD
			totalDuration += float64(run.DurationMS) / 1000 / 3600 // Convert ms to hours
		}
	}

	if len(recentRuns) == 0 {
		return events
	}

	// Calculate hourly rate: total cost / total hours
	if totalDuration > 0 {
		hourlyRate := totalCost / totalDuration
		if hourlyRate > 0.50 {
			events = append(events, &AnomalyEvent{
				AgentID:     agentID,
				AgentName:   agentName,
				AnomalyType: HighSpend,
				Message:     fmt.Sprintf("High spend detected: $%.4f/hr (threshold: $0.50/hr)", hourlyRate),
				DetectedAt:  now,
				Severity:    "warning",
			})
		}
	}

	return events
}

// ErrorStreakRule detects ≥3 errored runs in 10 min window.
type ErrorStreakRule struct{}

func (r *ErrorStreakRule) Name() string {
	return "ErrorStreakRule"
}

func (r *ErrorStreakRule) Detect(agentID, agentName string, runs []*parser.AgentRun) []*AnomalyEvent {
	var events []*AnomalyEvent

	if len(runs) == 0 {
		return events
	}

	// Check the most recent 10 minutes of runs
	now := time.Now()
	tenMinAgo := now.Add(-10 * time.Minute)

	var errorCount int
	for _, run := range runs {
		if run.EndTime.After(tenMinAgo) && run.IsError {
			errorCount++
		}
	}

	if errorCount >= 3 {
		events = append(events, &AnomalyEvent{
			AgentID:     agentID,
			AgentName:   agentName,
			AnomalyType: ErrorStreak,
			Message:     fmt.Sprintf("Error streak detected: %d failed runs in last 10 min", errorCount),
			DetectedAt:  now,
			Severity:    "critical",
		})
	}

	return events
}

// CostAnomalyRule detects total cost today >5× 7-day rolling average.
// Note: This rule requires the detector to maintain rolling average state.
type CostAnomalyRule struct{}

func (r *CostAnomalyRule) Name() string {
	return "CostAnomalyRule"
}

func (r *CostAnomalyRule) Detect(agentID, agentName string, runs []*parser.AgentRun) []*AnomalyEvent {
	// This rule requires detector state (dailyCosts, sevenDayAvg).
	// It will be called by ProcessRun after state is updated, so we skip
	// the actual logic here and instead emit in the detector itself.
	// Return empty for now.
	return []*AnomalyEvent{}
}

// CostAnomalyDetect is called by the Detector after updating rolling averages.
func (d *Detector) detectCostAnomaly(agentID, agentName string) []*AnomalyEvent {
	var events []*AnomalyEvent

	d.mu.RLock()
	dailyCost := d.dailyCosts[agentID]
	sevenDayAvg := d.sevenDayAvg[agentID]
	d.mu.RUnlock()

	// Avoid division by zero and only trigger if avg is meaningful
	if sevenDayAvg > 0 && dailyCost > 5*sevenDayAvg {
		events = append(events, &AnomalyEvent{
			AgentID:     agentID,
			AgentName:   agentName,
			AnomalyType: CostAnomaly,
			Message:     fmt.Sprintf("Cost anomaly: Today's spend $%.2f is %.1f× the 7-day average ($%.2f)", dailyCost, dailyCost/sevenDayAvg, sevenDayAvg),
			DetectedAt:  time.Now(),
			Severity:    "critical",
		})
	}

	return events
}
