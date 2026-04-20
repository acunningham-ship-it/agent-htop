package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Parser reads NDJSON log files and converts them to AgentRun structs.
type Parser struct {
	agentID   string
	companyID string
	runID     string
}

// NewParser creates a new parser for a given agent run.
// runID should be the log filename (without .ndjson extension).
func NewParser(companyID, agentID, runID string) *Parser {
	return &Parser{
		agentID:   agentID,
		companyID: companyID,
		runID:     runID,
	}
}

// Parse reads an NDJSON log file and returns a completed AgentRun.
func (p *Parser) Parse(r io.Reader) (*AgentRun, error) {
	run := &AgentRun{
		Runtime:    RuntimePaperclip,
		RunID:      p.runID,
		AgentID:    p.agentID,
		CompanyID:  p.companyID,
		ModelUsage: make(map[string]*ModelMetrics),
		ToolCalls:  make([]*ToolCall, 0),
	}

	scanner := bufio.NewScanner(r)
	// Set larger buffer to handle large log lines (default 64KB is too small)
	buffer := make([]byte, 0, 1024*1024) // 1MB
	scanner.Buffer(buffer, 1024*1024)     // 1MB max token size
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse outer wrapper
		var logLine LogLine
		if err := json.Unmarshal([]byte(line), &logLine); err != nil {
			return nil, fmt.Errorf("failed to parse log line: %w", err)
		}

		// Parse inner chunk (skip non-JSON lines like plain text output)
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(logLine.Chunk), &event); err != nil {
			// Skip non-JSON chunks (plain text output from process)
			continue
		}

		// Route by event type
		eventType, ok := event["type"].(string)
		if !ok {
			continue
		}

		switch eventType {
		case "system":
			p.handleSystem(event, run, logLine.Ts)
		case "assistant":
			p.handleAssistant(event, run)
		case "user":
			p.handleUser(event, run)
		case "rate_limit_event":
			p.handleRateLimit(event, run)
		case "result":
			p.handleResult(event, run, logLine.Ts)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return run, nil
}

// handleSystem processes system/init events.
func (p *Parser) handleSystem(event map[string]interface{}, run *AgentRun, ts string) {
	subtype, ok := event["subtype"].(string)
	if !ok || subtype != "init" {
		return
	}

	// Extract model
	if model, ok := event["model"].(string); ok {
		run.Model = model
	}

	// Extract session_id
	if sessionID, ok := event["session_id"].(string); ok {
		run.SessionID = sessionID
	}

	// Extract cwd
	if cwd, ok := event["cwd"].(string); ok {
		run.WorkDir = cwd
	}

	// Extract permission mode
	if pm, ok := event["permissionMode"].(string); ok {
		run.Permissions = pm
	}

	// Parse start time
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		run.StartTime = t
	}
}

// handleAssistant processes assistant events (token usage and tool calls).
func (p *Parser) handleAssistant(event map[string]interface{}, run *AgentRun) {
	message, ok := event["message"].(map[string]interface{})
	if !ok {
		return
	}

	// Extract usage from message.usage
	if usage, ok := message["usage"].(map[string]interface{}); ok {
		if input, ok := usage["input_tokens"].(float64); ok {
			run.TotalInputTokens += int(input)
		}
		if output, ok := usage["output_tokens"].(float64); ok {
			run.TotalOutputTokens += int(output)
		}
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
							ID:    id,
							Input: make(map[string]interface{}),
						}
						if name, ok := blockMap["name"].(string); ok {
							toolCall.Name = name
						}
						if input, ok := blockMap["input"].(map[string]interface{}); ok {
							toolCall.Input = input
						}
						run.ToolCalls = append(run.ToolCalls, toolCall)
					}
				}
			}
		}
	}
}

// handleUser processes user events (tool results).
func (p *Parser) handleUser(event map[string]interface{}, run *AgentRun) {
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
								break
							}
						}
					}
				}
			}
		}
	}
}

// handleRateLimit processes rate_limit_event events.
func (p *Parser) handleRateLimit(event map[string]interface{}, run *AgentRun) {
	if rli, ok := event["rate_limit_info"].(map[string]interface{}); ok {
		if status, ok := rli["status"].(string); ok {
			run.RateLimitStatus = status
		}
		if resetsAt, ok := rli["resetsAt"].(float64); ok {
			run.RateLimitResetsAt = int64(resetsAt)
		}
	}
}

// handleResult processes result events (completion summary).
func (p *Parser) handleResult(event map[string]interface{}, run *AgentRun, ts string) {
	subtype, ok := event["subtype"].(string)
	if !ok {
		return
	}

	run.Status = subtype // success | error | cancelled
	run.IsError = subtype != "success"

	// Parse end time
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		run.EndTime = t
	}

	// Extract result summary
	if result, ok := event["result"].(string); ok {
		run.Result = result
	}

	// Extract terminal reason
	if tr, ok := event["terminal_reason"].(string); ok {
		run.TerminalReason = tr
	}

	// Extract duration
	if duration, ok := event["duration_ms"].(float64); ok {
		run.DurationMS = int64(duration)
	}

	// Extract total cost
	if cost, ok := event["total_cost_usd"].(float64); ok {
		run.TotalCostUSD = cost
	}

	// Extract num turns
	if turns, ok := event["num_turns"].(float64); ok {
		run.NumTurns = int(turns)
	}

	// Extract aggregate usage
	if usage, ok := event["usage"].(map[string]interface{}); ok {
		if input, ok := usage["input_tokens"].(float64); ok {
			run.TotalInputTokens = int(input)
		}
		if output, ok := usage["output_tokens"].(float64); ok {
			run.TotalOutputTokens = int(output)
		}
	}

	// Extract per-model usage
	if modelUsage, ok := event["modelUsage"].(map[string]interface{}); ok {
		for modelName, modelDataRaw := range modelUsage {
			if modelData, ok := modelDataRaw.(map[string]interface{}); ok {
				metrics := &ModelMetrics{}
				if v, ok := modelData["inputTokens"].(float64); ok {
					metrics.InputTokens = int(v)
				}
				if v, ok := modelData["outputTokens"].(float64); ok {
					metrics.OutputTokens = int(v)
				}
				if v, ok := modelData["cacheCreationInputTokens"].(float64); ok {
					metrics.CacheCreationTokens = int(v)
				}
				if v, ok := modelData["cacheReadInputTokens"].(float64); ok {
					metrics.CacheReadTokens = int(v)
				}
				if v, ok := modelData["costUSD"].(float64); ok {
					metrics.CostUSD = v
				}
				if v, ok := modelData["contextWindow"].(float64); ok {
					metrics.ContextWindow = int(v)
				}
				if v, ok := modelData["maxOutputTokens"].(float64); ok {
					metrics.MaxOutputTokens = int(v)
				}
				run.ModelUsage[modelName] = metrics
			}
		}
	}
}
