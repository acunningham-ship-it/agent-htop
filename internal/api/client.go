package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client wraps the Paperclip API with caching.
type Client struct {
	baseURL      string
	httpClient   *http.Client
	agentCache   *AgentCache
	issueCacheMu sync.RWMutex
	issueCache   map[string]*Issue
}

// Agent represents a Paperclip agent.
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Issue represents a Paperclip issue (used for fallback run metadata).
type Issue struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	AssigneeAgentID string  `json:"assigneeAgentId"`
	CreatedAt     string    `json:"createdAt"`
	UpdatedAt     string    `json:"updatedAt"`
}

// NewClient creates a new Paperclip API client.
// baseURL should be like "http://localhost:3101"
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		agentCache: NewAgentCache(),
		issueCache: make(map[string]*Issue),
	}
}

// GetAgentName returns the friendly name for an agent, with caching.
func (c *Client) GetAgentName(ctx context.Context, agentID string) (string, error) {
	return c.agentCache.GetName(ctx, agentID, c)
}

// RefreshAgentCache pre-fetches and caches agent names for a company to avoid repeated API calls.
func (c *Client) RefreshAgentCache(ctx context.Context, companyID string) error {
	return c.agentCache.RefreshIfStale(ctx, []string{}, c, companyID)
}

// GetAgent fetches a single agent from the API.
func (c *Client) GetAgent(ctx context.Context, agentID string) (*Agent, error) {
	url := fmt.Sprintf("%s/api/agents/%s", c.baseURL, agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	var agent Agent
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		return nil, fmt.Errorf("failed to decode agent: %w", err)
	}

	return &agent, nil
}

// ListAgents fetches agents for a company.
func (c *Client) ListAgents(ctx context.Context, companyID string) ([]*Agent, error) {
	url := fmt.Sprintf("%s/api/companies/%s/agents", c.baseURL, companyID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	var agents []*Agent
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, fmt.Errorf("failed to decode agents: %w", err)
	}

	return agents, nil
}

// GetIssue fetches a single issue (fallback for missing logs).
func (c *Client) GetIssue(ctx context.Context, issueID string) (*Issue, error) {
	// Check cache
	c.issueCacheMu.RLock()
	if issue, ok := c.issueCache[issueID]; ok {
		c.issueCacheMu.RUnlock()
		return issue, nil
	}
	c.issueCacheMu.RUnlock()

	url := fmt.Sprintf("%s/api/issues/%s", c.baseURL, issueID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, fmt.Errorf("failed to decode issue: %w", err)
	}

	// Cache it
	c.issueCacheMu.Lock()
	c.issueCache[issueID] = &issue
	c.issueCacheMu.Unlock()

	return &issue, nil
}

// Health checks if Paperclip API is reachable.
func (c *Client) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/health", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("API returned %d", resp.StatusCode)
	}

	return nil
}

// InterruptAgent sends an interrupt signal to an agent's current execution.
func (c *Client) InterruptAgent(ctx context.Context, agentID string) error {
	url := fmt.Sprintf("%s/api/agents/%s/interrupt", c.baseURL, agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("API unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to interrupt agent: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// TerminateAgent terminates an agent.
func (c *Client) TerminateAgent(ctx context.Context, agentID string) error {
	url := fmt.Sprintf("%s/api/agents/%s/terminate", c.baseURL, agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("API unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to terminate agent: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// PauseAgent pauses an agent.
func (c *Client) PauseAgent(ctx context.Context, agentID string) error {
	url := fmt.Sprintf("%s/api/agents/%s/pause", c.baseURL, agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("API unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to pause agent: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ResumeAgent resumes a paused agent.
func (c *Client) ResumeAgent(ctx context.Context, agentID string) error {
	url := fmt.Sprintf("%s/api/agents/%s/resume", c.baseURL, agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("API unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to resume agent: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
