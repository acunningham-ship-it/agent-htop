package incidents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPostmortemMarkdownFormat(t *testing.T) {
	scanner := NewScanner(nil, "test-company", "/tmp/test-cache")

	incident := &Incident{
		ID:                "test-incident-1",
		Title:             "Test Aggregator Failure",
		Severity:          "high",
		DetectedAt:        time.Now(),
		IndicatorType:     "panic",
		RootCauseSummary:  "Mutex deadlock in event channel handling",
		FilePath:          "internal/aggregator/aggregator.go:123",
		RelatedWikiLinks:  []string{"[[deadlock-patterns]]", "[[goroutine-safety]]"},
	}

	pm, err := scanner.GeneratePostmortem(context.Background(), incident, "test-agent-id")
	if err != nil {
		t.Fatalf("failed to generate postmortem: %v", err)
	}

	markdown := scanner.PostmortemToMarkdown(pm)

	// Check required sections
	requiredSections := []string{
		"# Test Aggregator Failure",
		"## What broke",
		"## Root cause",
		"## Fix",
		"## Prevention",
		"---",
	}

	for _, section := range requiredSections {
		if !strings.Contains(markdown, section) {
			t.Errorf("postmortem missing required section: %s", section)
		}
	}

	// Check front matter
	if !strings.Contains(markdown, "title: Test Aggregator Failure") {
		t.Error("postmortem missing title in front matter")
	}
	if !strings.Contains(markdown, "severity: high") {
		t.Error("postmortem missing severity in front matter")
	}
	if !strings.Contains(markdown, "[[deadlock-patterns]]") {
		t.Error("postmortem missing related wiki links")
	}
}

func TestRateLimitTracking(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "test-incident-cache")
	defer os.RemoveAll(tmpDir)

	scanner := NewScanner(nil, "test-company", tmpDir)

	// Check rate limit for current week
	canWrite1, count1, err := scanner.CheckRateLimit(context.Background())
	if err != nil {
		t.Fatalf("failed to check rate limit: %v", err)
	}

	if !canWrite1 || count1 != 0 {
		t.Errorf("expected canWrite=true, count=0; got %v, %d", canWrite1, count1)
	}

	// Check again - should increment
	canWrite2, count2, err := scanner.CheckRateLimit(context.Background())
	if err != nil {
		t.Fatalf("failed to check rate limit: %v", err)
	}

	if !canWrite2 || count2 != 1 {
		t.Errorf("expected canWrite=true, count=1; got %v, %d", canWrite2, count2)
	}

	// Verify persisted to disk
	trackPath := filepath.Join(tmpDir, "incident-count.json")
	if _, err := os.Stat(trackPath); os.IsNotExist(err) {
		t.Error("rate limit tracker file not persisted to disk")
	}
}

func TestPostmortemPathGeneration(t *testing.T) {
	scanner := NewScanner(nil, "test-company", "/tmp/test-cache")

	incident := &Incident{
		Title: "Example Aggregator Failure",
	}

	path := scanner.generatePostmortemPath(incident)

	if !strings.HasPrefix(path, "wiki/incidents/") {
		t.Errorf("postmortem path doesn't start with wiki/incidents/: %s", path)
	}

	if !strings.HasSuffix(path, ".md") {
		t.Errorf("postmortem path doesn't end with .md: %s", path)
	}

	if !strings.Contains(path, "example-aggregator-failure") {
		t.Errorf("postmortem path doesn't contain slug: %s", path)
	}
}

func TestISOWeekFormat(t *testing.T) {
	scanner := NewScanner(nil, "test-company", "/tmp/test-cache")

	// Test a known date: 2026-04-22 (Wednesday)
	testDate := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)
	week := scanner.getISOWeek(testDate)

	// April 22, 2026 is in ISO week 17 of 2026
	expectedWeek := "2026-W17"
	if week != expectedWeek {
		t.Errorf("expected ISO week %s, got %s", expectedWeek, week)
	}
}

func TestHashRootCause(t *testing.T) {
	scanner := NewScanner(nil, "test-company", "/tmp/test-cache")

	incident1 := &Incident{
		RootCauseSummary: "Mutex deadlock in event channel",
		FilePath:         "internal/aggregator/aggregator.go",
	}

	incident2 := &Incident{
		RootCauseSummary: "Mutex deadlock in event channel",
		FilePath:         "internal/aggregator/aggregator.go",
	}

	incident3 := &Incident{
		RootCauseSummary: "Mutex deadlock in event channel",
		FilePath:         "internal/other/file.go",
	}

	hash1 := scanner.hashRootCause(incident1)
	hash2 := scanner.hashRootCause(incident2)
	hash3 := scanner.hashRootCause(incident3)

	if hash1 != hash2 {
		t.Error("identical incidents should have identical hashes")
	}

	if hash1 == hash3 {
		t.Error("incidents with different file paths should have different hashes")
	}
}
