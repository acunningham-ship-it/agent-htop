package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/aggregator"
	"github.com/acunningham-ship-it/agent-htop/internal/anomaly"
	"github.com/acunningham-ship-it/agent-htop/internal/health"
)

// AlertLevel filters which anomalies to send.
type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "info"
	AlertLevelWarning  AlertLevel = "warn"
	AlertLevelCritical AlertLevel = "critical"
)

// Notifier sends anomaly events and health alerts to Discord.
type Notifier struct {
	webhookURL string
	client     *http.Client

	// Rate limiting: per-agent cooldown to avoid spam
	mu          sync.Mutex
	lastAlert   map[string]time.Time // agentID -> last alert time
	cooldown    time.Duration
	dashboardURL string

	// Alert level filtering
	minLevel AlertLevel

	// Health alert tracking (deduplication)
	sentHealthAlerts map[string]bool // alertRule -> sent (to prevent duplicates)

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewNotifier creates a new Discord notifier.
// webhookURL can be empty (in which case the notifier will be a no-op).
func NewNotifier(webhookURL string, dashboardURL string, minLevel AlertLevel) *Notifier {
	if minLevel == "" {
		minLevel = AlertLevelInfo
	}

	return &Notifier{
		webhookURL:       webhookURL,
		client:           &http.Client{Timeout: 10 * time.Second},
		lastAlert:        make(map[string]time.Time),
		cooldown:         10 * time.Minute, // Max 1 alert per agent per 10 min
		dashboardURL:     dashboardURL,
		minLevel:         minLevel,
		sentHealthAlerts: make(map[string]bool),
		stopCh:           make(chan struct{}),
	}
}

// Start begins listening to anomaly events and health alerts, sending them to Discord.
func (n *Notifier) Start(ctx context.Context, detector *anomaly.Detector) {
	n.StartWithAggregator(ctx, nil, detector)
}

// StartWithAggregator begins listening to anomaly events and health alerts.
func (n *Notifier) StartWithAggregator(ctx context.Context, agg *aggregator.Aggregator, detector *anomaly.Detector) {
	if n.webhookURL == "" {
		log.Printf("[discord] Discord webhook not configured (AGENT_HTOP_DISCORD_WEBHOOK), alerts disabled")
		return
	}

	n.wg.Add(1)
	go n.run(ctx, agg, detector)
}

// run is the main event loop for consuming anomaly events and monitoring health alerts.
func (n *Notifier) run(ctx context.Context, agg *aggregator.Aggregator, detector *anomaly.Detector) {
	defer n.wg.Done()

	// Start health alert monitor if aggregator is provided
	if agg != nil {
		n.wg.Add(1)
		go n.monitorHealthAlerts(ctx, agg)
	}

	// Anomaly event loop
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.stopCh:
			return
		case event := <-detector.Events():
			if event == nil {
				return
			}

			// Check alert level
			if !n.shouldAlert(event.Severity) {
				continue
			}

			// Check rate limit
			if !n.isRateLimited(event.AgentID) {
				if err := n.sendAlert(event); err != nil {
					log.Printf("[discord] Failed to send alert: %v", err)
				}
			}
		}
	}
}

// shouldAlert checks if the event's severity meets the minimum alert level.
func (n *Notifier) shouldAlert(severity string) bool {
	switch n.minLevel {
	case AlertLevelInfo:
		return true // Info includes all levels
	case AlertLevelWarning:
		return severity == "warning" || severity == "critical"
	case AlertLevelCritical:
		return severity == "critical"
	default:
		return true
	}
}

// isRateLimited checks if we've alerted this agent recently.
func (n *Notifier) isRateLimited(agentID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	lastTime, exists := n.lastAlert[agentID]
	if !exists {
		n.lastAlert[agentID] = time.Now()
		return false
	}

	if time.Since(lastTime) < n.cooldown {
		return true
	}

	n.lastAlert[agentID] = time.Now()
	return false
}

// sendAlert posts a formatted Discord message for the anomaly.
func (n *Notifier) sendAlert(event *anomaly.AnomalyEvent) error {
	emoji := n.emojiForAnomalyType(event.AnomalyType)
	color := n.colorForSeverity(event.Severity)

	// Build a Discord embed
	embed := map[string]interface{}{
		"title":       fmt.Sprintf("%s %s", emoji, event.AgentName),
		"description": event.Message,
		"color":       color,
		"fields": []map[string]interface{}{
			{
				"name":   "Type",
				"value":  string(event.AnomalyType),
				"inline": true,
			},
			{
				"name":   "Severity",
				"value":  event.Severity,
				"inline": true,
			},
			{
				"name":   "Detected",
				"value":  event.DetectedAt.Format(time.RFC3339),
				"inline": false,
			},
		},
		"timestamp": event.DetectedAt.Format(time.RFC3339),
	}

	// Add dashboard link if available
	if n.dashboardURL != "" {
		embed["url"] = n.dashboardURL
	}

	payload := map[string]interface{}{
		"content": "",
		"embeds":  []map[string]interface{}{embed},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", n.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}

	return nil
}

// emojiForAnomalyType returns an appropriate emoji for the anomaly type.
func (n *Notifier) emojiForAnomalyType(anomalyType anomaly.AnomalyType) string {
	switch anomalyType {
	case anomaly.HighSpend:
		return "💸"
	case anomaly.ErrorStreak:
		return "⚠️"
	case anomaly.CostAnomaly:
		return "🚨"
	default:
		return "🔔"
	}
}

// colorForSeverity returns a Discord embed color for the severity level.
func (n *Notifier) colorForSeverity(severity string) int {
	switch severity {
	case "critical":
		return 0xFF0000 // Red
	case "warning":
		return 0xFFA500 // Orange
	default:
		return 0x4A90E2 // Blue
	}
}

// monitorHealthAlerts polls the aggregator for health alerts and sends new ones to Discord.
func (n *Notifier) monitorHealthAlerts(ctx context.Context, agg *aggregator.Aggregator) {
	defer n.wg.Done()

	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-n.stopCh:
			return
		case <-ticker.C:
			alerts := agg.GetActiveAlerts()
			for _, alert := range alerts {
				// Only send critical and high alerts
				if alert.Severity != "critical" && alert.Severity != "high" {
					continue
				}

				// Skip if already sent (deduplication)
				alertKey := alert.Rule + ":" + alert.Severity
				n.mu.Lock()
				alreadySent := n.sentHealthAlerts[alertKey]
				n.mu.Unlock()

				if alreadySent {
					continue
				}

				// Send the alert
				if err := n.sendHealthAlert(alert); err != nil {
					log.Printf("[discord] Failed to send health alert: %v", err)
				} else {
					// Mark as sent
					n.mu.Lock()
					n.sentHealthAlerts[alertKey] = true
					n.mu.Unlock()
				}
			}

			// Clean up alerts that are no longer active
			activeKeys := make(map[string]bool)
			for _, alert := range alerts {
				alertKey := alert.Rule + ":" + alert.Severity
				activeKeys[alertKey] = true
			}

			n.mu.Lock()
			for key := range n.sentHealthAlerts {
				if !activeKeys[key] {
					delete(n.sentHealthAlerts, key)
				}
			}
			n.mu.Unlock()
		}
	}
}

// sendHealthAlert posts a formatted Discord message for the health alert.
func (n *Notifier) sendHealthAlert(alert *health.Alert) error {
	// Choose emoji and color based on severity
	var emoji string
	var color int
	switch alert.Severity {
	case "critical":
		emoji = "🚨"
		color = 0xFF0000 // Red
	case "high":
		emoji = "⚠️"
		color = 0xFFA500 // Orange
	default:
		emoji = "ℹ️"
		color = 0x4A90E2 // Blue
	}

	// Build Discord embed
	embed := map[string]interface{}{
		"title":       fmt.Sprintf("%s System Health Alert", emoji),
		"description": alert.Message,
		"color":       color,
		"fields": []map[string]interface{}{
			{
				"name":   "Alert Rule",
				"value":  alert.Rule,
				"inline": true,
			},
			{
				"name":   "Severity",
				"value":  alert.Severity,
				"inline": true,
			},
			{
				"name":   "Started",
				"value":  alert.Since.Format(time.RFC3339),
				"inline": false,
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Add dashboard link if available
	if n.dashboardURL != "" {
		embed["url"] = n.dashboardURL
	}

	payload := map[string]interface{}{
		"content": "",
		"embeds":  []map[string]interface{}{embed},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", n.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}

	log.Printf("[discord] Sent health alert for %s (severity: %s)", alert.Rule, alert.Severity)
	return nil
}

// Stop stops the notifier.
func (n *Notifier) Stop() {
	close(n.stopCh)
	n.wg.Wait()
}
