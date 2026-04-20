package sysinfo

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	gopsnet "github.com/shirou/gopsutil/v4/net"
)

// InterfaceMetrics contains metrics for a single network interface.
type InterfaceMetrics struct {
	Name         string  `json:"name"`
	IP           string  `json:"ip"`
	State        string  `json:"state"`    // "up" or "down"
	BytesSent    uint64  `json:"bytesSent"`
	BytesRecv    uint64  `json:"bytesRecv"`
	ThroughputUp float64 `json:"throughputUp"`  // bytes per second
	ThroughputDn float64 `json:"throughputDn"`  // bytes per second
}

// WiFiMetrics contains WiFi connection information.
type WiFiMetrics struct {
	Connected bool   `json:"connected"`
	SSID      string `json:"ssid"`
	SignalDBm int    `json:"signalDBm"`
}

// NetworkMetrics contains network-related system information.
type NetworkMetrics struct {
	InternetUp bool               `json:"internetUp"` // Whether internet is available
	Interfaces []InterfaceMetrics `json:"interfaces"`
	WiFi       *WiFiMetrics       `json:"wifi"`
	UpdatedAt  time.Time          `json:"updatedAt"`
}

// NetworkCollector periodically collects network metrics.
type NetworkCollector struct {
	mu               sync.RWMutex
	metrics          *NetworkMetrics
	lastIOCounters   map[string]gopsnet.IOCountersStat // Track previous IOCounters for throughput calculation
	lastCollectTime  time.Time
	ticker           *time.Ticker
	stopCh           chan struct{}
	wg               sync.WaitGroup
}

// NewNetworkCollector creates a new network metrics collector.
func NewNetworkCollector(interval time.Duration) *NetworkCollector {
	return &NetworkCollector{
		metrics:        &NetworkMetrics{InternetUp: true, Interfaces: []InterfaceMetrics{}, WiFi: nil},
		lastIOCounters: make(map[string]gopsnet.IOCountersStat),
		ticker:         time.NewTicker(interval),
		stopCh:         make(chan struct{}),
	}
}

// Start begins collecting network metrics.
func (n *NetworkCollector) Start() error {
	// Collect immediately on start
	_ = n.collect()

	// Start background collector
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for {
			select {
			case <-n.ticker.C:
				_ = n.collect()
			case <-n.stopCh:
				return
			}
		}
	}()

	return nil
}

// Stop stops the collector.
func (n *NetworkCollector) Stop() {
	close(n.stopCh)
	n.ticker.Stop()
	n.wg.Wait()
}

// Get returns the latest network metrics.
func (n *NetworkCollector) Get() *NetworkMetrics {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.metrics == nil {
		return &NetworkMetrics{InternetUp: true}
	}

	// Return a copy to prevent external mutation
	metrics := *n.metrics
	return &metrics
}

// collect fetches the latest network metrics from the system.
func (n *NetworkCollector) collect() error {
	internetUp := isInternetUp()
	now := time.Now()

	// Collect interface metrics
	interfaces, err := gopsnet.Interfaces()
	if err != nil {
		interfaces = []gopsnet.InterfaceStat{}
	}

	// Collect IO counters
	ioCounters, err := gopsnet.IOCounters(true)
	if err != nil {
		ioCounters = []gopsnet.IOCountersStat{}
	}

	// Build interface metrics
	var ifaceMetrics []InterfaceMetrics
	timeDelta := n.lastCollectTime.Sub(now).Seconds()
	if timeDelta == 0 {
		timeDelta = 1 // Avoid division by zero
	}

	for _, iface := range interfaces {
		state := "down"
		// Check if interface is up by checking Flags
		for _, flag := range iface.Flags {
			if flag == "up" {
				state = "up"
				break
			}
		}

		// Find matching IO counter
		var ioCounter gopsnet.IOCountersStat
		for _, io := range ioCounters {
			if io.Name == iface.Name {
				ioCounter = io
				break
			}
		}

		// Calculate throughput
		var throughputUp, throughputDn float64
		if prev, ok := n.lastIOCounters[iface.Name]; ok {
			// Only calculate if we have a time delta
			if timeDelta > 0 {
				bytesSentDelta := int64(ioCounter.BytesSent) - int64(prev.BytesSent)
				bytesRecvDelta := int64(ioCounter.BytesRecv) - int64(prev.BytesRecv)
				if bytesSentDelta >= 0 {
					throughputUp = float64(bytesSentDelta) / timeDelta
				}
				if bytesRecvDelta >= 0 {
					throughputDn = float64(bytesRecvDelta) / timeDelta
				}
			}
		}

		// Get primary IP address
		ip := ""
		if len(iface.Addrs) > 0 {
			ip = iface.Addrs[0].Addr
		}

		ifaceMetrics = append(ifaceMetrics, InterfaceMetrics{
			Name:         iface.Name,
			IP:           ip,
			State:        state,
			BytesSent:    ioCounter.BytesSent,
			BytesRecv:    ioCounter.BytesRecv,
			ThroughputUp: throughputUp,
			ThroughputDn: throughputDn,
		})
	}

	// Collect WiFi metrics
	wifi := getWiFiMetrics()

	n.mu.Lock()
	defer n.mu.Unlock()

	n.metrics = &NetworkMetrics{
		InternetUp: internetUp,
		Interfaces: ifaceMetrics,
		WiFi:       wifi,
		UpdatedAt:  now,
	}

	// Update last collection time and IO counters
	n.lastCollectTime = now
	n.lastIOCounters = make(map[string]gopsnet.IOCountersStat)
	for _, io := range ioCounters {
		n.lastIOCounters[io.Name] = io
	}

	return nil
}

// isInternetUp checks if internet connectivity is available.
// It uses DNS resolution as a simple check (no external HTTP calls).
func isInternetUp() bool {
	// Try to resolve multiple DNS names to increase reliability
	hosts := []string{"8.8.8.8", "1.1.1.1", "208.67.222.222"} // Google DNS, Cloudflare, OpenDNS

	for _, host := range hosts {
		// Simple check: can we resolve this IP?
		conn, err := net.DialTimeout("tcp", host+":53", 2*time.Second)
		if err == nil {
			conn.Close()
			return true
		}
	}

	return false
}

// getWiFiMetrics detects WiFi connection information.
func getWiFiMetrics() *WiFiMetrics {
	// Try Linux first (iwconfig or iw)
	wifi := getWiFiLinux()
	if wifi != nil {
		return wifi
	}

	// Try macOS (airport)
	wifi = getWiFiMacOS()
	if wifi != nil {
		return wifi
	}

	return nil
}

// getWiFiLinux detects WiFi on Linux using iwconfig or iw.
func getWiFiLinux() *WiFiMetrics {
	// Try iwconfig first
	cmd := exec.Command("iwconfig")
	output, err := cmd.Output()
	if err == nil {
		return parseIwconfig(string(output))
	}

	// Try iw
	cmd = exec.Command("iw", "dev")
	output, err = cmd.Output()
	if err == nil {
		return parseIwLink(string(output))
	}

	return nil
}

// getWiFiMacOS detects WiFi on macOS using airport command.
func getWiFiMacOS() *WiFiMetrics {
	cmd := exec.Command("/System/Library/PrivateFrameworks/Apple80211.framework/Versions/A/Resources/airport", "-I")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseAirport(string(output))
}

// parseIwconfig parses iwconfig output for SSID and signal strength.
func parseIwconfig(output string) *WiFiMetrics {
	lines := strings.Split(output, "\n")
	var ssid string
	var signalDBm int

	for _, line := range lines {
		if strings.Contains(line, "SSID:") {
			// Extract SSID between quotes
			parts := strings.Split(line, "\"")
			if len(parts) >= 2 {
				ssid = parts[1]
			}
		}
		if strings.Contains(line, "Signal level=") {
			// Extract signal strength (format: "Signal level=-50 dBm")
			parts := strings.Fields(line)
			for i, part := range parts {
				if strings.Contains(part, "level=") {
					if i+1 < len(parts) {
						fmt.Sscanf(parts[i+1], "%d", &signalDBm)
					}
				}
			}
		}
	}

	if ssid != "" {
		return &WiFiMetrics{
			Connected: true,
			SSID:      ssid,
			SignalDBm: signalDBm,
		}
	}
	return nil
}

// parseIwLink parses iw dev output for WiFi information.
func parseIwLink(output string) *WiFiMetrics {
	lines := strings.Split(output, "\n")
	var ssid string
	var signalDBm int
	var connected bool

	for _, line := range lines {
		if strings.Contains(line, "SSID:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ssid = strings.Join(parts[1:], " ")
				connected = true
			}
		}
		if strings.Contains(line, "signal:") {
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "signal:" && i+1 < len(parts) {
					fmt.Sscanf(parts[i+1], "%d", &signalDBm)
				}
			}
		}
	}

	if connected && ssid != "" {
		return &WiFiMetrics{
			Connected: true,
			SSID:      ssid,
			SignalDBm: signalDBm,
		}
	}
	return nil
}

// parseAirport parses macOS airport command output.
func parseAirport(output string) *WiFiMetrics {
	lines := strings.Split(output, "\n")
	var ssid string
	var signalDBm int

	for _, line := range lines {
		if strings.Contains(line, "SSID:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				ssid = strings.TrimSpace(parts[1])
			}
		}
		if strings.Contains(line, "agrCtlRSSI:") {
			parts := strings.Fields(line)
			for i, part := range parts {
				if strings.Contains(part, "agrCtlRSSI:") && i+1 < len(parts) {
					fmt.Sscanf(parts[i+1], "%d", &signalDBm)
				}
			}
		}
	}

	if ssid != "" && ssid != "<unknown>" {
		return &WiFiMetrics{
			Connected: true,
			SSID:      ssid,
			SignalDBm: signalDBm,
		}
	}
	return nil
}
