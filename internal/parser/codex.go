package parser

import (
	"fmt"
	"io"
)

// CodexParser reads Codex execution logs.
//
// Codex is a sandboxed code execution environment. Currently in research phase.
// This parser is a stub pending investigation of the Codex log format.
//
// Expected Research Outcomes
// ──────────────────────────
// When research is complete, document:
//
// 1. **Log Location**
//    - Directory path (e.g., ~/.codex/sessions/ or ~/.codex/runs/)
//    - File naming convention
//    - Rollover/retention policy
//
// 2. **Log Format**
//    - JSONL (newline-delimited JSON) - most likely
//    - JSON object per event
//    - UTF-8 encoding with proper escaping
//
// 3. **Event Types**
//    - Execution start/init (required: timestamp, session_id, model, environment)
//    - Code execution events (required: code, language, result, status)
//    - Completion/result (required: exit_code, duration, output)
//    - Error events (required: error message, stack trace)
//    - Metrics (optional: token usage, resource consumption)
//
// 4. **Fields to Extract**
//    - session_id: Unique execution session identifier (string UUID)
//    - model: Which Codex environment (e.g., "codex-python-3.11")
//    - start_time: Execution start timestamp (ISO8601)
//    - end_time: Execution end timestamp (ISO8601)
//    - duration_ms: Total execution time (number)
//    - status: success | error | timeout | cancelled (string)
//    - output: Execution output or error message (string)
//    - exit_code: Process exit code if applicable (number)
//
// 5. **Agent Identification**
//    - Map Codex execution session to Paperclip agent (via API lookup?)
//    - Or extract agent name from context/environment
//    - Or use session ID directly with display name fallback
//
// Implementation Strategy
// ──────────────────────
// Once the format is known:
// 1. Create sample log files in tests/fixtures/codex-logs/
// 2. Implement Parse() similar to ClaudeParser
// 3. Add TestCodexParseJSONL test with sample data
// 4. Integrate into aggregator.go similar to Claude runtime
// 5. Document expected log format in README
type CodexParser struct {
	sessionID string
	logPath   string
}

// NewCodexParser creates a parser for a Codex execution session.
func NewCodexParser(sessionID, logPath string) *CodexParser {
	return &CodexParser{
		sessionID: sessionID,
		logPath:   logPath,
	}
}

// Parse reads a Codex log file and returns an AgentRun.
// Currently returns an error indicating the parser is not yet implemented.
// Once Codex log format is documented (see comments above), implement the parser
// to normalize Codex logs into the standard AgentRun struct.
func (p *CodexParser) Parse(r io.Reader) (*AgentRun, error) {
	return nil, fmt.Errorf("Codex parser not yet implemented: awaiting format specification and sample logs")
}
