package parser

import (
	"fmt"
	"io"
)

// CodexParser reads Codex execution logs.
// TODO: Research Codex log format and implement parser.
//
// Codex is a code execution environment similar to Claude Code.
// This is a placeholder pending investigation of:
// 1. Where Codex stores execution logs
// 2. Log format (JSONL? structured? plain text?)
// 3. Fields available (token usage? model? execution time?)
// 4. Agent identification (how to map Codex runs to agent identities?)
//
// Once the format is known, implement similarly to ClaudeParser.
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
// TODO: Implement after Codex log format is documented.
func (p *CodexParser) Parse(r io.Reader) (*AgentRun, error) {
	return nil, fmt.Errorf("Codex parser not yet implemented (awaiting format research)")
}
