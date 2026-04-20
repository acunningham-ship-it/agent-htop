package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// ClaudeParser reads JSONL log files from Claude Code sessions.
// Claude Code logs are simpler than Paperclip (no outer wrapper, direct JSON events).
type ClaudeParser struct {
	sessionID string
	projectPath string
}

// NewClaudeParser creates a parser for a Claude Code session.
// sessionID is the .jsonl filename (without extension).
// projectPath is the full path to the project (e.g., ~/.claude/projects/-home-armani-projects-agent-htop/)
func NewClaudeParser(sessionID, projectPath string) *ClaudeParser {
	return &ClaudeParser{
		sessionID: sessionID,
		projectPath: projectPath,
	}
}

// Parse reads a Claude Code JSONL session file and returns an AgentRun.
func (p *ClaudeParser) Parse(r io.Reader) (*AgentRun, error) {
	run := &AgentRun{
		Runtime:    RuntimeClaude,
		RunID:      p.sessionID,
		SessionID:  p.sessionID,
		ModelUsage: make(map[string]*ModelMetrics),
		ToolCalls:  make([]*ToolCall, 0),
	}

	scanner := bufio.NewScanner(r)
	firstEventProcessed := false
	var lastEventTime time.Time
	toolInvocations := make(map[string]time.Time) // Map tool_use_id to invocation time

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse Claude Code event (direct JSON, no wrapper)
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Skip malformed lines
			continue
		}

		eventType, ok := event["type"].(string)
		if !ok {
			continue
		}

		// Parse timestamp for timing
		var eventTime time.Time
		if ts, ok := event["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				eventTime = t
				lastEventTime = t
				if !firstEventProcessed {
					run.StartTime = t
					firstEventProcessed = true
				}
			}
		}

		// Extract common fields
		if sessionID, ok := event["sessionId"].(string); ok && run.SessionID == "" {
			run.SessionID = sessionID
		}
		if cwd, ok := event["cwd"].(string); ok && run.WorkDir == "" {
			run.WorkDir = cwd
		}
		// Note: "version" in events is Claude Code harness version, not model.
		// Extract model from first assistant event.

		// Route by event type
		switch eventType {
		case "assistant":
			p.handleAssistant(event, run, eventTime, toolInvocations)
		case "user":
			p.handleUser(event, run, eventTime, toolInvocations)
		case "queue-operation":
			// Metadata only
			p.handleQueueOperation(event, run)
		case "last-prompt":
			// Final prompt; marks end of session
			run.TerminalReason = "completed"
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	// Set end time from last event
	if !lastEventTime.IsZero() {
		run.EndTime = lastEventTime
	}

	// Calculate duration
	if !run.StartTime.IsZero() && !run.EndTime.IsZero() {
		run.DurationMS = int64(run.EndTime.Sub(run.StartTime).Milliseconds())
	}

	// If terminal reason wasn't set, assume success (no explicit error)
	if run.TerminalReason == "" {
		run.TerminalReason = "completed"
	}

	// If status wasn't explicitly set, derive from terminal reason
	if run.Status == "" {
		if run.TerminalReason == "completed" {
			run.Status = "success"
		} else if run.TerminalReason == "error" {
			run.Status = "error"
		} else if run.TerminalReason == "cancelled" {
			run.Status = "cancelled"
		} else {
			run.Status = "success" // default
		}
	}

	return run, nil
}

// handleAssistant processes assistant events (Claude responses with token usage).
func (p *ClaudeParser) handleAssistant(event map[string]interface{}, run *AgentRun, eventTime time.Time, toolInvocations map[string]time.Time) {
	message, ok := event["message"].(map[string]interface{})
	if !ok {
		return
	}

	// Extract model from first assistant event
	if run.Model == "" {
		if model, ok := message["model"].(string); ok {
			run.Model = model
		}
	}

	// Extract usage
	if usage, ok := message["usage"].(map[string]interface{}); ok {
		if input, ok := usage["input_tokens"].(float64); ok {
			run.TotalInputTokens += int(input)
		}
		if output, ok := usage["output_tokens"].(float64); ok {
			run.TotalOutputTokens += int(output)
		}
		// Cache tokens (useful for future analysis)
		// Ignored for now, but available
	}

	// Count tool calls in content
	if content, ok := message["content"].([]interface{}); ok {
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, ok := blockMap["type"].(string); ok && blockType == "tool_use" {
					run.NumToolCalls++

					// Extract tool call details
					if id, ok := blockMap["id"].(string); ok {
						toolCall := &ToolCall{
							ID:        id,
							Input:     make(map[string]interface{}),
							StartTime: eventTime,
						}
						if name, ok := blockMap["name"].(string); ok {
							toolCall.Name = name
						}
						if input, ok := blockMap["input"].(map[string]interface{}); ok {
							toolCall.Input = input
						}
						run.ToolCalls = append(run.ToolCalls, toolCall)
						// Record invocation time for later matching with result
						toolInvocations[id] = eventTime
					}
				}
			}
		}
	}

	// Track turns (count assistant messages)
	run.NumTurns++
}

// handleUser processes user events (inputs and tool results).
func (p *ClaudeParser) handleUser(event map[string]interface{}, run *AgentRun, eventTime time.Time, toolInvocations map[string]time.Time) {
	message, ok := event["message"].(map[string]interface{})
	if !ok {
		return
	}

	// Extract tool results from content
	if content, ok := message["content"].([]interface{}); ok {
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, ok := blockMap["type"].(string); ok && blockType == "tool_result" {
					// Link result to tool call
					if toolUseID, ok := blockMap["tool_use_id"].(string); ok {
						result := ""
						if resultContent, ok := blockMap["content"].(string); ok {
							result = resultContent
						}
						isError := false
						if isErr, ok := blockMap["is_error"].(bool); ok {
							isError = isErr
						}

						// Find matching tool call and update it
						for _, toolCall := range run.ToolCalls {
							if toolCall.ID == toolUseID {
								toolCall.Result = result
								toolCall.IsError = isError
								toolCall.EndTime = eventTime
								break
							}
						}
					}
				}
			}
		}
	}
}

// handleQueueOperation processes queue operations (mainly metadata).
func (p *ClaudeParser) handleQueueOperation(event map[string]interface{}, run *AgentRun) {
	// Extract any prompt/session info if needed
	// Not critical for metrics, but useful for debugging
}
