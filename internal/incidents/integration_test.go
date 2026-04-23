package incidents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEndToEndPostmortemWrite(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "wiki-incidents-test")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	scanner := NewScanner(nil, "test-company", filepath.Join(os.TempDir(), "cache"))
	
	scanner.SetWikiBridge(func(ctx context.Context, filePath, content string) error {
		fileName := filepath.Base(filePath)
		fullPath := filepath.Join(tmpDir, fileName)
		return os.WriteFile(fullPath, []byte(content), 0644)
	})

	now := time.Now()
	incident := &Incident{
		ID:                "HTO-81",
		Title:             "Test incident for verification",
		Severity:          "high",
		DetectedAt:        now,
		IndicatorType:     "manual_tag",
		RootCauseSummary:  "Manual incident tag (🚨 prefix)",
		FilePath:          "HTO-81",
		IssueID:           "e5bd0570-3c2a-4409-b0da-d3daae1594ea",
		RelatedWikiLinks:  []string{"[[manual-incident-tags]]"},
		TimelineEvents: []TimelineEvent{
			{
				Time:        now,
				Description: "Incident detected (manual tag)",
			},
			{
				Time:        now.Add(1 * time.Minute),
				Description: "Postmortem generated",
			},
		},
	}

	filePath, err := scanner.WritePostmortem(context.Background(), incident, "8341cbd0-bab0-4fab-98fa-47b512f34b9c")
	if err != nil {
		t.Fatalf("Failed to write postmortem: %v", err)
	}

	fileName := filepath.Base(filePath)
	createdPath := filepath.Join(tmpDir, fileName)
	if _, err := os.Stat(createdPath); os.IsNotExist(err) {
		t.Errorf("Postmortem file not created at: %s", createdPath)
	}

	content, err := os.ReadFile(createdPath)
	if err != nil {
		t.Errorf("Failed to read postmortem: %v", err)
	}

	contentStr := string(content)
	
	required := []string{
		"title: Test incident for verification",
		"## What broke",
		"## Root cause",
		"## Fix",
		"## Prevention",
		"## Timeline",
		"[[manual-incident-tags]]",
	}
	
	for _, section := range required {
		if !strings.Contains(contentStr, section) {
			t.Errorf("Postmortem missing: %s", section)
		}
	}

	t.Logf("✓ Postmortem written successfully: %s", filePath)
	t.Logf("✓ File size: %d bytes", len(content))
	t.Logf("✓ All required sections present")
}

func TestRateLimitMetaCascade(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "rate-limit-test")
	cacheDir := filepath.Join(os.TempDir(), "cache-rate-test")
	os.MkdirAll(tmpDir, 0755)
	os.MkdirAll(cacheDir, 0755)
	defer os.RemoveAll(tmpDir)
	defer os.RemoveAll(cacheDir)

	scanner := NewScanner(nil, "test-company", cacheDir)

	for i := 1; i <= 4; i++ {
		can, count, err := scanner.CheckRateLimit(context.Background())
		if err != nil {
			t.Fatalf("Rate limit check failed: %v", err)
		}

		if i <= 3 {
			if !can || count != i-1 {
				t.Errorf("Rate limit #%d: expected can=true count=%d; got %v %d", i, i-1, can, count)
			}
		} else {
			if can || count != 3 {
				t.Errorf("Rate limit #%d: expected can=false count=3; got %v %d", i, can, count)
			}
		}
	}

	t.Logf("✓ Rate limiting enforced: max 3/week")
}
