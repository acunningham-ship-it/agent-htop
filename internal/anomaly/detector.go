package anomaly

import (
	"fmt"
	"sync"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/parser"
)

// AnomalyType identifies a detected anomaly.
type AnomalyType string

const (
	HighSpend    AnomalyType = "HIGH_SPEND"
	ErrorStreak  AnomalyType = "ERROR_STREAK"
	CostAnomaly  AnomalyType = "COST_ANOMALY"
	// Note: STALLED detection requires heartbeat tracking, not just final runs.
	// Excluded from v1 as log schema provides completed runs only.
)

// AnomalyEvent represents a detected anomaly on an agent.
type AnomalyEvent struct {
	AgentID     string
	AgentName   string
	AnomalyType AnomalyType
	Message     string
	DetectedAt  time.Time
	Severity    string // "warning" | "critical"
}

// Detector runs pluggable anomaly detection rules against agent runs.
type Detector struct {
	mu sync.RWMutex

	// Per-agent recent run history
	agentRuns map[string][]*parser.AgentRun

	// Per-agent daily cost tracking for COST_ANOMALY
	dailyCosts    map[string]float64
	sevenDayAvg   map[string]float64
	costLastReset map[string]time.Time

	// Rules
	rules []Rule

	// Output channel
	eventsCh chan *AnomalyEvent
	stopCh   chan struct{}
}

// Rule defines an anomaly detection rule.
type Rule interface {
	Name() string
	Detect(agentID, agentName string, runs []*parser.AgentRun) []*AnomalyEvent
}

// NewDetector creates a new anomaly detector with default rules.
func NewDetector() *Detector {
	d := &Detector{
		agentRuns:     make(map[string][]*parser.AgentRun),
		dailyCosts:    make(map[string]float64),
		sevenDayAvg:   make(map[string]float64),
		costLastReset: make(map[string]time.Time),
		eventsCh:      make(chan *AnomalyEvent, 100),
		stopCh:        make(chan struct{}),
	}

	// Register default rules
	d.rules = []Rule{
		&HighSpendRule{},
		&ErrorStreakRule{},
		&CostAnomalyRule{},
	}

	return d
}

// ProcessRun analyzes a new or updated run and detects anomalies.
func (d *Detector) ProcessRun(run *parser.AgentRun, agentName string) {
	if run == nil {
		return
	}

	d.mu.Lock()

	// Update run history
	if d.agentRuns[run.AgentID] == nil {
		d.agentRuns[run.AgentID] = make([]*parser.AgentRun, 0)
	}

	// Replace if updating existing run, otherwise append
	found := false
	for i, existing := range d.agentRuns[run.AgentID] {
		if existing.RunID == run.RunID {
			d.agentRuns[run.AgentID][i] = run
			found = true
			break
		}
	}
	if !found {
		d.agentRuns[run.AgentID] = append(d.agentRuns[run.AgentID], run)
	}

	// Keep only last 100 runs per agent (sliding window for memory)
	if len(d.agentRuns[run.AgentID]) > 100 {
		d.agentRuns[run.AgentID] = d.agentRuns[run.AgentID][len(d.agentRuns[run.AgentID])-100:]
	}

	// Update daily cost tracking (reset at midnight)
	now := time.Now()
	if _, exists := d.costLastReset[run.AgentID]; !exists {
		d.costLastReset[run.AgentID] = now
		d.dailyCosts[run.AgentID] = 0
	} else if now.Day() != d.costLastReset[run.AgentID].Day() {
		// Day changed: save yesterday's total to rolling average and reset
		d.updateRollingAverage(run.AgentID)
		d.costLastReset[run.AgentID] = now
		d.dailyCosts[run.AgentID] = 0
	}

	d.dailyCosts[run.AgentID] += run.TotalCostUSD

	// Run all rules
	recentRuns := d.agentRuns[run.AgentID]
	for _, rule := range d.rules {
		// Skip CostAnomalyRule here; it's called below with detector state
		if _, ok := rule.(*CostAnomalyRule); ok {
			continue
		}
		events := rule.Detect(run.AgentID, agentName, recentRuns)
		for _, event := range events {
			select {
			case d.eventsCh <- event:
			case <-d.stopCh:
				d.mu.Unlock()
				return
			}
		}
	}

	// Get cost anomaly state before unlocking
	dailyCost := d.dailyCosts[run.AgentID]
	sevenDayAvg := d.sevenDayAvg[run.AgentID]
	d.mu.Unlock()

	// Check cost anomaly (no lock needed, we copied the values)
	if sevenDayAvg > 0 && dailyCost > 5*sevenDayAvg {
		event := &AnomalyEvent{
			AgentID:     run.AgentID,
			AgentName:   agentName,
			AnomalyType: CostAnomaly,
			Message:     fmt.Sprintf("Cost anomaly: Today's spend $%.2f is %.1f× the 7-day average ($%.2f)", dailyCost, dailyCost/sevenDayAvg, sevenDayAvg),
			DetectedAt:  time.Now(),
			Severity:    "critical",
		}
		select {
		case d.eventsCh <- event:
		case <-d.stopCh:
			return
		}
	}
}

// updateRollingAverage updates the 7-day rolling average with today's cost.
func (d *Detector) updateRollingAverage(agentID string) {
	today := d.dailyCosts[agentID]

	// Initialize if needed
	if d.sevenDayAvg[agentID] == 0 {
		d.sevenDayAvg[agentID] = today
		return
	}

	// Simple rolling average: (6 * prev_avg + today) / 7
	d.sevenDayAvg[agentID] = (6*d.sevenDayAvg[agentID] + today) / 7
}

// GetDailyCost returns the current day's total cost for an agent.
func (d *Detector) GetDailyCost(agentID string) float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.dailyCosts[agentID]
}

// GetSevenDayAverage returns the rolling 7-day average cost for an agent.
func (d *Detector) GetSevenDayAverage(agentID string) float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.sevenDayAvg[agentID]
}

// Events returns the anomaly event channel.
func (d *Detector) Events() <-chan *AnomalyEvent {
	return d.eventsCh
}

// Stop stops the detector.
func (d *Detector) Stop() {
	close(d.stopCh)
	// Close the event channel to unblock any receivers waiting for events.
	// Safe because ProcessRun() checks stopCh before sending.
	close(d.eventsCh)
}

// CurrentAnomalies returns a snapshot of current anomalies for the given agent.
// If agentID is empty, returns anomalies for all agents.
func (d *Detector) CurrentAnomalies(agentID string) []*AnomalyEvent {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []*AnomalyEvent

	// If specific agent requested, get anomalies for that agent only
	if agentID != "" {
		runs := d.agentRuns[agentID]
		if len(runs) == 0 {
			return result
		}

		// Run all rules for this agent
		for _, rule := range d.rules {
			events := rule.Detect(agentID, agentID, runs)
			result = append(result, events...)
		}

		// Check cost anomaly separately
		dailyCost := d.dailyCosts[agentID]
		sevenDayAvg := d.sevenDayAvg[agentID]
		if sevenDayAvg > 0 && dailyCost > 5*sevenDayAvg {
			result = append(result, &AnomalyEvent{
				AgentID:     agentID,
				AgentName:   agentID,
				AnomalyType: CostAnomaly,
				Message:     fmt.Sprintf("Cost anomaly: Today's spend $%.2f is %.1f× the 7-day average ($%.2f)", dailyCost, dailyCost/sevenDayAvg, sevenDayAvg),
				DetectedAt:  time.Now(),
				Severity:    "critical",
			})
		}

		return result
	}

	// Return anomalies for all agents
	for aid, runs := range d.agentRuns {
		if len(runs) == 0 {
			continue
		}

		// Run all rules for this agent
		for _, rule := range d.rules {
			events := rule.Detect(aid, aid, runs)
			result = append(result, events...)
		}

		// Check cost anomaly
		dailyCost := d.dailyCosts[aid]
		sevenDayAvg := d.sevenDayAvg[aid]
		if sevenDayAvg > 0 && dailyCost > 5*sevenDayAvg {
			result = append(result, &AnomalyEvent{
				AgentID:     aid,
				AgentName:   aid,
				AnomalyType: CostAnomaly,
				Message:     fmt.Sprintf("Cost anomaly: Today's spend $%.2f is %.1f× the 7-day average ($%.2f)", dailyCost, dailyCost/sevenDayAvg, sevenDayAvg),
				DetectedAt:  time.Now(),
				Severity:    "critical",
			})
		}
	}

	return result
}
