package health

import (
	"fmt"
	"time"
)

// Alert represents a health alert.
type Alert struct {
	Rule      string    `json:"rule"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	Since     time.Time `json:"since"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FilesystemMetrics represents a single filesystem's metrics.
type FilesystemMetrics interface {
	GetMountPoint() string
	GetUsedPercent() float64
}

// SystemMetrics interface for system health checks.
type SystemMetrics interface {
	GetCPULoad1Min() float64
	GetCPULogicalCores() int
	GetMemAvailableMB() uint64
	GetMemTotalMB() uint64
	GetMemUsedPercent() float64
	GetMemSwapUsedPercent() float64
	GetMemSwapTotalMB() uint64
	GetDiskUsedPercent() float64
	GetDiskFreeGB() uint64
	GetFilesystemMetrics() []FilesystemMetrics
	GetGPUTempC() float64
	GetGPUAvailable() bool
	GetNetworkInternetUp() bool
}

// AlertRule defines the interface for alert rules.
type AlertRule interface {
	Name() string
	Evaluate(metrics SystemMetrics) *Alert
}

// OOMRiskRule detects out-of-memory risks.
type OOMRiskRule struct {
	lastAlertTime time.Time // Track when alert was last triggered
}

func NewOOMRiskRule() *OOMRiskRule {
	return &OOMRiskRule{}
}

func (r *OOMRiskRule) Name() string {
	return "oom_risk"
}

func (r *OOMRiskRule) Evaluate(metrics SystemMetrics) *Alert {
	now := time.Now()
	availableMB := metrics.GetMemAvailableMB()
	totalMB := metrics.GetMemTotalMB()
	swapUsed := metrics.GetMemSwapUsedPercent()

	// Critical: Available RAM < 200MB
	if availableMB < 200 {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "critical",
			Message:   fmt.Sprintf("Available RAM critically low: %dMB / %dMB (%.1f%% free)", availableMB, totalMB, float64(availableMB)/float64(totalMB)*100),
			Since:     now,
			UpdatedAt: now,
		}
	}

	// High: Swap usage > 80%
	if swapUsed > 80.0 {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "high",
			Message:   fmt.Sprintf("Swap usage critically high: %.1f%%", swapUsed),
			Since:     now,
			UpdatedAt: now,
		}
	}

	// High: Available RAM 200-500MB
	if availableMB < 500 {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "high",
			Message:   fmt.Sprintf("Available RAM low: %dMB / %dMB (%.1f%% free)", availableMB, totalMB, float64(availableMB)/float64(totalMB)*100),
			Since:     now,
			UpdatedAt: now,
		}
	}

	return nil
}

// LoadSpikeRule detects abnormal CPU load spikes.
type LoadSpikeRule struct{}

func NewLoadSpikeRule() *LoadSpikeRule {
	return &LoadSpikeRule{}
}

func (r *LoadSpikeRule) Name() string {
	return "load_spike"
}

func (r *LoadSpikeRule) Evaluate(metrics SystemMetrics) *Alert {
	now := time.Now()
	load1Min := metrics.GetCPULoad1Min()
	cores := metrics.GetCPULogicalCores()
	threshold := float64(cores) * 2.0

	// Medium: 1-min load > 2× core count
	if load1Min > threshold {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "medium",
			Message:   fmt.Sprintf("CPU load spike detected: %.2f (threshold: %.2f)", load1Min, threshold),
			Since:     now,
			UpdatedAt: now,
		}
	}

	return nil
}

// AlertEvaluator evaluates system metrics for alerts.
type AlertEvaluator struct {
	rules        []AlertRule
	activeAlerts map[string]*Alert // rule name -> alert
}

// DiskUsageRule detects disk space issues.
type DiskUsageRule struct{}

func NewDiskUsageRule() *DiskUsageRule {
	return &DiskUsageRule{}
}

func (r *DiskUsageRule) Name() string {
	return "disk_full"
}

func (r *DiskUsageRule) Evaluate(metrics SystemMetrics) *Alert {
	now := time.Now()
	usedPercent := metrics.GetDiskUsedPercent()

	// Critical: > 95%
	if usedPercent > 95 {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "critical",
			Message:   fmt.Sprintf("Disk critically full: %.1f%% used (pausing write-heavy agents)", usedPercent),
			Since:     now,
			UpdatedAt: now,
		}
	}

	// High: > 90%
	if usedPercent > 90 {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "high",
			Message:   fmt.Sprintf("Disk space low: %.1f%% used", usedPercent),
			Since:     now,
			UpdatedAt: now,
		}
	}

	return nil
}

// GPUThermalRule detects GPU overheating.
type GPUThermalRule struct{}

func NewGPUThermalRule() *GPUThermalRule {
	return &GPUThermalRule{}
}

func (r *GPUThermalRule) Name() string {
	return "gpu_thermal"
}

func (r *GPUThermalRule) Evaluate(metrics SystemMetrics) *Alert {
	// Only evaluate if GPU is available
	if !metrics.GetGPUAvailable() {
		return nil
	}

	now := time.Now()
	tempC := metrics.GetGPUTempC()

	// Critical: > 95°C
	if tempC > 95 {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "critical",
			Message:   fmt.Sprintf("GPU critical: %.1f°C (threshold: 95°C)", tempC),
			Since:     now,
			UpdatedAt: now,
		}
	}

	// High: > 85°C
	if tempC > 85 {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "high",
			Message:   fmt.Sprintf("GPU hot: %.1f°C (threshold: 85°C)", tempC),
			Since:     now,
			UpdatedAt: now,
		}
	}

	return nil
}

// DiskUsagePerFilesystemRule detects disk space issues on individual filesystems.
type DiskUsagePerFilesystemRule struct{}

func NewDiskUsagePerFilesystemRule() *DiskUsagePerFilesystemRule {
	return &DiskUsagePerFilesystemRule{}
}

func (r *DiskUsagePerFilesystemRule) Name() string {
	return "filesystem_full"
}

func (r *DiskUsagePerFilesystemRule) Evaluate(metrics SystemMetrics) *Alert {
	now := time.Now()
	filesystems := metrics.GetFilesystemMetrics()

	// Check each filesystem for high usage
	for _, fs := range filesystems {
		usedPercent := fs.GetUsedPercent()
		mountPoint := fs.GetMountPoint()

		// Critical: > 95%
		if usedPercent > 95 {
			return &Alert{
				Rule:      r.Name(),
				Severity:  "critical",
				Message:   fmt.Sprintf("Filesystem %s critically full: %.1f%% (pausing agents)", mountPoint, usedPercent),
				Since:     now,
				UpdatedAt: now,
			}
		}

		// High: > 90%
		if usedPercent > 90 {
			return &Alert{
				Rule:      r.Name(),
				Severity:  "high",
				Message:   fmt.Sprintf("Filesystem %s space low: %.1f%% used", mountPoint, usedPercent),
				Since:     now,
				UpdatedAt: now,
			}
		}
	}

	return nil
}

// NetworkDownRule detects network connectivity loss.
type NetworkDownRule struct{}

func NewNetworkDownRule() *NetworkDownRule {
	return &NetworkDownRule{}
}

func (r *NetworkDownRule) Name() string {
	return "network_down"
}

func (r *NetworkDownRule) Evaluate(metrics SystemMetrics) *Alert {
	now := time.Now()

	// Critical: Internet is down
	if !metrics.GetNetworkInternetUp() {
		return &Alert{
			Rule:      r.Name(),
			Severity:  "critical",
			Message:   "Internet connectivity lost (pausing upload agents)",
			Since:     now,
			UpdatedAt: now,
		}
	}

	return nil
}

// NewAlertEvaluator creates a new alert evaluator.
func NewAlertEvaluator() *AlertEvaluator {
	ae := &AlertEvaluator{
		rules:        make([]AlertRule, 0),
		activeAlerts: make(map[string]*Alert),
	}
	// Register default rules
	ae.RegisterRule(NewOOMRiskRule())
	ae.RegisterRule(NewLoadSpikeRule())
	ae.RegisterRule(NewDiskUsageRule())
	ae.RegisterRule(NewDiskUsagePerFilesystemRule())
	ae.RegisterRule(NewGPUThermalRule())
	ae.RegisterRule(NewNetworkDownRule())
	return ae
}

// RegisterRule adds a new alert rule.
func (ae *AlertEvaluator) RegisterRule(rule AlertRule) {
	ae.rules = append(ae.rules, rule)
}

// Evaluate evaluates system metrics and updates active alerts.
func (ae *AlertEvaluator) Evaluate(metrics SystemMetrics) {
	newAlerts := make(map[string]*Alert)

	for _, rule := range ae.rules {
		alert := rule.Evaluate(metrics)
		if alert != nil {
			// Preserve the "since" timestamp from previous alert if rule still active
			if prev, exists := ae.activeAlerts[rule.Name()]; exists {
				alert.Since = prev.Since
			}
			newAlerts[rule.Name()] = alert
		}
	}

	ae.activeAlerts = newAlerts
}

// GetActiveAlerts returns the current active alerts as a slice.
func (ae *AlertEvaluator) GetActiveAlerts() []*Alert {
	alerts := make([]*Alert, 0, len(ae.activeAlerts))
	for _, alert := range ae.activeAlerts {
		alerts = append(alerts, alert)
	}
	return alerts
}

// HasCriticalAlerts checks if any critical alerts are active.
func (ae *AlertEvaluator) HasCriticalAlerts() bool {
	for _, alert := range ae.activeAlerts {
		if alert.Severity == "critical" {
			return true
		}
	}
	return false
}

// HasHighAlerts checks if any high or critical alerts are active.
func (ae *AlertEvaluator) HasHighAlerts() bool {
	for _, alert := range ae.activeAlerts {
		if alert.Severity == "high" || alert.Severity == "critical" {
			return true
		}
	}
	return false
}
