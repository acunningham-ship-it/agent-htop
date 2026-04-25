package sysinfo

import (
	"context"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// MemoryMetrics contains memory-related system information.
type MemoryMetrics struct {
	TotalMB        uint64    `json:"totalMB"`        // Total RAM in MB
	UsedMB         uint64    `json:"usedMB"`         // Used RAM in MB
	FreeMB         uint64    `json:"freeMB"`         // Free RAM in MB
	AvailableMB    uint64    `json:"availableMB"`    // Available RAM in MB
	UsedPercent    float64   `json:"usedPercent"`    // Used percentage (0-100)
	SwapTotalMB    uint64    `json:"swapTotalMB"`    // Total swap in MB
	SwapUsedMB     uint64    `json:"swapUsedMB"`     // Used swap in MB
	SwapFreeMB     uint64    `json:"swapFreeMB"`     // Free swap in MB
	SwapUsedPercent float64  `json:"swapUsedPercent"` // Swap used percentage (0-100)
	CacheMB        uint64    `json:"cacheMB"`        // OS cache in MB
	UpdatedAt      time.Time `json:"updatedAt"`
}

// MemoryCollector periodically collects memory metrics.
type MemoryCollector struct {
	mu      sync.RWMutex
	metrics *MemoryMetrics
	ticker  *time.Ticker
	stopCh  chan struct{}
	wg      sync.WaitGroup
	ctx     context.Context
}

// NewMemoryCollector creates a new memory metrics collector.
func NewMemoryCollector(interval time.Duration) *MemoryCollector {
	return &MemoryCollector{
		metrics: &MemoryMetrics{},
		ticker:  time.NewTicker(interval),
		stopCh:  make(chan struct{}),
		ctx:     context.Background(),
	}
}

// Start begins collecting memory metrics with context awareness.
func (m *MemoryCollector) Start(ctx context.Context) error {
	m.ctx = ctx
	// Collect immediately on start
	if err := m.collect(); err != nil {
		return err
	}

	// Start background collector
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.ticker.C:
				_ = m.collect()
			case <-m.stopCh:
				return
			}
		}
	}()

	return nil
}

// Stop stops the collector.
func (m *MemoryCollector) Stop() {
	close(m.stopCh)
	m.ticker.Stop()
	m.wg.Wait()
}

// Get returns the latest memory metrics.
func (m *MemoryCollector) Get() *MemoryMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metrics == nil {
		return &MemoryMetrics{}
	}

	// Return a copy to prevent external mutation
	metrics := *m.metrics
	return &metrics
}

// collect fetches the latest memory metrics from the system.
func (m *MemoryCollector) collect() error {
	// Get virtual memory stats
	vm, err := mem.VirtualMemory()
	if err != nil {
		return err
	}

	// Get swap memory stats
	sw, err := mem.SwapMemory()
	if err != nil {
		// Swap may not be available on all systems, use zeros
		sw = &mem.SwapMemoryStat{
			Total:       0,
			Used:        0,
			Free:        0,
			UsedPercent: 0,
		}
	}

	// Calculate cache (may vary by OS)
	var cacheBytes uint64
	if vm.Buffers > 0 {
		cacheBytes = vm.Buffers
	}
	if vm.Cached > 0 {
		cacheBytes += vm.Cached
	}

	// Update metrics
	m.mu.Lock()
	defer m.mu.Unlock()

	m.metrics = &MemoryMetrics{
		TotalMB:         vm.Total / 1024 / 1024,
		UsedMB:          vm.Used / 1024 / 1024,
		FreeMB:          vm.Free / 1024 / 1024,
		AvailableMB:     vm.Available / 1024 / 1024,
		UsedPercent:     vm.UsedPercent,
		SwapTotalMB:     sw.Total / 1024 / 1024,
		SwapUsedMB:      sw.Used / 1024 / 1024,
		SwapFreeMB:      sw.Free / 1024 / 1024,
		SwapUsedPercent: sw.UsedPercent,
		CacheMB:         cacheBytes / 1024 / 1024,
		UpdatedAt:       time.Now(),
	}

	return nil
}
