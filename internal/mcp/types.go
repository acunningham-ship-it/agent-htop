package mcp

import "encoding/json"

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      interface{}       `json:"id,omitempty"`
	Method  string            `json:"method"`
	Params  json.RawMessage   `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RpcError   `json:"error,omitempty"`
}

// RpcError represents a JSON-RPC 2.0 error.
type RpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error codes
const (
	ParseErrorCode     = -32700
	InvalidRequestCode = -32600
	MethodNotFoundCode = -32601
	InvalidParamsCode  = -32602
	InternalErrorCode  = -32603
	ServerErrorCode    = -32000
)

// Tool parameter types

// ListSessionsParams for list_sessions tool
type ListSessionsParams struct {
	Runtime string `json:"runtime,omitempty"` // Filter by runtime
	Status  string `json:"status,omitempty"`  // Filter by status (running|idle|error)
}

// GetSessionParams for get_session tool
type GetSessionParams struct {
	AgentID string `json:"agentId"`
}

// SessionControlParams for kill/pause/resume tools
type SessionControlParams struct {
	AgentID   string `json:"agentId"`
	Reason    string `json:"reason,omitempty"`
	CallerID  string `json:"callerId,omitempty"` // Optional override for caller
}

// GetCostProjectionParams for get_cost_projection tool
type GetCostProjectionParams struct {
	AgentID string `json:"agentId,omitempty"` // If omitted, returns fleet-wide
}

// GetToolUsageParams for get_tool_usage tool
type GetToolUsageParams struct {
	AgentID string `json:"agentId,omitempty"` // If omitted, returns fleet-wide
}

// GetSystemAlertsParams for get_system_alerts tool
type GetSystemAlertsParams struct {
	// No parameters needed, returns all active system alerts
}

// Tool result types

// SessionControlResult for kill/pause/resume responses
type SessionControlResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
