package policy

import (
	"context"
	"fmt"
	"log"

	"github.com/acunningham-ship-it/agent-htop/internal/api"
	"github.com/acunningham-ship-it/agent-htop/internal/notify"
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
	// TODO: Implement via Paperclip API
	// This would call the agent wake/kill endpoint
	return fmt.Errorf("kill not yet implemented via Paperclip API")
}

// NoOpKillHandler is a kill handler that does nothing (for testing/non-Paperclip).
type NoOpKillHandler struct{}

// Kill does nothing.
func (h *NoOpKillHandler) Kill(agentID string) error {
	return nil
}

// DiscordAlertHandler sends alerts via Discord.
type DiscordAlertHandler struct {
	notifier *notify.Notifier
}

// NewDiscordAlertHandler creates a new Discord alert handler.
func NewDiscordAlertHandler(notifier *notify.Notifier) *DiscordAlertHandler {
	return &DiscordAlertHandler{notifier: notifier}
}

// Alert sends an alert via Discord.
func (h *DiscordAlertHandler) Alert(channel, title, message string) error {
	if h.notifier == nil {
		return fmt.Errorf("notifier not configured")
	}

	// TODO: Use the notifier to send the alert
	// For now, log it
	log.Printf("[DISCORD] %s: %s", title, message)
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
// If notifier is provided, Discord alerts are available.
func NewPolicyEngine(apiClient *api.Client, notifier *notify.Notifier) *Engine {
	var killHandler KillHandler
	if apiClient != nil {
		killHandler = NewPaperclipKillHandler(apiClient)
	} else {
		killHandler = &NoOpKillHandler{}
	}

	var alertHandler AlertHandler
	if notifier != nil {
		alertHandler = NewDiscordAlertHandler(notifier)
	} else {
		alertHandler = &NoOpAlertHandler{}
	}

	logHandler := &StandardLogHandler{}

	return NewEngine(killHandler, alertHandler, logHandler)
}
