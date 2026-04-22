package incidents

import "time"

// Incident represents a detected system incident.
type Incident struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Severity         string            `json:"severity"` // high, medium, low
	DetectedAt       time.Time         `json:"detected_at"`
	LastOccurrence   time.Time         `json:"last_occurrence"`
	IndicatorType    string            `json:"indicator_type"` // error_streak, panic, timeout, manual_tag
	RunIDs           []string          `json:"run_ids"`        // Related run IDs
	IssueID          string            `json:"issue_id,omitempty"`
	RootCauseHash    string            `json:"root_cause_hash"` // SHA256(root_cause + file_path)
	RootCauseSummary string            `json:"root_cause_summary"`
	FilePath         string            `json:"file_path,omitempty"` // For dedup and filing
	ErrorText        string            `json:"error_text,omitempty"`
	TimelineEvents   []TimelineEvent   `json:"timeline,omitempty"`
	RelatedWikiLinks []string          `json:"related_wiki_links,omitempty"` // [[link]] references
}

// TimelineEvent represents a single point in an incident's timeline.
type TimelineEvent struct {
	Time        time.Time `json:"time"`
	Description string    `json:"description"`
}

// Postmortem represents the markdown postmortem file content.
type Postmortem struct {
	Title              string
	Date               time.Time
	Severity           string
	IndicatorType      string
	RelatedLinks       []string // [[link]] format
	WhatBroke          string   // 1-3 paragraphs, symptoms only
	RootCause          string   // Technical explanation with file:line and run IDs
	Fix                string   // What changed (PR/commit links)
	Prevention         string   // What check/test/rule was added
	Timeline           []TimelineEvent
	CapturedByAgentID  string
}

// RateLimitTracker tracks weekly incident counts.
type RateLimitTracker struct {
	WeekCounts map[string]int `json:"counts"` // ISO week keys like "2026-W16"
}

// MetaCascadeIncident is written when 4+ incidents occur in the same week.
type MetaCascadeIncident struct {
	Date               time.Time
	TriggerRunIDs      []string
	RecommendedActions []string
}
