package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/action"
	"github.com/acunningham-ship-it/agent-htop/internal/anomaly"
	"github.com/acunningham-ship-it/agent-htop/internal/parser"
	"github.com/acunningham-ship-it/agent-htop/internal/sysinfo"
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

// handleGetNetworkMetrics returns current network metrics.
func (s *Server) handleGetNetworkMetrics(params json.RawMessage) (interface{}, *JSONRPCErr) {
	state := s.agg.GetSystemState()
	if state == nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    "system state unavailable",
		}
	}

	netMetrics := state.Host.Network

	type InterfaceInfo struct {
		Name         string  `json:"name"`
		IP           string  `json:"ip"`
		State        string  `json:"state"`
		BytesSent    uint64  `json:"bytes_sent"`
		BytesRecv    uint64  `json:"bytes_recv"`
		ThroughputUp float64 `json:"throughput_up_bps"`
		ThroughputDn float64 `json:"throughput_down_bps"`
	}

	type WiFiInfo struct {
		Connected bool   `json:"connected"`
		SSID      string `json:"ssid"`
		SignalDBm int    `json:"signal_dBm"`
	}

	interfaces := make([]*InterfaceInfo, len(netMetrics.Interfaces))
	for i, iface := range netMetrics.Interfaces {
		interfaces[i] = &InterfaceInfo{
			Name:         iface.Name,
			IP:           iface.IP,
			State:        iface.State,
			BytesSent:    iface.BytesSent,
			BytesRecv:    iface.BytesRecv,
			ThroughputUp: iface.ThroughputUp,
			ThroughputDn: iface.ThroughputDn,
		}
	}

	var wifi *WiFiInfo
	if netMetrics.WiFi != nil {
		wifi = &WiFiInfo{
			Connected: netMetrics.WiFi.Connected,
			SSID:      netMetrics.WiFi.SSID,
			SignalDBm: netMetrics.WiFi.SignalDBm,
		}
	}

	return map[string]interface{}{
		"internet_up": netMetrics.InternetUp,
		"interfaces":  interfaces,
		"wifi":        wifi,
		"updated_at":  netMetrics.UpdatedAt,
	}, nil
}

// handleGetAgentTask returns the current task and task history for a session.
func (s *Server) handleGetAgentTask(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.SessionID == "" {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "session_id is required",
		}
	}

	// Get the session/run
	run := s.agg.GetSessionByID(req.SessionID)
	if run == nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    "session not found",
		}
	}

	// Find the corresponding agent view to get task info
	var currentTask *parser.CurrentTask
	var taskHistory []*parser.TaskHistory

	state := s.agg.GetFleetState()
	if state != nil {
		for _, agent := range state.Agents {
			if agent.AgentID == run.AgentID {
				currentTask = agent.CurrentTask
				taskHistory = agent.TaskHistory
				break
			}
		}
	}

	type TaskInfo struct {
		Tool        string    `json:"tool,omitempty"`
		ArgsSummary string    `json:"args_summary,omitempty"`
		ElapsedSec  int64     `json:"elapsed_sec"`
		IsStalled   bool      `json:"is_stalled"`
		StartedAt   time.Time `json:"started_at"`
	}

	type HistoryEntry struct {
		Tool        string    `json:"tool"`
		ArgsSummary string    `json:"args_summary"`
		DurationSec int64     `json:"duration_sec"`
		IsError     bool      `json:"is_error"`
		StartedAt   time.Time `json:"started_at"`
		EndedAt     time.Time `json:"ended_at"`
	}

	var currentTaskInfo *TaskInfo
	if currentTask != nil {
		currentTaskInfo = &TaskInfo{
			Tool:        currentTask.ToolName,
			ArgsSummary: currentTask.ArgsSummary,
			ElapsedSec:  currentTask.ElapsedSec,
			IsStalled:   currentTask.IsStalled,
			StartedAt:   currentTask.StartedAt,
		}
	}

	history := make([]*HistoryEntry, len(taskHistory))
	for i, task := range taskHistory {
		history[i] = &HistoryEntry{
			Tool:        task.ToolName,
			ArgsSummary: task.ArgsSummary,
			DurationSec: task.DurationSec,
			IsError:     task.IsError,
			StartedAt:   task.StartedAt,
			EndedAt:     task.EndedAt,
		}
	}

	return map[string]interface{}{
		"session_id":    req.SessionID,
		"agent_id":      run.AgentID,
		"current_task":  currentTaskInfo,
		"task_history":  history,
	}, nil
}

// handleEnqueueTask enqueues a task to a named queue.
func (s *Server) handleEnqueueTask(params json.RawMessage) (interface{}, *JSONRPCErr) {
	if s.queueManager == nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Queue functionality not available",
		}
	}

	var req struct {
		QueueName string          `json:"queue"`
		TaskSpec  json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.QueueName == "" {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "queue name is required",
		}
	}

	taskID, err := s.queueManager.Enqueue(req.QueueName, req.TaskSpec)
	if err != nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Failed to enqueue task",
			Data:    err.Error(),
		}
	}

	return map[string]interface{}{
		"task_id": taskID,
	}, nil
}

// handleClaimTask claims the next pending task from a queue.
func (s *Server) handleClaimTask(params json.RawMessage) (interface{}, *JSONRPCErr) {
	if s.queueManager == nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Queue functionality not available",
		}
	}

	var req struct {
		QueueName string `json:"queue"`
		AgentID   string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.QueueName == "" {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "queue name is required",
		}
	}

	agentID := req.AgentID
	if agentID == "" {
		agentID = s.getCallerAgentID(params)
	}

	task, err := s.queueManager.ClaimTask(req.QueueName, agentID)
	if err != nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Failed to claim task",
			Data:    err.Error(),
		}
	}

	if task == nil {
		return map[string]interface{}{
			"task": nil,
		}, nil
	}

	return map[string]interface{}{
		"task": map[string]interface{}{
			"id":       task.ID,
			"queue_id": task.QueueID,
			"queue":    task.QueueName,
			"spec":     task.Spec,
			"status":   task.Status,
		},
	}, nil
}

// handleCompleteTask marks a task as completed.
func (s *Server) handleCompleteTask(params json.RawMessage) (interface{}, *JSONRPCErr) {
	if s.queueManager == nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Queue functionality not available",
		}
	}

	var req struct {
		TaskID string          `json:"task_id"`
		Result json.RawMessage `json:"result,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.TaskID == "" {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "task_id is required",
		}
	}

	if err := s.queueManager.CompleteTask(req.TaskID, req.Result); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Failed to complete task",
			Data:    err.Error(),
		}
	}

	return map[string]interface{}{
		"success": true,
	}, nil
}

// handleFailTask marks a task as failed, triggering retries or dead-letter.
func (s *Server) handleFailTask(params json.RawMessage) (interface{}, *JSONRPCErr) {
	if s.queueManager == nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Queue functionality not available",
		}
	}

	var req struct {
		TaskID string `json:"task_id"`
		Reason string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.TaskID == "" {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "task_id is required",
		}
	}

	if err := s.queueManager.FailTask(req.TaskID, req.Reason); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Failed to fail task",
			Data:    err.Error(),
		}
	}

	return map[string]interface{}{
		"success": true,
	}, nil
}

// handleListTasks lists tasks in a queue with optional status filter.
func (s *Server) handleListTasks(params json.RawMessage) (interface{}, *JSONRPCErr) {
	if s.queueManager == nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Queue functionality not available",
		}
	}

	var req struct {
		QueueName *string `json:"queue,omitempty"`
		Status    *string `json:"status,omitempty"`
		Limit     *int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	// If no queue specified, return stats for all queues
	if req.QueueName == nil || *req.QueueName == "" {
		snapshot, err := s.queueManager.GetAllStats()
		if err != nil {
			return nil, &JSONRPCErr{
				Code:    -32600,
				Message: "Failed to get queue stats",
				Data:    err.Error(),
			}
		}

		return map[string]interface{}{
			"queues":      snapshot.Queues,
			"total_tasks": snapshot.TotalTasks,
			"updated_at":  snapshot.UpdatedAt,
		}, nil
	}

	limit := 100
	if req.Limit != nil && *req.Limit > 0 {
		limit = *req.Limit
	}

	status := ""
	if req.Status != nil {
		status = *req.Status
	}

	tasks, err := s.queueManager.ListTasks(*req.QueueName, status, limit)
	if err != nil {
		return nil, &JSONRPCErr{
			Code:    -32600,
			Message: "Failed to list tasks",
			Data:    err.Error(),
		}
	}

	taskList := make([]map[string]interface{}, len(tasks))
	for i, task := range tasks {
		taskList[i] = map[string]interface{}{
			"id":           task.ID,
			"queue_id":     task.QueueID,
			"queue":        task.QueueName,
			"status":       task.Status,
			"claimed_by":   task.ClaimedBy,
			"claimed_at":   task.ClaimedAt,
			"completed_at": task.CompletedAt,
			"retry_count":  task.RetryCount,
			"max_retries":  task.MaxRetries,
			"created_at":   task.CreatedAt,
			"updated_at":   task.UpdatedAt,
		}
	}

	return map[string]interface{}{
		"queue": *req.QueueName,
		"status": status,
		"tasks":  taskList,
	}, nil
}

// handleListProcesses lists system processes with optional sorting and limiting.
func (s *Server) handleListProcesses(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		Sort  *string `json:"sort,omitempty"`
		Limit *int    `json:"limit,omitempty"`
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

	// Get process list
	procCollector := s.agg.GetProcessCollector()
	if procCollector == nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    "process collector not available",
		}
	}

	procList := procCollector.Get()
	if procList == nil || procList.Processes == nil {
		return map[string]interface{}{
			"processes": []*sysinfo.ProcessInfo{},
			"count":     0,
			"timestamp": time.Now(),
		}, nil
	}

	// Apply sorting if specified
	if req.Sort != nil && *req.Sort != "" {
		procList.SortBy(*req.Sort)
	}

	// Apply limit if specified
	limit := len(procList.Processes)
	if req.Limit != nil && *req.Limit > 0 {
		if *req.Limit < limit {
			limit = *req.Limit
		}
	}

	return map[string]interface{}{
		"processes": procList.Processes[:limit],
		"count":     limit,
		"timestamp": procList.UpdatedAt,
	}, nil
}

// handleKillProcess sends a signal to a process by PID.
func (s *Server) handleKillProcess(params json.RawMessage) (interface{}, *JSONRPCErr) {
	var req struct {
		PID    int32  `json:"pid"`
		Signal string `json:"signal,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    err.Error(),
		}
	}

	if req.PID == 0 {
		return nil, &JSONRPCErr{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "pid is required and must be non-zero",
		}
	}

	// Default to TERM signal
	signal := "TERM"
	if req.Signal != "" {
		signal = req.Signal
	}

	// Send signal to process
	if err := sysinfo.KillProcess(req.PID, signal); err != nil {
		return nil, &JSONRPCErr{
			Code:    -32603,
			Message: "Internal error",
			Data:    fmt.Sprintf("failed to kill process: %v", err),
		}
	}

	// Log the action
	if s.actionLog != nil {
		entry := &action.ActionLogEntry{
			Timestamp: time.Now(),
			AgentID:   s.getCallerAgentID(params),
			Action:    fmt.Sprintf("kill_process(pid=%d, signal=%s)", req.PID, signal),
			Details:   fmt.Sprintf("Killed process %d with signal %s", req.PID, signal),
			Status:    "success",
		}
		s.actionLog.Log(entry)
	}

	return map[string]interface{}{
		"pid":     req.PID,
		"signal":  signal,
		"success": true,
		"message": fmt.Sprintf("Signal %s sent to process %d", signal, req.PID),
	}, nil
}
