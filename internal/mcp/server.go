package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/acunningham-ship-it/agent-htop/internal/action"
	"github.com/acunningham-ship-it/agent-htop/internal/aggregator"
	"github.com/acunningham-ship-it/agent-htop/internal/api"
	"github.com/acunningham-ship-it/agent-htop/internal/queue"
)

// JSONRPCRequest represents an inbound JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id"`
}

// JSONRPCResponse represents an outbound JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *JSONRPCErr `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// JSONRPCErr represents a JSON-RPC 2.0 error.
type JSONRPCErr struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Server is the MCP JSON-RPC 2.0 server.
type Server struct {
	agg           *aggregator.Aggregator
	apiClient     *api.Client
	companyID     string
	callerAgentID string
	actionLog     *action.ActionLog
	queueManager  *queue.Manager

	mu sync.Mutex // Protects scanner safety if needed
}

// NewServer creates a new MCP server.
func NewServer(agg *aggregator.Aggregator, apiClient *api.Client, companyID, callerAgentID string, queueMgr *queue.Manager) *Server {
	s := &Server{
		agg:           agg,
		apiClient:     apiClient,
		companyID:     companyID,
		callerAgentID: callerAgentID,
		queueManager:  queueMgr,
	}

	// Initialize action log
	var err error
	s.actionLog, err = action.NewActionLog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize action log: %v\n", err)
	}

	return s
}

// Start runs the MCP server, reading from stdin and writing to stdout.
func (s *Server) Start(ctx context.Context) {
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse JSON-RPC request
		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			// Parse error: return JSON-RPC error response
			errResp := &JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &JSONRPCErr{
					Code:    -32700,
					Message: "Parse error",
					Data:    err.Error(),
				},
				ID: nil,
			}
			s.writeResponse(errResp)
			continue
		}

		// Validate JSON-RPC version
		if req.JSONRPC != "2.0" {
			errResp := &JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &JSONRPCErr{
					Code:    -32600,
					Message: "Invalid Request",
					Data:    "jsonrpc must be '2.0'",
				},
				ID: req.ID,
			}
			s.writeResponse(errResp)
			continue
		}

		// Handle request
		resp := s.handleRequest(ctx, &req)
		s.writeResponse(resp)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Scanner error: %v\n", err)
	}
}

// handleRequest dispatches JSON-RPC request to the appropriate tool handler.
func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	// Map of method name to handler function
	handlers := map[string]func(json.RawMessage) (interface{}, *JSONRPCErr){
		"list_sessions":         s.handleListSessions,
		"get_session":           s.handleGetSession,
		"get_anomalies":         s.handleGetAnomalies,
		"kill_session":          s.handleKillSession,
		"pause_session":         s.handlePauseSession,
		"get_cost_projection":   s.handleGetCostProjection,
		"get_tool_usage":        s.handleGetToolUsage,
		"list_policies":         s.handleListPolicies,
		"get_system_alerts":     s.handleGetSystemAlerts,
		"get_network_metrics":   s.handleGetNetworkMetrics,
		"get_host_metrics":      s.handleGetHostMetrics,
		"get_gpu_metrics":       s.handleGetGPUMetrics,
		"get_agent_task":        s.handleGetAgentTask,
		"enqueue_task":          s.handleEnqueueTask,
		"claim_task":            s.handleClaimTask,
		"complete_task":         s.handleCompleteTask,
		"fail_task":             s.handleFailTask,
		"list_tasks":            s.handleListTasks,
		"list_processes":        s.handleListProcesses,
		"kill_process":          s.handleKillProcess,
		"get_network_state":     s.handleGetNetworkState,
		"test_connectivity":     s.handleTestConnectivity,
	}

	handler, ok := handlers[req.Method]
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCErr{
				Code:    -32601,
				Message: "Method not found",
				Data:    fmt.Sprintf("unknown method: %s", req.Method),
			},
			ID: req.ID,
		}
	}

	// Call handler
	result, errResp := handler(req.Params)
	if errResp != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   errResp,
			ID:      req.ID,
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      req.ID,
	}
}

// writeResponse writes a JSON-RPC response to stdout.
func (s *Server) writeResponse(resp *JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal response: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

// Helper to extract optional string param from JSON object.
func extractStringParam(params json.RawMessage, key string) (string, *JSONRPCErr) {
	if len(params) == 0 {
		return "", nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(params, &obj); err != nil {
		return "", &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}
	val, ok := obj[key]
	if !ok {
		return "", nil
	}
	str, ok := val.(string)
	if !ok {
		return "", &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    fmt.Sprintf("%s must be a string", key),
		}
	}
	return str, nil
}

// Helper to get caller agent ID from request param or environment.
func (s *Server) getCallerAgentID(params json.RawMessage) string {
	// First check if request param overrides it
	if caller, err := extractStringParam(params, "caller_agent_id"); err == nil && caller != "" {
		return caller
	}
	// Fall back to environment variable or default
	if s.callerAgentID != "" {
		return s.callerAgentID
	}
	return "unknown"
}
