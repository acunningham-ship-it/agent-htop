package sysinfo

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GPUMetrics contains GPU-related system information.
type GPUMetrics struct {
	TempC     float64   `json:"tempC"`     // GPU temperature in Celsius
	Available bool      `json:"available"` // Whether GPU data is available
	UpdatedAt time.Time `json:"updatedAt"`
}

// GPUCollector periodically collects GPU metrics.
type GPUCollector struct {
	mu      sync.RWMutex
	metrics *GPUMetrics
	ticker  *time.Ticker
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewGPUCollector creates a new GPU metrics collector.
func NewGPUCollector(interval time.Duration) *GPUCollector {
	return &GPUCollector{
		metrics: &GPUMetrics{Available: false},
		ticker:  time.NewTicker(interval),
		stopCh:  make(chan struct{}),
	}
}

// Start begins collecting GPU metrics.
func (g *GPUCollector) Start() error {
	// Collect immediately on start
	_ = g.collect()

	// Start background collector
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		for {
			select {
			case <-g.ticker.C:
				_ = g.collect()
			case <-g.stopCh:
				return
			}
		}
	}()

	return nil
}

// Stop stops the collector.
func (g *GPUCollector) Stop() {
	close(g.stopCh)
	g.ticker.Stop()
	g.wg.Wait()
}

// Get returns the latest GPU metrics.
func (g *GPUCollector) Get() *GPUMetrics {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.metrics == nil {
		return &GPUMetrics{Available: false}
	}

	// Return a copy to prevent external mutation
	metrics := *g.metrics
	return &metrics
}

// collect fetches the latest GPU metrics from the system.
func (g *GPUCollector) collect() error {
	// Try nvidia-smi first (NVIDIA GPUs)
	temp, err := getNVIDIAGPUTemp()
	if err == nil {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.metrics = &GPUMetrics{
			TempC:     temp,
			Available: true,
			UpdatedAt: time.Now(),
		}
		return nil
	}

	// Try other GPU detection methods here if needed

	// If no GPU found, mark as unavailable
	g.mu.Lock()
	defer g.mu.Unlock()
	g.metrics = &GPUMetrics{
		Available: false,
		UpdatedAt: time.Now(),
	}

	return nil
}

// getNVIDIAGPUTemp attempts to get GPU temperature via nvidia-smi.
func getNVIDIAGPUTemp() (float64, error) {
	// Run nvidia-smi to get GPU temperature
	cmd := exec.Command("nvidia-smi", "--query-gpu=temperature.gpu", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Parse the output (typically a single number)
	tempStr := strings.TrimSpace(string(output))
	temp, err := strconv.ParseFloat(tempStr, 64)
	if err != nil {
		return 0, err
	}

	return temp, nil
}
