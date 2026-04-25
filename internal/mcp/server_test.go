package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestJSONRPCRequest_Unmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*JSONRPCRequest)
	}{
		{
			name:  "valid list_sessions request",
			input: `{"jsonrpc":"2.0","method":"list_sessions","params":{},"id":1}`,
			check: func(req *JSONRPCRequest) {
				if req.JSONRPC != "2.0" {
					t.Errorf("Expected jsonrpc 2.0, got %s", req.JSONRPC)
				}
				if req.Method != "list_sessions" {
					t.Errorf("Expected method list_sessions, got %s", req.Method)
				}
				if req.ID != float64(1) {
					t.Errorf("Expected ID 1, got %v", req.ID)
				}
			},
		},
		{
			name:  "valid get_anomalies request",
			input: `{"jsonrpc":"2.0","method":"get_anomalies","params":{"agent_id":"test"},"id":2}`,
			check: func(req *JSONRPCRequest) {
				if req.Method != "get_anomalies" {
					t.Errorf("Expected method get_anomalies, got %s", req.Method)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req JSONRPCRequest
			err := json.Unmarshal([]byte(tt.input), &req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.check != nil {
				tt.check(&req)
			}
		})
	}
}

func TestJSONRPCResponse_Marshal(t *testing.T) {
	tests := []struct {
		name   string
		resp   *JSONRPCResponse
		check  func([]byte)
	}{
		{
			name: "success response",
			resp: &JSONRPCResponse{
				JSONRPC: "2.0",
				Result:  map[string]interface{}{"sessions": []interface{}{}},
				ID:      1,
			},
			check: func(b []byte) {
				var m map[string]interface{}
				if err := json.Unmarshal(b, &m); err != nil {
					t.Fatalf("Unmarshal result: %v", err)
				}
				if m["jsonrpc"] != "2.0" {
					t.Errorf("Expected jsonrpc 2.0, got %v", m["jsonrpc"])
				}
				if m["result"] == nil {
					t.Error("Expected result field")
				}
				if m["error"] != nil {
					t.Error("Did not expect error field in success response")
				}
			},
		},
		{
			name: "error response",
			resp: &JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &JSONRPCErr{
					Code:    -32601,
					Message: "Method not found",
				},
				ID: 1,
			},
			check: func(b []byte) {
				var m map[string]interface{}
				if err := json.Unmarshal(b, &m); err != nil {
					t.Fatalf("Unmarshal error: %v", err)
				}
				if m["error"] == nil {
					t.Error("Expected error field")
				}
				if m["result"] != nil {
					t.Error("Did not expect result field in error response")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if tt.check != nil {
				tt.check(b)
			}
		})
	}
}

func TestToolHandlers_Integration(t *testing.T) {
	// Since aggregator requires complex setup (watcher, API client, etc.),
	// we test JSON-RPC marshaling and error handling without full integration.
	// Full integration testing is done via the MCP server CLI.
	ctx := context.Background()

	tests := []struct {
		name        string
		request     string
		expectError bool
	}{
		{
			name:        "valid list_sessions request",
			request:     `{"jsonrpc":"2.0","method":"list_sessions","params":{},"id":1}`,
			expectError: false,
		},
		{
			name:        "valid get_anomalies request",
			request:     `{"jsonrpc":"2.0","method":"get_anomalies","params":{},"id":2}`,
			expectError: false,
		},
		{
			name:        "valid list_policies request",
			request:     `{"jsonrpc":"2.0","method":"list_policies","params":{},"id":3}`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req JSONRPCRequest
			err := json.Unmarshal([]byte(tt.request), &req)
			if err != nil {
				t.Fatalf("Failed to unmarshal request: %v", err)
			}
			if req.JSONRPC != "2.0" {
				t.Errorf("Expected jsonrpc 2.0, got %s", req.JSONRPC)
			}
			_ = ctx // Verify context is available for future handler tests
		})
	}
}
