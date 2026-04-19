package api

import (
	"context"
	"sync"
	"time"
)

// AgentCache caches agent names with TTL to reduce API calls.
type AgentCache struct {
	mu        sync.RWMutex
	names     map[string]string // agentID -> name
	lastSync  time.Time
	syncTTL   time.Duration
}

// NewAgentCache creates a new agent cache with a 5-minute TTL.
func NewAgentCache() *AgentCache {
	return &AgentCache{
		names:   make(map[string]string),
		syncTTL: 5 * time.Minute,
	}
}

// GetName retrieves an agent name, fetching from API if not cached.
func (ac *AgentCache) GetName(ctx context.Context, agentID string, client *Client) (string, error) {
	ac.mu.RLock()
	if name, exists := ac.names[agentID]; exists {
		ac.mu.RUnlock()
		return name, nil
	}
	ac.mu.RUnlock()

	// Cache miss: fetch from API
	agent, err := client.GetAgent(ctx, agentID)
	if err != nil {
		// Fall back to UUID display
		return agentID, err
	}

	// Store in cache
	ac.mu.Lock()
	ac.names[agentID] = agent.Name
	ac.mu.Unlock()

	return agent.Name, nil
}

// RefreshIfStale refreshes agent names for a list of agents if cache is stale.
func (ac *AgentCache) RefreshIfStale(ctx context.Context, agentIDs []string, client *Client, companyID string) error {
	ac.mu.RLock()
	stale := time.Since(ac.lastSync) >= ac.syncTTL
	ac.mu.RUnlock()

	if !stale && len(ac.names) > 0 {
		return nil // Cache is fresh
	}

	// Fetch all agents for the company
	agents, err := client.ListAgents(ctx, companyID)
	if err != nil {
		return err
	}

	// Update cache
	ac.mu.Lock()
	defer ac.mu.Unlock()
	for _, agent := range agents {
		ac.names[agent.ID] = agent.Name
	}
	ac.lastSync = time.Now()

	return nil
}

// SetName manually sets a name in the cache (useful for testing).
func (ac *AgentCache) SetName(agentID, name string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.names[agentID] = name
}

// Clear clears the cache.
func (ac *AgentCache) Clear() {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.names = make(map[string]string)
	ac.lastSync = time.Time{}
}
