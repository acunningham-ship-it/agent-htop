package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/action"
	"github.com/acunningham-ship-it/agent-htop/internal/anomaly"
	"github.com/acunningham-ship-it/agent-htop/internal/parser"
)

// handleListSessions lists agent sessions with optional filtering.
func (s *Server) handleListSessions(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		Runtime *string `json:"runtime,omitempty"`
		Status  *string `json:"status,omitempty"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &JSONRPCErr{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			}
		}
	}

	// Parse runtime filter if provided
	var runtimeFilter *parser.Runtime
	if req.Runtime != nil {
		rt := parser.Runtime(*req.Runtime)
		runtimeFilter = &rt
	}

	runs := s.agg.ListSessions(runtimeFilter, req.Status)

	// Format response
	type SessionInfo struct {
		ID       string    `json:"id"`
		AgentID  string    `json:"agent_id"`
		Status   string    `json:"status"`
		Model    string    `json:"model"`
		Cost     float64   `json:"cost"`
		Duration int64     `json:"duration_ms"`
		StartAt  time.Time `json:"started_at"`
		EndAt    time.Time `json:"ended_at"`
		IsError  bool      `json:"is_error"`
	}

	sessions := make([]*SessionInfo, len(runs))
	for i, run := range runs {
		sessions[i] = &SessionInfo{
			ID:       run.RunID,
			AgentID:  run.AgentID,
			Status:   run.Status,
			Model:    run.Model,
			Cost:     run.TotalCostUSD,
			Duration: run.DurationMS,
			StartAt:  run.StartTime,
			EndAt:    run.EndTime,
			IsError:  run.IsError,
		}
	}

	return map[string]interface{}{
		"sessions": sessions,
	}, nil
}

// handleGetSession retrieves a specific session by ID.
func (s *Server) handleGetSession(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.ID == "" {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "id is required",
		}
	}

	run := s.agg.GetSessionByID(req.ID)
	if run == nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    "session not found",
		}
	}

	return run, nil
}

// handleGetAnomalies retrieves current anomalies, optionally filtered by agent.
func (s *Server) handleGetAnomalies(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		AgentID *string `json:"agent_id,omitempty"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &JSONRPCErr{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			}
		}
	}

	detector := s.agg.GetDetector()
	var anomalies []*anomaly.AnomalyEvent
	if req.AgentID != nil && *req.AgentID != "" {
		anomalies = detector.CurrentAnomalies(*req.AgentID)
	} else {
		anomalies = detector.CurrentAnomalies("")
	}

	type AnomalyInfo struct {
		SessionID string    `json:"session_id"`
		AgentID   string    `json:"agent_id"`
		AgentName string    `json:"agent_name"`
		Rule      string    `json:"rule"`
		Severity  string    `json:"severity"`
		Timestamp time.Time `json:"timestamp"`
		Message   string    `json:"message"`
	}

	result := make([]*AnomalyInfo, len(anomalies))
	for i, event := range anomalies {
		result[i] = &AnomalyInfo{
			SessionID: event.AgentID, // Use agentID as sessionID for now
			AgentID:   event.AgentID,
			AgentName: event.AgentName,
			Rule:      string(event.AnomalyType),
			Severity:  event.Severity,
			Timestamp: event.DetectedAt,
			Message:   event.Message,
		}
	}

	return map[string]interface{}{
		"anomalies": result,
	}, nil
}

// handleKillSession terminates a session.
func (s *Server) handleKillSession(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		ID            string `json:"id"`
		Reason        string `json:"reason"`
		CallerAgentID *string `json:"caller_agent_id,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.ID == "" {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "id is required",
		}
	}

	run := s.agg.GetSessionByID(req.ID)
	if run == nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    "session not found",
		}
	}

	// Terminate the agent
	err := s.apiClient.TerminateAgent(context.Background(), run.AgentID)
	if err != nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    fmt.Sprintf("failed to terminate agent: %v", err),
		}
	}

	// Log the action
	callerID := s.callerAgentID
	if req.CallerAgentID != nil && *req.CallerAgentID != "" {
		callerID = *req.CallerAgentID
	}
	if callerID == "" {
		callerID = "unknown"
	}
	if s.actionLog != nil {
		s.actionLog.Record(&action.Action{
			Timestamp:       time.Now(),
			CallerAgentID:   callerID,
			ActionType:      "kill",
			TargetSessionID: req.ID,
			Reason:          req.Reason,
		})
	}

	return map[string]interface{}{
		"success":   true,
		"timestamp": time.Now(),
		"message":   "session terminated successfully",
	}, nil
}

// handlePauseSession pauses a session.
func (s *Server) handlePauseSession(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		ID            string `json:"id"`
		Reason        string `json:"reason"`
		CallerAgentID *string `json:"caller_agent_id,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.ID == "" {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "id is required",
		}
	}

	run := s.agg.GetSessionByID(req.ID)
	if run == nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    "session not found",
		}
	}

	// Pause the agent
	err := s.apiClient.PauseAgent(context.Background(), run.AgentID)
	if err != nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    fmt.Sprintf("failed to pause agent: %v", err),
		}
	}

	// Log the action
	callerID := s.callerAgentID
	if req.CallerAgentID != nil && *req.CallerAgentID != "" {
		callerID = *req.CallerAgentID
	}
	if callerID == "" {
		callerID = "unknown"
	}
	if s.actionLog != nil {
		s.actionLog.Record(&action.Action{
			Timestamp:       time.Now(),
			CallerAgentID:   callerID,
			ActionType:      "pause",
			TargetSessionID: req.ID,
			Reason:          req.Reason,
		})
	}

	return map[string]interface{}{
		"success":   true,
		"timestamp": time.Now(),
		"message":   "session paused successfully",
	}, nil
}

// handleGetCostProjection returns cost projection for the fleet.
func (s *Server) handleGetCostProjection(params json.RawMessage) (interface{}, *JSONRPCErr) {
	state := s.agg.GetFleetState()
	if state == nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    "fleet state unavailable",
		}
	}

	type AgentProjection struct {
		AgentID    string  `json:"agent_id"`
		Projection float64 `json:"projection"`
	}

	// Calculate fleet-wide aggregates
	var spentToday, projectedToday, dailyAverage, spendRate float64
	var projectedValues []float64

	for _, agent := range state.Agents {
		if agent.Projection != nil {
			spentToday += agent.Projection.SpentToday
			projectedToday += agent.Projection.ProjectedToday
			dailyAverage += agent.Projection.DailyAverage
			spendRate += agent.Projection.SpendRate
			projectedValues = append(projectedValues, agent.Projection.ProjectedToday)
		}
	}

	agents := make([]*AgentProjection, len(state.Agents))
	for i, agent := range state.Agents {
		projection := 0.0
		if agent.Projection != nil {
			projection = agent.Projection.ProjectedToday
		}
		agents[i] = &AgentProjection{
			AgentID:    agent.AgentID,
			Projection: projection,
		}
	}

	return map[string]interface{}{
		"spent_today":      spentToday,
		"projected_today":  projectedToday,
		"daily_average":    dailyAverage,
		"spend_rate":       spendRate,
		"agents":           agents,
	}, nil
}

// handleGetToolUsage returns tool usage metrics.
func (s *Server) handleGetToolUsage(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		SessionID *string `json:"session_id,omitempty"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &JSONRPCErr{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			}
		}
	}

	var filterID string
	if req.SessionID != nil {
		filterID = *req.SessionID
	}

	heatmap := s.agg.GetToolHeatmap(filterID)
	if heatmap == nil {
		return map[string]interface{}{
			"tools":       []*parser.ToolUsage{},
			"total_calls": 0,
			"total_errors": 0,
		}, nil
	}

	type ToolInfo struct {
		Name       string  `json:"name"`
		CallCount  int     `json:"call_count"`
		ErrorCount int     `json:"error_count"`
		SuccessRate float64 `json:"success_rate"`
		P50Ms      float64 `json:"p50_ms"`
		P95Ms      float64 `json:"p95_ms"`
		MaxMs      float64 `json:"max_ms"`
		CostEst    float64 `json:"cost_est"`
	}

	tools := make([]*ToolInfo, len(heatmap.Tools))
	for i, tool := range heatmap.Tools {
		tools[i] = &ToolInfo{
			Name:        tool.Name,
			CallCount:   tool.CallCount,
			ErrorCount:  tool.ErrorCount,
			SuccessRate: tool.SuccessRate,
			P50Ms:       tool.P50DurationMS,
			P95Ms:       tool.P95DurationMS,
			MaxMs:       tool.MaxDurationMS,
			CostEst:     tool.TotalCostEst,
		}
	}

	return map[string]interface{}{
		"tools":        tools,
		"total_calls":  heatmap.TotalCalls,
		"total_errors": heatmap.TotalErrors,
	}, nil
}

// handleListPolicies returns the list of active anomaly detection policies.
func (s *Server) handleListPolicies(params json.RawMessage) (interface{}, *JSONRPCErr) {
	type Policy struct {
		Name        string  `json:"name"`
		Threshold   string  `json:"threshold"`
		Severity    string  `json:"severity"`
		Description string  `json:"description"`
	}

	policies := []*Policy{
		{
			Name:        "HighSpend",
			Threshold:   ">$0.50/hour",
			Severity:    "warning",
			Description: "Detects when an agent is spending more than $0.50 per hour",
		},
		{
			Name:        "ErrorStreak",
			Threshold:   "≥3 errors in 10 minutes",
			Severity:    "critical",
			Description: "Detects when an agent has 3 or more errors within a 10 minute window",
		},
		{
			Name:        "CostAnomaly",
			Threshold:   ">5x 7-day average",
			Severity:    "critical",
			Description: "Detects when daily spend exceeds 5 times the 7-day average cost",
		},
	}

	return map[string]interface{}{
		"policies": policies,
	}, nil
}

// handleGetSystemAlerts returns current system health alerts.
func (s *Server) handleGetSystemAlerts(params json.RawMessage) (interface{}, *JSONRPCErr) {
	state := s.agg.GetSystemState()
	if state == nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    "system state unavailable",
		}
	}

	type AlertInfo struct {
		Rule      string    `json:"rule"`
		Severity  string    `json:"severity"`
		Message   string    `json:"message"`
		Since     time.Time `json:"since"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	alerts := make([]*AlertInfo, len(state.Host.Alerts))
	for i, alert := range state.Host.Alerts {
		alerts[i] = &AlertInfo{
			Rule:      alert.Rule,
			Severity:  alert.Severity,
			Message:   alert.Message,
			Since:     alert.Since,
			UpdatedAt: alert.UpdatedAt,
		}
	}

	return map[string]interface{}{
		"alerts": alerts,
	}, nil
}
