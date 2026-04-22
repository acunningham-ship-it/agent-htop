package incidents

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acunningham-ship-it/agent-htop/internal/api"
)

// Scanner detects incidents and generates postmortems.
type Scanner struct {
	apiClient     *api.Client
	companyID     string
	cacheDir      string
	wikiBridgeFn  func(ctx context.Context, filePath, content string) error // Calls wiki_write tool
}

// NewScanner creates a new incident scanner.
func NewScanner(apiClient *api.Client, companyID, cacheDir string) *Scanner {
	return &Scanner{
		apiClient:    apiClient,
		companyID:    companyID,
		cacheDir:     cacheDir,
		wikiBridgeFn: nil, // Will be set by caller
	}
}

// SetWikiBridge sets the function to write postmortems to wiki.
func (s *Scanner) SetWikiBridge(fn func(ctx context.Context, filePath, content string) error) {
	s.wikiBridgeFn = fn
}

// ScanLast24h detects incidents from the past 24 hours.
// It returns a list of incidents that met the trigger criteria.
func (s *Scanner) ScanLast24h(ctx context.Context) ([]*Incident, error) {
	var incidents []*Incident

	// Query all issues from the company (we'll filter by date/time locally)
	issues, err := s.apiClient.ListIssues(ctx, s.companyID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list issues: %w", err)
	}

	now := time.Now()
	oneDayAgo := now.Add(-24 * time.Hour)

	// Check each issue for incident indicators
	for _, issue := range issues {
		updatedAt, _ := time.Parse(time.RFC3339, issue.UpdatedAt)

		// Skip issues not updated in the last 24h
		if updatedAt.Before(oneDayAgo) {
			continue
		}

		// Check for manual incident tag (label:incident or 🚨 prefix)
		if s.hasIncidentTag(issue) {
			incident := &Incident{
				ID:             issue.ID,
				Title:          issue.Title,
				Severity:       "high",
				DetectedAt:     now,
				IndicatorType:  "manual_tag",
				IssueID:        issue.ID,
				RootCauseSummary: "Manually tagged as incident",
			}
			incidents = append(incidents, incident)
			continue
		}

		// Check for error streaks (agent status=error ≥2 times in 24h)
		// Note: This would require querying runs, which is not yet in the API client.
		// For now, we'll detect it from stderr patterns if available.

		// Check for panic or Traceback in issue comments/logs
		if s.hasPanicIndicator(issue) {
			incident := &Incident{
				ID:             issue.ID,
				Title:          issue.Title,
				Severity:       "high",
				DetectedAt:     now,
				IndicatorType:  "panic",
				IssueID:        issue.ID,
				RootCauseSummary: "Panic or unhandled exception detected",
			}
			incidents = append(incidents, incident)
		}
	}

	return incidents, nil
}

// hasIncidentTag checks if an issue has manual incident indicators.
func (s *Scanner) hasIncidentTag(issue *api.Issue) bool {
	// Check for 🚨 prefix in title
	if strings.HasPrefix(issue.Title, "🚨") {
		return true
	}

	// Check for label "incident" in labels
	for _, label := range issue.Labels {
		if strings.ToLower(label) == "incident" {
			return true
		}
	}

	return false
}

// hasPanicIndicator checks if issue shows panic/exception indicators.
func (s *Scanner) hasPanicIndicator(issue *api.Issue) bool {
	// For now, check title and we'd check stderr from logs
	// This is a simplified check; in production, parse run logs
	return strings.Contains(strings.ToLower(issue.Title), "panic") ||
		strings.Contains(strings.ToLower(issue.Title), "traceback")
}

// GeneratePostmortem creates postmortem markdown for an incident.
func (s *Scanner) GeneratePostmortem(ctx context.Context, incident *Incident, capturingAgentID string) (*Postmortem, error) {
	pm := &Postmortem{
		Title:             incident.Title,
		Date:              incident.DetectedAt,
		Severity:          incident.Severity,
		IndicatorType:     incident.IndicatorType,
		CapturedByAgentID: capturingAgentID,
		WhatBroke:         "Automated incident detection triggered. Manual review required.",
		RootCause:         fmt.Sprintf("Detected via: %s\nRun IDs: %v", incident.IndicatorType, incident.RunIDs),
		Fix:               "To be completed during incident response.",
		Prevention:        "To be completed during incident response.",
		Timeline:          incident.TimelineEvents,
		RelatedLinks:      incident.RelatedWikiLinks,
	}

	return pm, nil
}

// PostmortemToMarkdown converts a postmortem to markdown format.
func (s *Scanner) PostmortemToMarkdown(pm *Postmortem) string {
	var sb strings.Builder

	// Front matter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("title: %s\n", pm.Title))
	sb.WriteString(fmt.Sprintf("date: %s\n", pm.Date.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("type: incident\n"))
	sb.WriteString(fmt.Sprintf("source: derived\n"))
	sb.WriteString(fmt.Sprintf("captured_by: %s\n", pm.CapturedByAgentID))
	sb.WriteString(fmt.Sprintf("severity: %s\n", pm.Severity))

	if len(pm.RelatedLinks) > 0 {
		sb.WriteString("related:\n")
		for _, link := range pm.RelatedLinks {
			sb.WriteString(fmt.Sprintf("  - \"%s\"\n", link))
		}
	}
	sb.WriteString("---\n\n")

	// Content
	sb.WriteString(fmt.Sprintf("# %s\n\n", pm.Title))

	sb.WriteString("## What broke\n")
	sb.WriteString(pm.WhatBroke)
	sb.WriteString("\n\n")

	sb.WriteString("## Root cause\n")
	sb.WriteString(pm.RootCause)
	sb.WriteString("\n\n")

	sb.WriteString("## Fix\n")
	sb.WriteString(pm.Fix)
	sb.WriteString("\n\n")

	sb.WriteString("## Prevention\n")
	sb.WriteString(pm.Prevention)
	sb.WriteString("\n\n")

	if len(pm.Timeline) > 0 {
		sb.WriteString("## Timeline\n")
		for _, event := range pm.Timeline {
			sb.WriteString(fmt.Sprintf("- `%s` %s\n", event.Time.Format("15:04"), event.Description))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// CheckRateLimit checks if we can write a new incident this week.
// Returns (canWrite, currentCount, error).
func (s *Scanner) CheckRateLimit(ctx context.Context) (bool, int, error) {
	trackPath := filepath.Join(s.cacheDir, "incident-count.json")

	// Ensure cache dir exists
	if err := os.MkdirAll(s.cacheDir, 0755); err != nil {
		return false, 0, fmt.Errorf("failed to create cache dir: %w", err)
	}

	// Read existing tracker
	var tracker RateLimitTracker
	if data, err := os.ReadFile(trackPath); err == nil {
		if err := json.Unmarshal(data, &tracker); err != nil {
			tracker = RateLimitTracker{WeekCounts: make(map[string]int)}
		}
	} else {
		tracker = RateLimitTracker{WeekCounts: make(map[string]int)}
	}

	// Get current ISO week
	week := s.getISOWeek(time.Now())

	// Check count for this week
	count := tracker.WeekCounts[week]
	if count >= 3 {
		return false, count, nil
	}

	// Increment and save
	tracker.WeekCounts[week]++
	if data, err := json.MarshalIndent(tracker, "", "  "); err == nil {
		os.WriteFile(trackPath, data, 0644)
	}

	return true, count, nil
}

// DuplicateCheck checks if this incident is a recent duplicate.
// Returns (isDuplicate, existingFilePath, error).
func (s *Scanner) DuplicateCheck(ctx context.Context, incident *Incident) (bool, string, error) {
	hash := s.hashRootCause(incident)

	// Read existing postmortems from disk
	homeDir, _ := os.UserHomeDir()
	wikiDir := filepath.Join(homeDir, "wiki", "incidents")
	if _, err := os.Stat(wikiDir); os.IsNotExist(err) {
		return false, "", nil // No postmortems yet
	}

	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return false, "", fmt.Errorf("failed to read wiki dir: %w", err)
	}

	now := time.Now()
	thirtyDaysAgo := now.Add(-30 * 24 * time.Hour)

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			filePath := filepath.Join(wikiDir, entry.Name())
			info, _ := entry.Info()

			// Check if within 30 days
			if info.ModTime().Before(thirtyDaysAgo) {
				continue
			}

			// Read file and check hash
			content, _ := os.ReadFile(filePath)
			if strings.Contains(string(content), hash) {
				return true, filePath, nil
			}
		}
	}

	return false, "", nil
}

// hashRootCause generates a SHA256 hash of the root cause for dedup.
func (s *Scanner) hashRootCause(incident *Incident) string {
	input := incident.RootCauseSummary + incident.FilePath
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash)[:16] // First 16 chars for readability
}

// getISOWeek returns the ISO week string like "2026-W16".
func (s *Scanner) getISOWeek(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// WritePostmortem writes a postmortem to the wiki via the bridge function.
func (s *Scanner) WritePostmortem(ctx context.Context, incident *Incident, capturingAgentID string) (string, error) {
	if s.wikiBridgeFn == nil {
		return "", fmt.Errorf("wiki bridge function not set")
	}

	pm, err := s.GeneratePostmortem(ctx, incident, capturingAgentID)
	if err != nil {
		return "", err
	}

	content := s.PostmortemToMarkdown(pm)
	filePath := s.generatePostmortemPath(incident)

	if err := s.wikiBridgeFn(ctx, filePath, content); err != nil {
		return "", fmt.Errorf("failed to write postmortem: %w", err)
	}

	return filePath, nil
}

// generatePostmortemPath creates the file path for a postmortem.
func (s *Scanner) generatePostmortemPath(incident *Incident) string {
	slug := strings.ToLower(strings.ReplaceAll(incident.Title, " ", "-"))
	slug = strings.ReplaceAll(slug, "/", "-")
	date := time.Now().Format("2006-01-02")
	return fmt.Sprintf("wiki/incidents/%s-%s.md", date, slug)
}
