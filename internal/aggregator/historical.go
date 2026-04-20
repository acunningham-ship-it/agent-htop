package aggregator

import (
	"time"
)

// DayBucket represents aggregated metrics for a single day.
type DayBucket struct {
	Date              time.Time             // Midnight on this date (local time)
	SessionCount      int                   // Total number of runs
	TotalCostUSD      float64               // Sum of all costs
	TotalInputTokens  int64                 // Sum of input tokens
	TotalOutputTokens int64                 // Sum of output tokens
	TopModel          string                // Most frequently used model
	ErrorCount        int                   // Number of failed runs
	ModelBreakdown    map[string]int        // Model -> count
}

// HistoricalView represents the last 7 days of aggregated data (including today).
type HistoricalView struct {
	Days      []*DayBucket // 7 days, oldest first (index 0 is 6 days ago, index 6 is today)
	GeneratedAt time.Time
}

// NewHistoricalView creates an empty historical view.
func NewHistoricalView() *HistoricalView {
	return &HistoricalView{
		Days:        make([]*DayBucket, 0, 7),
		GeneratedAt: time.Now(),
	}
}

// AddDay adds a day bucket to the historical view.
func (h *HistoricalView) AddDay(bucket *DayBucket) {
	h.Days = append(h.Days, bucket)
}

// PadToSevenDays ensures the view has exactly 7 days (filling empty days if needed).
func (h *HistoricalView) PadToSevenDays(now time.Time) {
	// Ensure we have 7 days: today minus 6 days
	for len(h.Days) < 7 {
		missingDay := now.Add(-time.Duration((7-len(h.Days))*24) * time.Hour)
		year, month, day := missingDay.Date()
		midnight := time.Date(year, month, day, 0, 0, 0, 0, missingDay.Location())

		h.Days = append([]*DayBucket{
			{
				Date:           midnight,
				SessionCount:   0,
				TotalCostUSD:   0,
				TotalInputTokens: 0,
				TotalOutputTokens: 0,
				TopModel:       "-",
				ErrorCount:     0,
				ModelBreakdown: make(map[string]int),
			},
		}, h.Days...)
	}

	// Keep only last 7 days
	if len(h.Days) > 7 {
		h.Days = h.Days[len(h.Days)-7:]
	}
}

// dayBucketKey returns the date key (midnight) for a given timestamp in local time.
func dayBucketKey(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

// daysBetween returns the number of days between two dates (rounded down).
func daysBetween(from, to time.Time) int {
	from = dayBucketKey(from)
	to = dayBucketKey(to)
	return int(to.Sub(from).Hours() / 24)
}

// MaxCostForChart returns the maximum cost across all days (for scaling the bar chart).
func (h *HistoricalView) MaxCostForChart() float64 {
	var max float64
	for _, day := range h.Days {
		if day.TotalCostUSD > max {
			max = day.TotalCostUSD
		}
	}
	return max
}

// TopModelName determines the most common model across all days.
func (h *HistoricalView) TopModelName() string {
	modelCounts := make(map[string]int)
	for _, day := range h.Days {
		for model, count := range day.ModelBreakdown {
			modelCounts[model] += count
		}
	}

	if len(modelCounts) == 0 {
		return "-"
	}

	var top string
	var maxCount int
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			top = model
		}
	}
	return top
}
