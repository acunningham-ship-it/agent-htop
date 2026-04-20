package sysinfo

import (
	"sync"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/health"
)

// SystemState represents the complete system state snapshot.
// This is the unified response for get_system_state() MCP tool and --full JSON output.
type SystemState struct {
	SchemaVersion int64       `json:"schemaVersion"` // 1 for this version
	Timestamp     time.Time   `json:"timestamp"`
	Host          HostState   `json:"host"`
	Processes     ProcessList `json:"processes"`
}

// HostState contains host-level system metrics.
type HostState struct {
	CPU       CPUMetrics      `json:"cpu"`
	Memory    MemoryMetrics   `json:"memory"`
	Disk      DiskMetrics     `json:"disk"`
	GPU       GPUMetrics      `json:"gpu"`
	Network   NetworkMetrics  `json:"network"`
	Uptime    UptimeInfo      `json:"uptime"`
	Alerts    []*health.Alert `json:"alerts,omitempty"` // Health alerts (omitted if empty)
	UpdatedAt time.Time       `json:"updatedAt"`
}

// UptimeInfo contains uptime information.
type UptimeInfo struct {
	Seconds   uint64    `json:"seconds"`   // System uptime in seconds
	UpdatedAt time.Time `json:"updatedAt"`
}

// Implement health.SystemMetrics interface for SystemState
func (s *SystemState) GetCPULoad1Min() float64 {
	return s.Host.CPU.Load1Min
}

func (s *SystemState) GetCPULogicalCores() int {
	return s.Host.CPU.LogicalCores
}

func (s *SystemState) GetMemAvailableMB() uint64 {
	return s.Host.Memory.AvailableMB
}

func (s *SystemState) GetMemTotalMB() uint64 {
	return s.Host.Memory.TotalMB
}

func (s *SystemState) GetMemUsedPercent() float64 {
	return s.Host.Memory.UsedPercent
}

func (s *SystemState) GetMemSwapUsedPercent() float64 {
	return s.Host.Memory.SwapUsedPercent
}

func (s *SystemState) GetMemSwapTotalMB() uint64 {
	return s.Host.Memory.SwapTotalMB
}

func (s *SystemState) GetDiskUsedPercent() float64 {
	return s.Host.Disk.UsedPercent
}

func (s *SystemState) GetDiskFreeGB() uint64 {
	return s.Host.Disk.FreeGB
}

func (s *SystemState) GetGPUTempC() float64 {
	return s.Host.GPU.TempC
}

func (s *SystemState) GetGPUAvailable() bool {
	return s.Host.GPU.Available
}

func (s *SystemState) GetNetworkInternetUp() bool {
	return s.Host.Network.InternetUp
}

// StateCollector aggregates all system collectors and provides cached snapshots.
type StateCollector struct {
	mu               sync.RWMutex
	cpuCollector     *CPUCollector
	memCollector     *MemoryCollector
	diskCollector    *DiskCollector
	gpuCollector     *GPUCollector
	networkCollector *NetworkCollector
	procCollector    *ProcessCollector
	alertEvaluator   *health.AlertEvaluator
	lastSnapshot     *SystemState
	lastSnapshotTime time.Time
	cacheTTL         time.Duration // How long to cache before re-collecting
}

// NewStateCollector creates a new unified state collector.
func NewStateCollector(interval time.Duration, cacheTTL time.Duration) *StateCollector {
	return &StateCollector{
		cpuCollector:     NewCPUCollector(interval),
		memCollector:     NewMemoryCollector(interval),
		diskCollector:    NewDiskCollector(interval),
		gpuCollector:     NewGPUCollector(interval),
		networkCollector: NewNetworkCollector(interval),
		procCollector:    NewProcessCollector(interval),
		alertEvaluator:   health.NewAlertEvaluator(),
		cacheTTL:         cacheTTL,
	}
}

// Start begins all underlying collectors.
func (sc *StateCollector) Start() error {
	if err := sc.cpuCollector.Start(); err != nil {
		return err
	}
	if err := sc.memCollector.Start(); err != nil {
		return err
	}
	if err := sc.diskCollector.Start(); err != nil {
		return err
	}
	if err := sc.gpuCollector.Start(); err != nil {
		return err
	}
	if err := sc.networkCollector.Start(); err != nil {
		return err
	}
	if err := sc.procCollector.Start(); err != nil {
		return err
	}
	return nil
}

// Stop stops all underlying collectors.
func (sc *StateCollector) Stop() {
	sc.cpuCollector.Stop()
	sc.memCollector.Stop()
	sc.diskCollector.Stop()
	sc.gpuCollector.Stop()
	sc.networkCollector.Stop()
	sc.procCollector.Stop()
}

// GetSnapshot returns a cached or fresh system state snapshot.
// If the cache is fresh (< cacheTTL old), returns cached version.
// Otherwise, collects fresh data from all collectors.
func (sc *StateCollector) GetSnapshot() *SystemState {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()

	// Return cached snapshot if fresh
	if sc.lastSnapshot != nil && now.Sub(sc.lastSnapshotTime) < sc.cacheTTL {
		return sc.cloneSnapshot(sc.lastSnapshot)
	}

	// Collect fresh data
	uptime := uint64(0)
	if u, err := GetUptime(); err == nil {
		uptime = u
	}

	// Build the snapshot
	hostState := HostState{
		CPU:       *sc.cpuCollector.Get(),
		Memory:    *sc.memCollector.Get(),
		Disk:      *sc.diskCollector.Get(),
		GPU:       *sc.gpuCollector.Get(),
		Network:   *sc.networkCollector.Get(),
		Uptime:    UptimeInfo{Seconds: uptime, UpdatedAt: now},
		UpdatedAt: now,
	}

	// Evaluate alerts and add them to the snapshot
	snapshot := &SystemState{
		SchemaVersion: 1,
		Timestamp:     now,
		Host:          hostState,
		Processes:     *sc.procCollector.Get(),
	}

	// Evaluate alerts against the snapshot
	sc.alertEvaluator.Evaluate(snapshot)
	snapshot.Host.Alerts = sc.alertEvaluator.GetActiveAlerts()

	// Cache the snapshot
	sc.lastSnapshot = snapshot
	sc.lastSnapshotTime = now

	return sc.cloneSnapshot(snapshot)
}

// cloneSnapshot returns a deep copy of the snapshot to prevent external mutations.
func (sc *StateCollector) cloneSnapshot(state *SystemState) *SystemState {
	if state == nil {
		return nil
	}

	// Clone CPU metrics
	cpuCopy := state.Host.CPU
	if cpuCopy.PercentPerCore != nil {
		percentCopy := make([]float64, len(cpuCopy.PercentPerCore))
		copy(percentCopy, cpuCopy.PercentPerCore)
		cpuCopy.PercentPerCore = percentCopy
	}

	// Clone process list
	procListCopy := &ProcessList{
		Processes: make([]*ProcessInfo, len(state.Processes.Processes)),
		UpdatedAt: state.Processes.UpdatedAt,
	}
	for i, proc := range state.Processes.Processes {
		procCopy := *proc
		procListCopy.Processes[i] = &procCopy
	}

	// Clone alerts
	var alertsCopy []*health.Alert
	if state.Host.Alerts != nil {
		alertsCopy = make([]*health.Alert, len(state.Host.Alerts))
		copy(alertsCopy, state.Host.Alerts)
	}

	return &SystemState{
		SchemaVersion: state.SchemaVersion,
		Timestamp:     state.Timestamp,
		Host: HostState{
			CPU:        cpuCopy,
			Memory:     state.Host.Memory,
			Uptime:     state.Host.Uptime,
			Alerts:     alertsCopy,
			UpdatedAt:  state.Host.UpdatedAt,
		},
		Processes: *procListCopy,
	}
}
