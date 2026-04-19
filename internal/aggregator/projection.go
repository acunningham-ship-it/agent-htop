package aggregator

import (
	"time"
)

// CostColor represents the color for cost projection display.
type CostColor string

const (
	ColorGreen  CostColor = "green"   // Below daily average
	ColorYellow CostColor = "yellow"  // 1-2x daily average
	ColorRed    CostColor = "red"     // >2x daily average
)

// CostSample represents a cost measurement at a specific time.
type CostSample struct {
	Time             time.Time
	CumulativeCostUSD float64
}

// Projection represents the calculated cost projection for today.
type Projection struct {
	SpentToday       float64   // Cost accumulated so far today
	ProjectedToday   float64   // Estimated total cost by midnight
	DailyAverage     float64   // Rolling average of daily spend
	SpendRate        float64   // Current spend rate (USD per second)
	Color            CostColor // Green/yellow/red based on comparison to average
	HoursRemaining   float64   // Hours until midnight
}

// AgentCostTracker tracks cumulative cost samples for an agent to enable projection.
type AgentCostTracker struct {
	AgentID        string
	LastCost       float64        // Most recent cumulative cost
	Samples        []CostSample   // Rolling window of cost samples (last 15 min)
	DailyTotals    []float64      // Daily cost totals for rolling average (last 7 days)
	LastSampleTime time.Time      // Time of last sample
	SampleInterval time.Duration  // Target interval between samples
}

// NewAgentCostTracker creates a new cost tracker for an agent.
func NewAgentCostTracker(agentID string) *AgentCostTracker {
	return &AgentCostTracker{
		AgentID:        agentID,
		Samples:        make([]CostSample, 0, 900), // 15 min at 1 sample/sec max
		DailyTotals:    make([]float64, 0, 7),
		LastSampleTime: time.Now(),
		SampleInterval: 60 * time.Second,
	}
}

// AddSample adds a cost sample, pruning old samples outside the rolling window.
func (t *AgentCostTracker) AddSample(cost float64, now time.Time) {
	t.LastCost = cost
	t.Samples = append(t.Samples, CostSample{
		Time:             now,
		CumulativeCostUSD: cost,
	})

	// Prune samples older than 15 minutes
	cutoff := now.Add(-15 * time.Minute)
	for len(t.Samples) > 0 && t.Samples[0].Time.Before(cutoff) {
		t.Samples = t.Samples[1:]
	}

	t.LastSampleTime = now
}

// CalculateProjection calculates the cost projection based on current samples.
func (t *AgentCostTracker) CalculateProjection(now time.Time, dailyAverage float64) *Projection {
	// Determine today's start and end
	year, month, day := now.Date()
	todayStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	tomorrowStart := todayStart.Add(24 * time.Hour)
	hoursRemaining := tomorrowStart.Sub(now).Hours()

	// Calculate spent today
	spentToday := t.calculateSpentToday(todayStart)

	// Calculate spend rate from linear regression
	spendRate := t.calculateSpendRate()

	// Project cost at midnight
	projectedToday := spentToday + spendRate*hoursRemaining*3600 // Convert hours to seconds

	// Determine color based on comparison to daily average
	color := ColorGreen
	if dailyAverage > 0 {
		ratio := projectedToday / dailyAverage
		if ratio > 2.0 {
			color = ColorRed
		} else if ratio > 1.0 {
			color = ColorYellow
		}
	}

	return &Projection{
		SpentToday:     spentToday,
		ProjectedToday: projectedToday,
		DailyAverage:   dailyAverage,
		SpendRate:      spendRate,
		Color:          color,
		HoursRemaining: hoursRemaining,
	}
}

// calculateSpentToday calculates the total cost accumulated since start of today.
func (t *AgentCostTracker) calculateSpentToday(todayStart time.Time) float64 {
	if len(t.Samples) == 0 {
		return 0
	}

	// Find the cost at the start of today
	var costAtStartOfDay float64 = 0

	// Look for a sample that was before today or at today's start
	for i := range t.Samples {
		if t.Samples[i].Time.Before(todayStart) {
			// Update the baseline as we find samples before today
			costAtStartOfDay = t.Samples[i].CumulativeCostUSD
		} else {
			break
		}
	}

	// Cost spent today = current cost - cost at start of day
	return t.LastCost - costAtStartOfDay
}

// calculateSpendRate uses linear regression on recent samples to estimate USD/second.
func (t *AgentCostTracker) calculateSpendRate() float64 {
	if len(t.Samples) < 2 {
		return 0
	}

	// Simple linear regression: fit y = mx + b where y is cost, x is time
	// We only care about the slope m (spend rate)
	n := float64(len(t.Samples))

	// Convert timestamps to seconds since first sample
	baseTime := t.Samples[0].Time.Unix()
	var sumX, sumY, sumXY, sumX2 float64

	for _, sample := range t.Samples {
		x := float64(sample.Time.Unix() - baseTime)
		y := sample.CumulativeCostUSD

		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// Linear regression formula: m = (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0
	}

	slope := (n*sumXY - sumX*sumY) / denominator

	// Ensure slope is non-negative (no negative spend rate)
	if slope < 0 {
		slope = 0
	}

	return slope
}

// RollingDailyAverage calculates a 7-day rolling average from daily totals.
func RollingDailyAverage(dailyTotals []float64) float64 {
	if len(dailyTotals) == 0 {
		return 0
	}

	sum := 0.0
	for _, total := range dailyTotals {
		sum += total
	}

	return sum / float64(len(dailyTotals))
}

// FormatProjection returns a human-readable string for display.
func FormatProjection(p *Projection) string {
	if p == nil {
		return "N/A"
	}

	if p.ProjectedToday < 0.01 {
		return "no data"
	}

	return string(p.Color)
}
