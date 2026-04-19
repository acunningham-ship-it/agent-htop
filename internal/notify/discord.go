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

	"github.com/acunningham-ship-it/agent-htop/internal/anomaly"
)

// AlertLevel filters which anomalies to send.
type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "info"
	AlertLevelWarning  AlertLevel = "warn"
	AlertLevelCritical AlertLevel = "critical"
)

// Notifier sends anomaly events to Discord.
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
		webhookURL:   webhookURL,
		client:       &http.Client{Timeout: 10 * time.Second},
		lastAlert:    make(map[string]time.Time),
		cooldown:     10 * time.Minute, // Max 1 alert per agent per 10 min
		dashboardURL: dashboardURL,
		minLevel:     minLevel,
		stopCh:       make(chan struct{}),
	}
}

// Start begins listening to anomaly events and sending alerts.
func (n *Notifier) Start(ctx context.Context, detector *anomaly.Detector) {
	if n.webhookURL == "" {
		log.Printf("[discord] Discord webhook not configured (AGENT_HTOP_DISCORD_WEBHOOK), alerts disabled")
		return
	}

	n.wg.Add(1)
	go n.run(ctx, detector)
}

// run is the main event loop for consuming anomaly events.
func (n *Notifier) run(ctx context.Context, detector *anomaly.Detector) {
	defer n.wg.Done()

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

// Stop stops the notifier.
func (n *Notifier) Stop() {
	close(n.stopCh)
	n.wg.Wait()
}
