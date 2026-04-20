package policy

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// Engine evaluates policies and executes their actions.
type Engine struct {
	mu sync.RWMutex

	policies []*Policy
	state    map[string]map[string]*PolicyState // [agentID][policyName]PolicyState

	// Action executors
	killHandler  KillHandler
	alertHandler AlertHandler
	logHandler   LogHandler

	// Event tracking
	eventsCh chan *PolicyEvent
	stopCh   chan struct{}

	// Timing
	dryRunDefaults map[string]time.Duration // policy name -> default dry-run duration
}

// KillHandler kills an agent session.
type KillHandler interface {
	Kill(agentID string) error
}

// AlertHandler sends an alert.
type AlertHandler interface {
	Alert(channel, title, message string) error
}

// LogHandler logs a message.
type LogHandler interface {
	Log(message string)
}

// NewEngine creates a new policy engine.
func NewEngine(killHandler KillHandler, alertHandler AlertHandler, logHandler LogHandler) *Engine {
	return &Engine{
		policies:       make([]*Policy, 0),
		state:          make(map[string]map[string]*PolicyState),
		killHandler:    killHandler,
		alertHandler:   alertHandler,
		logHandler:     logHandler,
		eventsCh:       make(chan *PolicyEvent, 100),
		stopCh:         make(chan struct{}),
		dryRunDefaults: make(map[string]time.Duration),
	}
}

// AddPolicy adds a policy to the engine.
func (e *Engine) AddPolicy(p *Policy) error {
	if p.Name == "" {
		return fmt.Errorf("policy name required")
	}
	if p.When == "" {
		return fmt.Errorf("policy when expression required")
	}
	if p.Action == "" {
		p.Action = ActionLog // Default to log
	}

	// Parse dry-run TTL
	ttl := 24 * time.Hour // Default 24h
	if p.DryRunTTL != "" {
		d, err := time.ParseDuration(p.DryRunTTL)
		if err != nil {
			return fmt.Errorf("invalid dry_run_ttl for policy %s: %w", p.Name, err)
		}
		ttl = d
	}

	e.mu.Lock()
	e.policies = append(e.policies, p)
	e.dryRunDefaults[p.Name] = ttl
	e.mu.Unlock()

	return nil
}

// Evaluate evaluates all policies for an agent against the given context.
// Returns the list of policy events that were triggered.
func (e *Engine) Evaluate(ctx *SessionContext) []*PolicyEvent {
	if ctx == nil || ctx.AgentID == "" {
		return nil
	}

	var events []*PolicyEvent

	e.mu.RLock()
	policies := e.policies
	e.mu.RUnlock()

	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		event := e.evaluatePolicy(policy, ctx)
		if event != nil {
			events = append(events, event)
			e.sendEvent(event)
		}
	}

	return events
}

// evaluatePolicy evaluates a single policy.
func (e *Engine) evaluatePolicy(policy *Policy, ctx *SessionContext) *PolicyEvent {
	// Initialize agent state if needed
	e.mu.Lock()
	if e.state[ctx.AgentID] == nil {
		e.state[ctx.AgentID] = make(map[string]*PolicyState)
	}
	policyState := e.state[ctx.AgentID][policy.Name]
	if policyState == nil {
		policyState = &PolicyState{
			EnabledAt: time.Now(),
		}
		e.state[ctx.AgentID][policy.Name] = policyState
	}
	e.mu.Unlock()

	// Evaluate the when expression
	evaluator := NewExprEvaluator(policy.When)
	result, err := evaluator.Evaluate(ctx)

	event := &PolicyEvent{
		PolicyName: policy.Name,
		AgentID:    ctx.AgentID,
		AgentName:  ctx.AgentName,
		Action:     policy.Action,
		Reason:     policy.Reason,
		Timestamp:  time.Now(),
	}

	if err != nil {
		event.Error = err.Error()
		event.Message = fmt.Sprintf("Evaluation failed: %v", err)
		return event
	}

	// If condition not met, no event
	if !result {
		return nil
	}

	// Check if we're in dry-run mode
	e.mu.RLock()
	ttl := e.dryRunDefaults[policy.Name]
	e.mu.RUnlock()

	now := time.Now()
	if policyState.DryRunUntil.IsZero() {
		// First time this policy is triggered: set dry-run period
		policyState.DryRunUntil = now.Add(ttl)
	}

	isDryRun := now.Before(policyState.DryRunUntil)
	event.DryRun = isDryRun

	// Update state
	e.mu.Lock()
	policyState.TriggeredCount++
	policyState.LastTriggeredAt = now
	e.mu.Unlock()

	// Execute the action
	event.Message = e.executeAction(policy, ctx, isDryRun)

	return event
}

// executeAction executes the policy's action.
func (e *Engine) executeAction(policy *Policy, ctx *SessionContext, isDryRun bool) string {
	action := policy.Action

	// Check capability
	if action == ActionKill && !ctx.SupportsKill {
		msg := fmt.Sprintf("Kill action skipped: %s runtime does not support kill", ctx.RuntimeType)
		log.Printf("[POLICY] %s: %s", policy.Name, msg)
		return msg
	}

	if action == ActionPause && !ctx.SupportsPause {
		msg := fmt.Sprintf("Pause action skipped: %s runtime does not support pause", ctx.RuntimeType)
		log.Printf("[POLICY] %s: %s", policy.Name, msg)
		return msg
	}

	// Execute or dry-run
	switch action {
	case ActionKill:
		if isDryRun {
			msg := fmt.Sprintf("[DRY RUN] Would kill %s: %s", ctx.AgentID, policy.Reason)
			log.Printf("[POLICY] %s: %s", policy.Name, msg)
			return msg
		}
		if err := e.killHandler.Kill(ctx.AgentID); err != nil {
			msg := fmt.Sprintf("Kill failed: %v", err)
			log.Printf("[POLICY] %s: ERROR %s", policy.Name, msg)
			return msg
		}
		msg := fmt.Sprintf("Killed %s: %s", ctx.AgentID, policy.Reason)
		log.Printf("[POLICY] %s: %s", policy.Name, msg)
		return msg

	case ActionPause:
		if isDryRun {
			msg := fmt.Sprintf("[DRY RUN] Would pause %s: %s", ctx.AgentID, policy.Reason)
			log.Printf("[POLICY] %s: %s", policy.Name, msg)
			return msg
		}
		msg := fmt.Sprintf("Paused %s: %s", ctx.AgentID, policy.Reason)
		log.Printf("[POLICY] %s: %s", policy.Name, msg)
		return msg

	case ActionAlert:
		msg := fmt.Sprintf("Alert for %s: %s", ctx.AgentID, policy.Reason)
		if isDryRun {
			msg = fmt.Sprintf("[DRY RUN] Would alert: %s", msg)
		} else {
			channel := policy.Channel
			if channel == "" {
				channel = "discord"
			}
			if err := e.alertHandler.Alert(channel, policy.Name, msg); err != nil {
				log.Printf("[POLICY] %s: Alert failed: %v", policy.Name, err)
			}
		}
		log.Printf("[POLICY] %s: %s", policy.Name, msg)
		return msg

	case ActionLog:
		msg := fmt.Sprintf("Log for %s: %s", ctx.AgentID, policy.Reason)
		if isDryRun {
			msg = fmt.Sprintf("[DRY RUN] %s", msg)
		}
		log.Printf("[POLICY] %s: %s", policy.Name, msg)
		return msg

	default:
		return fmt.Sprintf("Unknown action: %s", action)
	}
}

// Events returns the policy event channel.
func (e *Engine) Events() <-chan *PolicyEvent {
	return e.eventsCh
}

// sendEvent sends a policy event (non-blocking).
func (e *Engine) sendEvent(event *PolicyEvent) {
	select {
	case e.eventsCh <- event:
	case <-e.stopCh:
	}
}

// GetStats returns statistics for a policy.
func (e *Engine) GetStats(agentID, policyName string) *PolicyState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if state, ok := e.state[agentID]; ok {
		if pState, ok := state[policyName]; ok {
			return pState
		}
	}
	return nil
}

// Stop stops the engine and closes the event channel.
func (e *Engine) Stop() {
	close(e.stopCh)
	close(e.eventsCh)
}

// Snapshot returns JSON-serializable state of all policies and their metrics.
func (e *Engine) Snapshot() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snapshot := map[string]interface{}{
		"policies": len(e.policies),
		"agents":   make(map[string]interface{}),
	}

	agents := snapshot["agents"].(map[string]interface{})

	for agentID, policies := range e.state {
		agentMap := make(map[string]interface{})
		for policyName, state := range policies {
			agentMap[policyName] = map[string]interface{}{
				"enabled_at":        state.EnabledAt,
				"dry_run_until":     state.DryRunUntil,
				"triggered_count":   state.TriggeredCount,
				"last_triggered_at": state.LastTriggeredAt,
			}
		}
		agents[agentID] = agentMap
	}

	return snapshot
}
