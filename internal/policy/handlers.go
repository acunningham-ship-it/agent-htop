package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/acunningham-ship-it/agent-htop/internal/api"
)

// PaperclipKillHandler kills sessions via Paperclip API.
type PaperclipKillHandler struct {
	client *api.Client
}

// NewPaperclipKillHandler creates a new kill handler for Paperclip.
func NewPaperclipKillHandler(client *api.Client) *PaperclipKillHandler {
	return &PaperclipKillHandler{client: client}
}

// Kill kills an agent session via the Paperclip API.
func (h *PaperclipKillHandler) Kill(agentID string) error {
	if h.client == nil {
		return fmt.Errorf("API client not configured")
	}
	return h.client.TerminateAgent(context.Background(), agentID)
}

// NoOpKillHandler is a kill handler that does nothing (for testing/non-Paperclip).
type NoOpKillHandler struct{}

// Kill does nothing.
func (h *NoOpKillHandler) Kill(agentID string) error {
	return nil
}

// DiscordAlertHandler sends alerts via Discord.
type DiscordAlertHandler struct {
	webhookURL string
}

// NewDiscordAlertHandler creates a new Discord alert handler.
func NewDiscordAlertHandler(webhookURL string) *DiscordAlertHandler {
	return &DiscordAlertHandler{webhookURL: webhookURL}
}

// Alert sends an alert via Discord.
func (h *DiscordAlertHandler) Alert(channel, title, message string) error {
	if h.webhookURL == "" {
		// No webhook configured, just log
		log.Printf("[POLICY] %s: %s", title, message)
		return nil
	}

	// Send to Discord
	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       title,
				"description": message,
				"color":       15105570, // Orange color for policy alerts
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := http.Post(h.webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send alert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}

	return nil
}

// NoOpAlertHandler is an alert handler that does nothing.
type NoOpAlertHandler struct{}

// Alert does nothing.
func (h *NoOpAlertHandler) Alert(channel, title, message string) error {
	return nil
}

// LogHandler logs policy events.
type StandardLogHandler struct{}

// Log logs a message.
func (h *StandardLogHandler) Log(message string) {
	log.Printf("[POLICY] %s", message)
}

// NewPolicyEngine creates a fully-wired policy engine.
// If apiClient is provided, Paperclip kill actions are available.
// If webhookURL is provided, Discord alerts are available.
func NewPolicyEngine(apiClient *api.Client, webhookURL string) *Engine {
	var killHandler KillHandler
	if apiClient != nil {
		killHandler = NewPaperclipKillHandler(apiClient)
	} else {
		killHandler = &NoOpKillHandler{}
	}

	var alertHandler AlertHandler
	if webhookURL != "" {
		alertHandler = NewDiscordAlertHandler(webhookURL)
	} else {
		alertHandler = &NoOpAlertHandler{}
	}

	logHandler := &StandardLogHandler{}

	return NewEngine(killHandler, alertHandler, logHandler)
}
