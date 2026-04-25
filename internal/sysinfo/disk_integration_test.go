
package sysinfo

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestDiskCollectorMultipleMounts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := NewDiskCollector(1*time.Second, false)
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start disk collector: %v", err)
	}
	defer collector.Stop()

	// Wait for collection
	time.Sleep(100 * time.Millisecond)

	diskList := collector.Get()
	if diskList == nil {
		t.Fatal("diskList is nil")
	}

	// Verify we have at least the root mount
	if len(diskList.Mounts) == 0 {
		t.Fatal("Expected at least one mount point")
	}

	t.Logf("Collected %d mounts", len(diskList.Mounts))
	for _, mount := range diskList.Mounts {
		t.Logf("  %s (%s): %.1f%% used", mount.MountPoint, mount.FSType, mount.UsedPercent)

		// Verify each mount has valid data
		if mount.MountPoint == "" {
			t.Errorf("Empty mount point")
		}
		if mount.FSType == "" {
			t.Errorf("Empty filesystem type")
		}
		if mount.UsedPercent < 0 || mount.UsedPercent > 100 {
			t.Errorf("Invalid used percent: %.1f%%", mount.UsedPercent)
		}
	}
}

func TestDiskListJSON(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := NewDiskCollector(1*time.Second, false)
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start disk collector: %v", err)
	}
	defer collector.Stop()

	time.Sleep(100 * time.Millisecond)

	diskList := collector.Get()
	if diskList == nil {
		t.Fatal("diskList is nil")
	}

	// Marshal to JSON to verify JSON serialization works
	jsonData, err := json.Marshal(diskList)
	if err != nil {
		t.Fatalf("Failed to marshal to JSON: %v", err)
	}

	if len(jsonData) == 0 {
		t.Fatal("Empty JSON output")
	}

	t.Logf("JSON output length: %d bytes", len(jsonData))
}

func TestSystemStateFilesystemMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := NewDiskCollector(1*time.Second, false)
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start disk collector: %v", err)
	}
	defer collector.Stop()

	time.Sleep(100 * time.Millisecond)

	diskList := collector.Get()
	state := &SystemState{
		SchemaVersion: 1,
		Timestamp:     time.Now(),
		Host: HostState{
			Disks: diskList,
		},
	}

	// Test GetFilesystemMetrics interface
	fsMetrics := state.GetFilesystemMetrics()
	if len(fsMetrics) == 0 {
		t.Fatal("Expected filesystem metrics, got none")
	}

	if len(fsMetrics) != len(diskList.Mounts) {
		t.Errorf("Expected %d filesystems, got %d", len(diskList.Mounts), len(fsMetrics))
	}

	// Verify interface methods work
	for i, fs := range fsMetrics {
		if fs.GetMountPoint() != diskList.Mounts[i].MountPoint {
			t.Errorf("Mount point mismatch at index %d", i)
		}
		if fs.GetUsedPercent() != diskList.Mounts[i].UsedPercent {
			t.Errorf("Used percent mismatch at index %d", i)
		}
	}

	t.Logf("Successfully retrieved %d filesystems via interface", len(fsMetrics))
}
