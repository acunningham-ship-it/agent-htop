package parser

import (
	"strings"
	"testing"
	"time"
)

func TestParseNDJSON(t *testing.T) {
	// Sample NDJSON log data from schema
	// Note: chunk field is a JSON object, not a string
	sampleLog := `{"ts":"2026-04-19T17:05:27.699Z","stream":"stdout","chunk":{"type":"system","subtype":"init","cwd":"/tmp/test","session_id":"2b2a89f8-e5d5-4a7c-8b42-7cd168ad43cb","uuid":"test-uuid","model":"claude-sonnet-4-6","claude_code_version":"2.1.97","permissionMode":"bypassPermissions","fast_mode_state":"off","apiKeySource":"none","output_style":"default","tools":[],"mcp_servers":[],"slash_commands":[],"agents":[],"skills":[],"plugins":[]}}
{"ts":"2026-04-19T17:05:30.000Z","stream":"stdout","chunk":{"type":"assistant","session_id":"2b2a89f8-e5d5-4a7c-8b42-7cd168ad43cb","uuid":"asst-1","message":{"model":"claude-sonnet-4-6","id":"msg_01","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":100,"output_tokens":50},"stop_reason":"tool_use"}}}
{"ts":"2026-04-19T17:05:35.000Z","stream":"stdout","chunk":{"type":"user","session_id":"2b2a89f8-e5d5-4a7c-8b42-7cd168ad43cb","uuid":"user-1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"file1.txt","is_error":false}]}}}
{"ts":"2026-04-19T17:05:40.000Z","stream":"stdout","chunk":{"type":"rate_limit_event","session_id":"2b2a89f8-e5d5-4a7c-8b42-7cd168ad43cb","uuid":"rle-1","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","resetsAt":1776632400}}}
{"ts":"2026-04-19T17:05:45.000Z","stream":"stdout","chunk":{"type":"result","subtype":"success","session_id":"2b2a89f8-e5d5-4a7c-8b42-7cd168ad43cb","uuid":"res-1","is_error":false,"result":"Task completed","duration_ms":18000,"duration_api_ms":15000,"num_turns":2,"stop_reason":"end_turn","total_cost_usd":0.001,"terminal_reason":"completed","fast_mode_state":"off","usage":{"input_tokens":100,"output_tokens":50},"modelUsage":{"claude-sonnet-4-6":{"inputTokens":100,"outputTokens":50,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.001,"contextWindow":200000,"maxOutputTokens":32000}}}}
`

	parser := NewParser("test-company", "test-agent", "test-run-id")
	run, err := parser.Parse(strings.NewReader(sampleLog))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Validate basic fields
	if run.RunID != "test-run-id" {
		t.Errorf("RunID: got %q, want %q", run.RunID, "test-run-id")
	}
	if run.AgentID != "test-agent" {
		t.Errorf("AgentID: got %q, want %q", run.AgentID, "test-agent")
	}
	if run.Model != "claude-sonnet-4-6" {
		t.Errorf("Model: got %q, want %q", run.Model, "claude-sonnet-4-6")
	}
	if run.Status != "success" {
		t.Errorf("Status: got %q, want %q", run.Status, "success")
	}
	if run.TotalCostUSD != 0.001 {
		t.Errorf("TotalCostUSD: got %f, want %f", run.TotalCostUSD, 0.001)
	}
	if run.DurationMS != 18000 {
		t.Errorf("DurationMS: got %d, want %d", run.DurationMS, 18000)
	}
	if run.NumTurns != 2 {
		t.Errorf("NumTurns: got %d, want %d", run.NumTurns, 2)
	}

	// Validate token usage
	if run.TotalInputTokens != 100 {
		t.Errorf("TotalInputTokens: got %d, want %d", run.TotalInputTokens, 100)
	}
	if run.TotalOutputTokens != 50 {
		t.Errorf("TotalOutputTokens: got %d, want %d", run.TotalOutputTokens, 50)
	}

	// Validate tool calls
	if run.NumToolCalls != 1 {
		t.Errorf("NumToolCalls: got %d, want %d", run.NumToolCalls, 1)
	}
	if len(run.ToolCalls) != 1 {
		t.Errorf("ToolCalls length: got %d, want %d", len(run.ToolCalls), 1)
	}

	if run.ToolCalls[0].ID != "toolu_01" {
		t.Errorf("ToolCall ID: got %q, want %q", run.ToolCalls[0].ID, "toolu_01")
	}
	if run.ToolCalls[0].Name != "Bash" {
		t.Errorf("ToolCall Name: got %q, want %q", run.ToolCalls[0].Name, "Bash")
	}
	if run.ToolCalls[0].Result != "file1.txt" {
		t.Errorf("ToolCall Result: got %q, want %q", run.ToolCalls[0].Result, "file1.txt")
	}
	if run.ToolCalls[0].IsError != false {
		t.Errorf("ToolCall IsError: got %v, want %v", run.ToolCalls[0].IsError, false)
	}

	// Validate model usage
	if _, ok := run.ModelUsage["claude-sonnet-4-6"]; !ok {
		t.Errorf("ModelUsage missing claude-sonnet-4-6")
	}

	// Validate rate limit
	if run.RateLimitStatus != "allowed" {
		t.Errorf("RateLimitStatus: got %q, want %q", run.RateLimitStatus, "allowed")
	}
	if run.RateLimitResetsAt != 1776632400 {
		t.Errorf("RateLimitResetsAt: got %d, want %d", run.RateLimitResetsAt, 1776632400)
	}

	// Validate timestamps
	expectedStart := time.Date(2026, 4, 19, 17, 5, 27, 699000000, time.UTC)
	if !run.StartTime.Equal(expectedStart) {
		t.Errorf("StartTime: got %v, want %v", run.StartTime, expectedStart)
	}

	expectedEnd := time.Date(2026, 4, 19, 17, 5, 45, 0, time.UTC)
	if !run.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime: got %v, want %v", run.EndTime, expectedEnd)
	}

	// Validate runtime
	if run.Runtime != RuntimePaperclip {
		t.Errorf("Runtime: got %v, want %v", run.Runtime, RuntimePaperclip)
	}
}

func TestClaudeParseJSONL(t *testing.T) {
	// Sample JSONL log data from Claude Code session
	sampleLog := `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-04-19T18:00:00.000Z","sessionId":"07081b17-a403-4572-90cf-df9390fcacc7","content":"Test session started"}
{"type":"assistant","timestamp":"2026-04-19T18:00:05.000Z","sessionId":"07081b17-a403-4572-90cf-df9390fcacc7","uuid":"asst-1","message":{"model":"claude-haiku-4-5-20251001","id":"msg_01","type":"message","role":"assistant","content":[{"type":"tool_use","id":"tool_01","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":120,"output_tokens":60}},"cwd":"/home/armani/projects/agent-htop","version":"2.1.91"}
{"type":"user","timestamp":"2026-04-19T18:00:10.000Z","sessionId":"07081b17-a403-4572-90cf-df9390fcacc7","uuid":"user-1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_01","content":"file1.txt\nfile2.txt","is_error":false}]}}
{"type":"assistant","timestamp":"2026-04-19T18:00:15.000Z","sessionId":"07081b17-a403-4572-90cf-df9390fcacc7","uuid":"asst-2","message":{"model":"claude-haiku-4-5-20251001","id":"msg_02","type":"message","role":"assistant","content":[{"type":"text","text":"Found 2 files"}],"usage":{"input_tokens":140,"output_tokens":40}},"cwd":"/home/armani/projects/agent-htop","version":"2.1.91"}
{"type":"last-prompt","lastPrompt":"done","sessionId":"07081b17-a403-4572-90cf-df9390fcacc7","timestamp":"2026-04-19T18:00:20.000Z"}
`

	parser := NewClaudeParser("07081b17-a403-4572-90cf-df9390fcacc7", "/home/armani/.claude/projects/-test/")
	run, err := parser.Parse(strings.NewReader(sampleLog))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Validate basic fields
	if run.RunID != "07081b17-a403-4572-90cf-df9390fcacc7" {
		t.Errorf("RunID: got %q, want %q", run.RunID, "07081b17-a403-4572-90cf-df9390fcacc7")
	}
	if run.SessionID != "07081b17-a403-4572-90cf-df9390fcacc7" {
		t.Errorf("SessionID: got %q, want %q", run.SessionID, "07081b17-a403-4572-90cf-df9390fcacc7")
	}
	if run.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("Model: got %q, want %q", run.Model, "claude-haiku-4-5-20251001")
	}
	if run.Status != "success" {
		t.Errorf("Status: got %q, want %q", run.Status, "success")
	}
	if run.WorkDir != "/home/armani/projects/agent-htop" {
		t.Errorf("WorkDir: got %q, want %q", run.WorkDir, "/home/armani/projects/agent-htop")
	}

	// Validate token usage (sum of all assistant events)
	if run.TotalInputTokens != 260 { // 120 + 140
		t.Errorf("TotalInputTokens: got %d, want %d", run.TotalInputTokens, 260)
	}
	if run.TotalOutputTokens != 100 { // 60 + 40
		t.Errorf("TotalOutputTokens: got %d, want %d", run.TotalOutputTokens, 100)
	}

	// Validate turns (count of assistant messages)
	if run.NumTurns != 2 {
		t.Errorf("NumTurns: got %d, want %d", run.NumTurns, 2)
	}

	// Validate tool calls
	if run.NumToolCalls != 1 {
		t.Errorf("NumToolCalls: got %d, want %d", run.NumToolCalls, 1)
	}
	if len(run.ToolCalls) != 1 {
		t.Errorf("ToolCalls length: got %d, want %d", len(run.ToolCalls), 1)
	}
	if run.ToolCalls[0].Name != "Bash" {
		t.Errorf("ToolCall Name: got %q, want %q", run.ToolCalls[0].Name, "Bash")
	}
	if run.ToolCalls[0].Result != "file1.txt\nfile2.txt" {
		t.Errorf("ToolCall Result: got %q, want %q", run.ToolCalls[0].Result, "file1.txt\nfile2.txt")
	}

	// Validate timestamps
	expectedStart := time.Date(2026, 4, 19, 18, 0, 0, 0, time.UTC)
	if !run.StartTime.Equal(expectedStart) {
		t.Errorf("StartTime: got %v, want %v", run.StartTime, expectedStart)
	}

	expectedEnd := time.Date(2026, 4, 19, 18, 0, 20, 0, time.UTC)
	if !run.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime: got %v, want %v", run.EndTime, expectedEnd)
	}

	// Validate duration
	expectedDuration := int64(20000) // 20 seconds in ms
	if run.DurationMS != expectedDuration {
		t.Errorf("DurationMS: got %d, want %d", run.DurationMS, expectedDuration)
	}

	// Validate runtime
	if run.Runtime != RuntimeClaude {
		t.Errorf("Runtime: got %v, want %v", run.Runtime, RuntimeClaude)
	}
}

func TestParseNDJSON_ErrorResult(t *testing.T) {
	// Test Paperclip log with error result
	sampleLog := `{"ts":"2026-04-19T19:00:00.000Z","stream":"stdout","chunk":{"type":"system","subtype":"init","cwd":"/tmp/error-test","session_id":"error-session-id","uuid":"test-uuid","model":"claude-sonnet-4-6","claude_code_version":"2.1.97","permissionMode":"limitedPermissions","fast_mode_state":"off","apiKeySource":"none","output_style":"default","tools":[],"mcp_servers":[],"slash_commands":[],"agents":[],"skills":[],"plugins":[]}}
{"ts":"2026-04-19T19:00:05.000Z","stream":"stderr","chunk":{"type":"result","subtype":"error","session_id":"error-session-id","uuid":"res-1","is_error":true,"result":"Task failed due to timeout","duration_ms":5000,"duration_api_ms":4000,"num_turns":1,"stop_reason":"error","total_cost_usd":0.0001,"terminal_reason":"error","fast_mode_state":"off","usage":{"input_tokens":50,"output_tokens":10},"modelUsage":{"claude-sonnet-4-6":{"inputTokens":50,"outputTokens":10,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.0001,"contextWindow":200000,"maxOutputTokens":32000}}}}
`

	parser := NewParser("test-company", "test-agent", "error-run")
	run, err := parser.Parse(strings.NewReader(sampleLog))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if run.Status != "error" {
		t.Errorf("Status: got %q, want %q", run.Status, "error")
	}
	if !run.IsError {
		t.Errorf("IsError: got %v, want %v", run.IsError, true)
	}
	if run.Result != "Task failed due to timeout" {
		t.Errorf("Result: got %q, want error message", run.Result)
	}
	if run.TerminalReason != "error" {
		t.Errorf("TerminalReason: got %q, want %q", run.TerminalReason, "error")
	}
}

func TestParseNDJSON_MultipleToolCalls(t *testing.T) {
	// Test with multiple tool calls and results
	sampleLog := `{"ts":"2026-04-19T20:00:00.000Z","stream":"stdout","chunk":{"type":"system","subtype":"init","cwd":"/tmp/test","session_id":"multi-tool-session","uuid":"test-uuid","model":"claude-sonnet-4-6","claude_code_version":"2.1.97","permissionMode":"bypassPermissions","fast_mode_state":"off","apiKeySource":"none","output_style":"default","tools":[],"mcp_servers":[],"slash_commands":[],"agents":[],"skills":[],"plugins":[]}}
{"ts":"2026-04-19T20:00:05.000Z","stream":"stdout","chunk":{"type":"assistant","session_id":"multi-tool-session","uuid":"asst-1","message":{"model":"claude-sonnet-4-6","id":"msg_01","type":"message","role":"assistant","content":[{"type":"tool_use","id":"tool_01","name":"Read","input":{"file":"/tmp/test.txt"}},{"type":"tool_use","id":"tool_02","name":"Write","input":{"file":"/tmp/output.txt"}}],"usage":{"input_tokens":100,"output_tokens":50},"stop_reason":"tool_use"}}}
{"ts":"2026-04-19T20:00:10.000Z","stream":"stdout","chunk":{"type":"user","session_id":"multi-tool-session","uuid":"user-1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_01","content":"file contents","is_error":false},{"type":"tool_result","tool_use_id":"tool_02","content":"","is_error":false}]}}}
{"ts":"2026-04-19T20:00:15.000Z","stream":"stdout","chunk":{"type":"result","subtype":"success","session_id":"multi-tool-session","uuid":"res-1","is_error":false,"result":"Done","duration_ms":15000,"duration_api_ms":10000,"num_turns":2,"stop_reason":"end_turn","total_cost_usd":0.001,"terminal_reason":"completed","fast_mode_state":"off","usage":{"input_tokens":100,"output_tokens":50},"modelUsage":{"claude-sonnet-4-6":{"inputTokens":100,"outputTokens":50,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.001,"contextWindow":200000,"maxOutputTokens":32000}}}}
`

	parser := NewParser("test-company", "test-agent", "multi-tool-run")
	run, err := parser.Parse(strings.NewReader(sampleLog))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if run.NumToolCalls != 2 {
		t.Errorf("NumToolCalls: got %d, want %d", run.NumToolCalls, 2)
	}
	if len(run.ToolCalls) != 2 {
		t.Errorf("ToolCalls length: got %d, want %d", len(run.ToolCalls), 2)
	}

	// First tool
	if run.ToolCalls[0].Name != "Read" {
		t.Errorf("First ToolCall Name: got %q, want %q", run.ToolCalls[0].Name, "Read")
	}
	if run.ToolCalls[0].Result != "file contents" {
		t.Errorf("First ToolCall Result: got %q, want %q", run.ToolCalls[0].Result, "file contents")
	}

	// Second tool
	if run.ToolCalls[1].Name != "Write" {
		t.Errorf("Second ToolCall Name: got %q, want %q", run.ToolCalls[1].Name, "Write")
	}
	if run.ToolCalls[1].Result != "" {
		t.Errorf("Second ToolCall Result: got %q, want empty string", run.ToolCalls[1].Result)
	}
}

func TestClaudeParseJSONL_ErrorHandling(t *testing.T) {
	// Test Claude parser with tool error result
	sampleLog := `{"type":"assistant","timestamp":"2026-04-19T18:00:05.000Z","sessionId":"error-test-id","uuid":"asst-1","message":{"model":"claude-haiku-4-5-20251001","id":"msg_01","type":"message","role":"assistant","content":[{"type":"tool_use","id":"tool_01","name":"Bash","input":{"command":"false"}}],"usage":{"input_tokens":100,"output_tokens":50}},"cwd":"/home/test","version":"2.1.91"}
{"type":"user","timestamp":"2026-04-19T18:00:10.000Z","sessionId":"error-test-id","uuid":"user-1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_01","content":"Command failed with exit code 1","is_error":true}]}}
{"type":"last-prompt","lastPrompt":"done","sessionId":"error-test-id","timestamp":"2026-04-19T18:00:15.000Z"}
`

	parser := NewClaudeParser("error-test-id", "/home/test/projects/")
	run, err := parser.Parse(strings.NewReader(sampleLog))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if run.NumToolCalls != 1 {
		t.Errorf("NumToolCalls: got %d, want %d", run.NumToolCalls, 1)
	}
	if len(run.ToolCalls) == 0 {
		t.Fatalf("ToolCalls is empty, cannot check error flag")
	}
	if !run.ToolCalls[0].IsError {
		t.Errorf("ToolCall IsError: got %v, want %v", run.ToolCalls[0].IsError, true)
	}
	if run.ToolCalls[0].Result != "Command failed with exit code 1" {
		t.Errorf("ToolCall Result: got %q, want error message", run.ToolCalls[0].Result)
	}
}

func TestClaudeParseJSONL_EmptyLog(t *testing.T) {
	// Test Claude parser with empty log
	sampleLog := ""

	parser := NewClaudeParser("empty-session", "/home/test/projects/")
	run, err := parser.Parse(strings.NewReader(sampleLog))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should return valid but empty run
	if run.SessionID != "empty-session" {
		t.Errorf("SessionID: got %q, want %q", run.SessionID, "empty-session")
	}
	if run.Status != "success" {
		t.Errorf("Status: got %q, want %q (default for empty log)", run.Status, "success")
	}
	if run.NumTurns != 0 {
		t.Errorf("NumTurns: got %d, want %d", run.NumTurns, 0)
	}
}
