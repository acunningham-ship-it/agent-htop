package main

import (
	"context"
	"fmt"
	"log"

	"github.com/acunningham-ship-it/agent-htop/internal/api"
	"github.com/acunningham-ship-it/agent-htop/internal/config"
	"github.com/acunningham-ship-it/agent-htop/internal/notify"
	"github.com/acunningham-ship-it/agent-htop/internal/policy"
)

// setupPolicies creates and configures the policy engine from config.
// Starts listening to policy events in the background.
func setupPolicies(ctx context.Context, cfg *config.Config, apiClient *api.Client, notifier *notify.Notifier) (*policy.Engine, error) {
	// Create the policy engine with all handlers
	engine := policy.NewPolicyEngine(apiClient, cfg.DiscordWebhook)

	// Load policies from config
	for _, rawPolicy := range cfg.Policies {
		if rawPolicy.Name == "" {
			continue // Skip empty policies
		}

		// Default enabled to true if not explicitly set
		enabled := rawPolicy.Enabled
		if rawPolicy.Enabled == false && rawPolicy.Name != "" {
			// If explicitly disabled in TOML, it will be false
			// But we need to detect if it was not set at all
			// TOML unmarshaling defaults to false for bools, so we can't distinguish
			// For now, default to enabled=true for all policies in config
			enabled = true
		}

		p := &policy.Policy{
			Name:        rawPolicy.Name,
			Description: rawPolicy.Description,
			When:        rawPolicy.When,
			Action:      policy.ActionType(rawPolicy.Action),
			Reason:      rawPolicy.Reason,
			Channel:     rawPolicy.Channel,
			DryRunTTL:   rawPolicy.DryRunTTL,
			Enabled:     enabled,
		}

		if err := engine.AddPolicy(p); err != nil {
			return nil, fmt.Errorf("failed to add policy %s: %w", p.Name, err)
		}

		log.Printf("[policy] Loaded policy: %s (%s: %s)", p.Name, p.When, p.Action)
	}

	// Start background goroutine to log policy events
	go handlePolicyEvents(ctx, engine)

	return engine, nil
}

// handlePolicyEvents listens to policy events and logs them.
func handlePolicyEvents(ctx context.Context, engine *policy.Engine) {
	for {
		select {
		case event := <-engine.Events():
			if event == nil {
				return
			}

			// Log the policy event
			level := "info"
			if event.Error != "" {
				level = "error"
			} else if event.DryRun {
				level = "debug"
			}

			logMsg := fmt.Sprintf("[policy] %s (%s): %s - %s", event.PolicyName, event.AgentName, event.Action, event.Message)
			if event.Error != "" {
				logMsg = fmt.Sprintf("[policy] %s (%s): ERROR - %s", event.PolicyName, event.AgentName, event.Error)
			}

			switch level {
			case "error":
				log.Printf("ERROR: %s", logMsg)
			case "debug":
				log.Printf("DEBUG: %s", logMsg)
			default:
				log.Printf("%s", logMsg)
			}

		case <-ctx.Done():
			return
		}
	}
}
