package sysinfo

import (
	"net"
	"sync"
	"time"
)

// NetworkMetrics contains network-related system information.
type NetworkMetrics struct {
	InternetUp bool      `json:"internetUp"` // Whether internet is available
	UpdatedAt  time.Time `json:"updatedAt"`
}

// NetworkCollector periodically collects network metrics.
type NetworkCollector struct {
	mu      sync.RWMutex
	metrics *NetworkMetrics
	ticker  *time.Ticker
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewNetworkCollector creates a new network metrics collector.
func NewNetworkCollector(interval time.Duration) *NetworkCollector {
	return &NetworkCollector{
		metrics: &NetworkMetrics{InternetUp: true},
		ticker:  time.NewTicker(interval),
		stopCh:  make(chan struct{}),
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
	// Check internet connectivity by attempting to resolve a reliable hostname
	internetUp := isInternetUp()

	n.mu.Lock()
	defer n.mu.Unlock()

	n.metrics = &NetworkMetrics{
		InternetUp: internetUp,
		UpdatedAt:  time.Now(),
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
