package aggregator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/acunningham-ship-it/agent-htop/internal/api"
	"github.com/acunningham-ship-it/agent-htop/internal/watcher"
)

func TestAggregatorLoadsRealLogs(t *testing.T) {
	// Use a real company from the test environment
	companyID := "29f5f5bf-ecff-4120-829d-ccd8c42ca7c2" // HamTek Dev

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	logDir := filepath.Join(home, ".paperclip", "instances", "default", "data", "run-logs")

	// Check if log directory exists
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Skip("Paperclip log directory not found, skipping integration test")
	}

	// Create aggregator
	apiClient := api.NewClient("http://localhost:3101")
	w, err := watcher.NewWatcher(logDir)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	agg := NewAggregator(companyID, logDir, apiClient, w)
	ctx := context.Background()

	// Load existing logs
	if err := agg.Start(ctx); err != nil {
		t.Fatalf("Failed to start aggregator: %v", err)
	}

	// Get fleet state
	state := agg.GetFleetState()

	// Verify we have agents loaded
	if len(state.Agents) == 0 {
		t.Errorf("Expected agents to be loaded, got 0")
	} else {
		t.Logf("Loaded %d agents from real logs", len(state.Agents))
		for _, agent := range state.Agents {
			t.Logf("  - %s (ID: %s, Status: %s, Cost: $%.4f)",
				agent.AgentName, agent.AgentID, agent.Status, agent.TotalCostUSD)
		}
	}

	agg.Stop()
}
