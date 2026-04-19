package aggregator

import "github.com/acunningham-ship-it/agent-htop/internal/parser"

// RuntimeCapabilities describes what operations are supported for a given runtime.
type RuntimeCapabilities struct {
	CanKill   bool // Can the runtime kill/stop a running agent
	CanPause  bool // Can the runtime pause a running agent
	CostKnown bool // Does the runtime report token cost
}

// CapabilitiesByRuntime maps each runtime to its capabilities.
var CapabilitiesByRuntime = map[parser.Runtime]RuntimeCapabilities{
	parser.RuntimePaperclip: {CanKill: true, CanPause: true, CostKnown: true},
	parser.RuntimeClaude:    {CanKill: false, CanPause: false, CostKnown: true},
	parser.RuntimeCodex:     {CanKill: false, CanPause: false, CostKnown: false},
}

// GetCapabilities returns the capabilities for a given runtime.
func GetCapabilities(runtime parser.Runtime) RuntimeCapabilities {
	if caps, ok := CapabilitiesByRuntime[runtime]; ok {
		return caps
	}
	// Default to least privileged
	return RuntimeCapabilities{CanKill: false, CanPause: false, CostKnown: false}
}
