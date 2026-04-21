package parser

import (
	"fmt"
	"strings"
	"testing"
)

// generateSyntheticLog generates a synthetic NDJSON log with numLines lines.
// Each line is a valid Paperclip log entry (system, assistant, user, result events).
func generateSyntheticLog(numLines int) string {
	var sb strings.Builder

	// Initial system event
	systemEvent := `{"ts":"2026-04-20T00:00:00.000Z","stream":"stdout","chunk":"{\"type\":\"system\",\"subtype\":\"init\",\"cwd\":\"/tmp\",\"session_id\":\"test-session\",\"model\":\"claude-sonnet-4-6\",\"permissionMode\":\"normal\"}"}`
	sb.WriteString(systemEvent)
	sb.WriteString("\n")

	// Generate repeating assistant/user/tool_result cycles
	for i := 0; i < numLines-2; i++ {
		if i%3 == 0 {
			// Assistant event with tool call
			assistantEvent := fmt.Sprintf(
				`{"ts":"2026-04-20T00:00:%02d.000Z","stream":"stdout","chunk":"{\"type\":\"assistant\",\"message\":{\"model\":\"claude-sonnet-4-6\",\"content\":[{\"type\":\"tool_use\",\"id\":\"tool_%d\",\"name\":\"Bash\",\"input\":{\"command\":\"echo test\"}}],\"usage\":{\"input_tokens\":100,\"output_tokens\":50}}}"}`+"\n",
				i%60, i,
			)
			sb.WriteString(assistantEvent)
		} else if i%3 == 1 {
			// User event with tool result
			userEvent := fmt.Sprintf(
				`{"ts":"2026-04-20T00:00:%02d.000Z","stream":"stdout","chunk":"{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"tool_%d\",\"content\":\"output_%d\"}]}}"}`+"\n",
				i%60, i-1, i,
			)
			sb.WriteString(userEvent)
		} else {
			// Filler event (rate limit or other)
			fillerEvent := fmt.Sprintf(
				`{"ts":"2026-04-20T00:00:%02d.000Z","stream":"stdout","chunk":"{\"type\":\"rate_limit_event\",\"rate_limit_info\":{\"status\":\"allowed\",\"resetsAt\":1776700000}}"}`+"\n",
				i%60,
			)
			sb.WriteString(fillerEvent)
		}
	}

	// Final result event
	resultEvent := `{"ts":"2026-04-20T00:01:00.000Z","stream":"stdout","chunk":"{\"type\":\"result\",\"subtype\":\"success\",\"duration_ms\":60000,\"total_cost_usd\":0.01,\"num_turns\":10,\"terminal_reason\":\"completed\",\"usage\":{\"input_tokens\":1000,\"output_tokens\":500},\"modelUsage\":{\"claude-sonnet-4-6\":{\"inputTokens\":1000,\"outputTokens\":500,\"costUSD\":0.01,\"contextWindow\":200000}}}"}`
	sb.WriteString(resultEvent)
	sb.WriteString("\n")

	return sb.String()
}

// BenchmarkParserColdParse100k benchmarks parsing a full 100k-line synthetic file.
// Target: < 200ms for cold parse.
func BenchmarkParserColdParse100k(b *testing.B) {
	log := generateSyntheticLog(100000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser := NewParser("test-company", "test-agent", "test-run")
		_, err := parser.Parse(strings.NewReader(log))
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}

// BenchmarkParserColdParse10k benchmarks parsing a 10k-line file (smaller baseline).
func BenchmarkParserColdParse10k(b *testing.B) {
	log := generateSyntheticLog(10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser := NewParser("test-company", "test-agent", "test-run")
		_, err := parser.Parse(strings.NewReader(log))
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}

// BenchmarkParserIncrementalAppend benchmarks the hot-path: reading only new lines.
// Simulates reading last 100 lines of a 100k-line file.
// Target: < 10ms for hot parse.
func BenchmarkParserIncrementalAppend(b *testing.B) {
	// Create full log
	fullLog := generateSyntheticLog(100000)

	// Extract just the last 100 lines (hot parse scenario)
	lines := strings.Split(fullLog, "\n")
	if len(lines) > 100 {
		hotLog := strings.Join(lines[len(lines)-100:], "\n")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			parser := NewParser("test-company", "test-agent", "test-run")
			_, err := parser.Parse(strings.NewReader(hotLog))
			if err != nil {
				b.Fatalf("Parse failed: %v", err)
			}
		}
	}
}

// BenchmarkParserColdParse50k benchmarks parsing a realistic 50k-line session.
// Target: < 200ms (typical long-running session).
func BenchmarkParserColdParse50k(b *testing.B) {
	log := generateSyntheticLog(50000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser := NewParser("test-company", "test-agent", "test-run")
		_, err := parser.Parse(strings.NewReader(log))
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}

// BenchmarkParserColdParse5k benchmarks parsing a typical 5k-line session.
func BenchmarkParserColdParse5k(b *testing.B) {
	log := generateSyntheticLog(5000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser := NewParser("test-company", "test-agent", "test-run")
		_, err := parser.Parse(strings.NewReader(log))
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}
