package sysinfo

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

// DiskMetrics contains disk-related system information.
type DiskMetrics struct {
	TotalGB    uint64    `json:"totalGB"`    // Total disk space in GB
	UsedGB     uint64    `json:"usedGB"`     // Used disk space in GB
	FreeGB     uint64    `json:"freeGB"`     // Free disk space in GB
	UsedPercent float64  `json:"usedPercent"` // Percentage of disk used
	Path       string    `json:"path"`       // Mount point (typically "/")
	UpdatedAt  time.Time `json:"updatedAt"`
}

// DiskCollector periodically collects disk metrics.
type DiskCollector struct {
	mu      sync.RWMutex
	metrics *DiskMetrics
	ticker  *time.Ticker
	stopCh  chan struct{}
	wg      sync.WaitGroup
	path    string // Path to monitor (default "/")
}

// NewDiskCollector creates a new disk metrics collector.
func NewDiskCollector(interval time.Duration) *DiskCollector {
	return &DiskCollector{
		metrics: &DiskMetrics{},
		ticker:  time.NewTicker(interval),
		stopCh:  make(chan struct{}),
		path:    "/",
	}
}

// Start begins collecting disk metrics.
func (d *DiskCollector) Start() error {
	// Collect immediately on start
	if err := d.collect(); err != nil {
		return err
	}

	// Start background collector
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			select {
			case <-d.ticker.C:
				_ = d.collect()
			case <-d.stopCh:
				return
			}
		}
	}()

	return nil
}

// Stop stops the collector.
func (d *DiskCollector) Stop() {
	close(d.stopCh)
	d.ticker.Stop()
	d.wg.Wait()
}

// Get returns the latest disk metrics.
func (d *DiskCollector) Get() *DiskMetrics {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.metrics == nil {
		return &DiskMetrics{}
	}

	// Return a copy to prevent external mutation
	metrics := *d.metrics
	return &metrics
}

// collect fetches the latest disk metrics from the system.
func (d *DiskCollector) collect() error {
	usage, err := disk.Usage(d.path)
	if err != nil {
		return err
	}

	// Update metrics (convert bytes to GB)
	d.mu.Lock()
	defer d.mu.Unlock()

	d.metrics = &DiskMetrics{
		TotalGB:     usage.Total / (1024 * 1024 * 1024),
		UsedGB:      usage.Used / (1024 * 1024 * 1024),
		FreeGB:      usage.Free / (1024 * 1024 * 1024),
		UsedPercent: usage.UsedPercent,
		Path:        d.path,
		UpdatedAt:   time.Now(),
	}

	return nil
}
