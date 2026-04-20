package sysinfo

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// MemoryMetrics contains memory-related system information.
type MemoryMetrics struct {
	TotalMB        uint64    // Total RAM in MB
	UsedMB         uint64    // Used RAM in MB
	FreeMB         uint64    // Free RAM in MB
	AvailableMB    uint64    // Available RAM in MB
	UsedPercent    float64   // Used percentage (0-100)
	SwapTotalMB    uint64    // Total swap in MB
	SwapUsedMB     uint64    // Used swap in MB
	SwapFreeMB     uint64    // Free swap in MB
	SwapUsedPercent float64  // Swap used percentage (0-100)
	CacheMB        uint64    // OS cache in MB
	UpdatedAt      time.Time
}

// MemoryCollector periodically collects memory metrics.
type MemoryCollector struct {
	mu      sync.RWMutex
	metrics *MemoryMetrics
	ticker  *time.Ticker
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewMemoryCollector creates a new memory metrics collector.
func NewMemoryCollector(interval time.Duration) *MemoryCollector {
	return &MemoryCollector{
		metrics: &MemoryMetrics{},
		ticker:  time.NewTicker(interval),
		stopCh:  make(chan struct{}),
	}
}

// Start begins collecting memory metrics.
func (m *MemoryCollector) Start() error {
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
