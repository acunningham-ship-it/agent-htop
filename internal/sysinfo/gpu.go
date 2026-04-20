package sysinfo

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GPU represents a single GPU device with its metrics.
type GPU struct {
	Name      string  `json:"name"`       // GPU model name
	Index     int     `json:"index"`      // GPU index (0, 1, etc.)
	UtilPct   float64 `json:"utilPct"`    // GPU utilization percentage (0-100)
	TempC     float64 `json:"tempC"`      // GPU temperature in Celsius
	VRAMUsed  uint64  `json:"vramUsed"`   // VRAM used in MB
	VRAMTotal uint64  `json:"vramTotal"`  // Total VRAM in MB
	PowerDraw float64 `json:"powerDraw"`  // Power draw in watts
}

// GPUMetrics contains GPU-related system information.
type GPUMetrics struct {
	GPUs      []*GPU    `json:"gpus"`      // List of detected GPUs
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
		metrics: &GPUMetrics{GPUs: []*GPU{}, Available: false},
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
		return &GPUMetrics{GPUs: []*GPU{}, Available: false}
	}

	// Return a copy to prevent external mutation
	metrics := *g.metrics
	if metrics.GPUs != nil {
		gpusCopy := make([]*GPU, len(metrics.GPUs))
		for i, gpu := range metrics.GPUs {
			gpuCopy := *gpu
			gpusCopy[i] = &gpuCopy
		}
		metrics.GPUs = gpusCopy
	}
	return &metrics
}

// collect fetches the latest GPU metrics from the system.
func (g *GPUCollector) collect() error {
	// Try NVIDIA first
	gpus, err := getNVIDIAGPUs()
	if err == nil && len(gpus) > 0 {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.metrics = &GPUMetrics{
			GPUs:      gpus,
			Available: true,
			UpdatedAt: time.Now(),
		}
		return nil
	}

	// Try Apple Silicon (powermetrics)
	gpus, err = getAppleSiliconGPU()
	if err == nil && len(gpus) > 0 {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.metrics = &GPUMetrics{
			GPUs:      gpus,
			Available: true,
			UpdatedAt: time.Now(),
		}
		return nil
	}

	// If no GPU found, mark as unavailable but don't error
	g.mu.Lock()
	defer g.mu.Unlock()
	g.metrics = &GPUMetrics{
		GPUs:      []*GPU{},
		Available: false,
		UpdatedAt: time.Now(),
	}

	return nil
}

// getNVIDIAGPUs attempts to get GPU metrics via nvidia-smi.
func getNVIDIAGPUs() ([]*GPU, error) {
	// Run nvidia-smi with CSV output for multiple GPUs
	// Query: index, name, utilization.gpu, utilization.memory, memory.used, memory.total, temperature.gpu, power.draw
	queryFields := "index,name,utilization.gpu,utilization.memory,memory.used,memory.total,temperature.gpu,power.draw"
	cmd := exec.Command("nvidia-smi", "--query-gpu="+queryFields, "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var gpus []*GPU
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		gpu, err := parseNVIDIAGPULine(line)
		if err == nil && gpu != nil {
			gpus = append(gpus, gpu)
		}
	}

	if len(gpus) == 0 {
		return nil, err
	}
	return gpus, nil
}

// parseNVIDIAGPULine parses a single line from nvidia-smi CSV output.
func parseNVIDIAGPULine(line string) (*GPU, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 8 {
		return nil, nil
	}

	gpu := &GPU{}

	// Parse index
	if idx, err := strconv.Atoi(strings.TrimSpace(fields[0])); err == nil {
		gpu.Index = idx
	}

	// Parse name
	gpu.Name = strings.TrimSpace(fields[1])

	// Parse GPU utilization
	if util, err := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64); err == nil {
		gpu.UtilPct = util
	}

	// Parse memory utilization (not used directly, but shows data is present)
	_ = strings.TrimSpace(fields[3])

	// Parse memory used (in MB)
	if memUsed, err := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 64); err == nil {
		gpu.VRAMUsed = memUsed
	}

	// Parse memory total (in MB)
	if memTotal, err := strconv.ParseUint(strings.TrimSpace(fields[5]), 10, 64); err == nil {
		gpu.VRAMTotal = memTotal
	}

	// Parse temperature
	if temp, err := strconv.ParseFloat(strings.TrimSpace(fields[6]), 64); err == nil {
		gpu.TempC = temp
	}

	// Parse power draw (in watts)
	powerStr := strings.TrimSpace(fields[7])
	if powerStr != "" {
		// Remove "W" suffix if present
		powerStr = strings.TrimSuffix(powerStr, "W")
		powerStr = strings.TrimSpace(powerStr)
		if power, err := strconv.ParseFloat(powerStr, 64); err == nil {
			gpu.PowerDraw = power
		}
	}

	return gpu, nil
}

// getAppleSiliconGPU attempts to detect Apple Silicon GPU via powermetrics.
func getAppleSiliconGPU() ([]*GPU, error) {
	// Check if we're on macOS
	if _, err := os.Stat("/usr/bin/powermetrics"); err != nil {
		return nil, err
	}

	// Attempt to run powermetrics with GPU metrics
	// This requires elevated privileges, so gracefully handle failure
	cmd := exec.Command("sudo", "powermetrics", "-n", "1", "-m", "gpu_power")
	output, err := cmd.Output()
	if err != nil {
		// If sudo fails or we don't have permissions, return nil gracefully
		return nil, err
	}

	// Parse powermetrics output for GPU info
	gpuMetrics := parseAppleSiliconMetrics(string(output))
	if len(gpuMetrics) > 0 {
		return gpuMetrics, nil
	}

	return nil, err
}

// parseAppleSiliconMetrics parses powermetrics output to extract GPU metrics.
func parseAppleSiliconMetrics(output string) []*GPU {
	// Apple Silicon GPU detection via powermetrics
	// Look for GPU power consumption lines and estimate utilization
	var gpus []*GPU

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "GPU Power") || strings.Contains(line, "gpu_power") {
			// Create a generic Apple Silicon GPU entry
			gpu := &GPU{
				Name:      "Apple Silicon GPU",
				Index:     0,
				TempC:     0, // powermetrics doesn't easily provide temp without special queries
				VRAMUsed:  0, // Unified memory, hard to determine GPU-specific usage
				VRAMTotal: 0,
			}

			// Try to extract power draw
			if strings.Contains(line, "mW") {
				// Parse power in milliwatts
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "mW" && i > 0 {
						if powerMW, err := strconv.ParseFloat(parts[i-1], 64); err == nil {
							gpu.PowerDraw = powerMW / 1000 // Convert mW to W
						}
					}
				}
			}

			gpus = append(gpus, gpu)
			break // Apple Silicon typically has one GPU
		}
	}

	return gpus
}
