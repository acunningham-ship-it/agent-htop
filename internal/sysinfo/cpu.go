package sysinfo

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
)

// CPUMetrics contains CPU-related system information.
type CPUMetrics struct {
	PercentPerCore []float64 `json:"percentPerCore"` // Per-core CPU percentage
	AveragePercent float64   `json:"averagePercent"` // Average CPU percentage across all cores
	Load1Min       float64   `json:"load1Min"`       // 1-minute load average
	Load5Min       float64   `json:"load5Min"`       // 5-minute load average
	Load15Min      float64   `json:"load15Min"`      // 15-minute load average
	LogicalCores   int       `json:"logicalCores"`   // Number of logical CPU cores
	PhysicalCores  int       `json:"physicalCores"`  // Number of physical CPU cores
	Uptime         uint64    `json:"uptime"`         // System uptime in seconds
	UpdatedAt      time.Time `json:"updatedAt"`
}

// CPUCollector periodically collects CPU metrics.
type CPUCollector struct {
	mu      sync.RWMutex
	metrics *CPUMetrics
	ticker  *time.Ticker
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewCPUCollector creates a new CPU metrics collector.
func NewCPUCollector(interval time.Duration) *CPUCollector {
	return &CPUCollector{
		metrics: &CPUMetrics{},
		ticker:  time.NewTicker(interval),
		stopCh:  make(chan struct{}),
	}
}

// Start begins collecting CPU metrics.
func (c *CPUCollector) Start() error {
	// Collect immediately on start
	if err := c.collect(); err != nil {
		return err
	}

	// Start background collector
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			select {
			case <-c.ticker.C:
				_ = c.collect()
			case <-c.stopCh:
				return
			}
		}
	}()

	return nil
}

// Stop stops the collector.
func (c *CPUCollector) Stop() {
	close(c.stopCh)
	c.ticker.Stop()
	c.wg.Wait()
}

// Get returns the latest CPU metrics.
func (c *CPUCollector) Get() *CPUMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.metrics == nil {
		return &CPUMetrics{}
	}

	// Return a copy to prevent external mutation
	metrics := *c.metrics
	if metrics.PercentPerCore != nil {
		percentCopy := make([]float64, len(metrics.PercentPerCore))
		copy(percentCopy, metrics.PercentPerCore)
		metrics.PercentPerCore = percentCopy
	}
	return &metrics
}

// collect fetches the latest CPU metrics from the system.
func (c *CPUCollector) collect() error {
	// Get per-core CPU percentages
	percentages, err := cpu.Percent(time.Second, true)
	if err != nil {
		return err
	}

	// Get logical and physical core counts
	logicalCores, err := cpu.Counts(false)
	if err != nil {
		logicalCores = len(percentages)
	}

	physicalCores, err := cpu.Counts(true)
	if err != nil {
		physicalCores = logicalCores
	}

	// Calculate average CPU percentage
	var sum float64
	for _, p := range percentages {
		sum += p
	}
	avg := sum / float64(len(percentages))

	// Get load averages
	avg1, avg5, avg15 := 0.0, 0.0, 0.0
	if l, err := load.Avg(); err == nil {
		avg1 = l.Load1
		avg5 = l.Load5
		avg15 = l.Load15
	}

	// Get uptime
	uptime := uint64(0)
	if u, err := GetUptime(); err == nil {
		uptime = u
	}

	// Update metrics
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics = &CPUMetrics{
		PercentPerCore: percentages,
		AveragePercent: avg,
		Load1Min:       avg1,
		Load5Min:       avg5,
		Load15Min:      avg15,
		LogicalCores:   logicalCores,
		PhysicalCores:  physicalCores,
		Uptime:         uptime,
		UpdatedAt:      time.Now(),
	}

	return nil
}
