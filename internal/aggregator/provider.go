package aggregator

import "context"

// AgentNamer resolves a UUID agent ID to a human-readable name.
// Returns ("", err) when the name is not known.
type AgentNamer interface {
	GetAgentName(ctx context.Context, agentID string) (string, error)
	RefreshAgentCache(ctx context.Context, companyID string) error
}

// NullAgentNamer is a no-op implementation for runtimes without an API.
type NullAgentNamer struct{}

func (NullAgentNamer) GetAgentName(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (NullAgentNamer) RefreshAgentCache(_ context.Context, _ string) error {
	return nil
}
