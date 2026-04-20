package sysinfo

import (
	"testing"
	"time"
)

func BenchmarkGetSnapshot(b *testing.B) {
	// Create a state collector with 1-second cache
	sc := NewStateCollector(100*time.Millisecond, 1*time.Second)
	defer sc.Stop()

	if err := sc.Start(); err != nil {
		b.Fatalf("Failed to start collector: %v", err)
	}

	// Wait for initial collection
	time.Sleep(200 * time.Millisecond)

	// Benchmark the snapshot retrieval
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sc.GetSnapshot()
	}
	b.StopTimer()

	// Print some stats
	elapsed := b.Elapsed()
	avgTime := elapsed / time.Duration(b.N)
	b.Logf("Average time per snapshot: %v", avgTime)

	// Ensure we meet the <100ms requirement
	if avgTime > 100*time.Millisecond {
		b.Errorf("GetSnapshot took too long: %v (expected <100ms)", avgTime)
	}
}

func TestStateCollectorCaching(t *testing.T) {
	// Create a state collector with 1-second cache
	sc := NewStateCollector(100*time.Millisecond, 1*time.Second)
	defer sc.Stop()

	if err := sc.Start(); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Wait for initial collection
	time.Sleep(200 * time.Millisecond)

	// Get first snapshot
	snap1 := sc.GetSnapshot()
	if snap1 == nil {
		t.Fatal("First snapshot is nil")
	}

	// Immediately get second snapshot (should be from cache)
	snap2 := sc.GetSnapshot()
	if snap2 == nil {
		t.Fatal("Second snapshot is nil")
	}

	// Timestamps should match (same cache)
	if !snap1.Timestamp.Equal(snap2.Timestamp) {
		t.Errorf("Expected cached snapshot (same timestamp), got different timestamps: %v vs %v",
			snap1.Timestamp, snap2.Timestamp)
	}

	// Wait for cache to expire
	time.Sleep(1100 * time.Millisecond)

	// Get third snapshot (should be fresh)
	snap3 := sc.GetSnapshot()
	if snap3 == nil {
		t.Fatal("Third snapshot is nil")
	}

	// Timestamp should be different (fresh collection)
	if snap1.Timestamp.Equal(snap3.Timestamp) {
		t.Error("Expected fresh snapshot (different timestamp), got same timestamp")
	}
}

func TestStateCollectorStructure(t *testing.T) {
	sc := NewStateCollector(100*time.Millisecond, 1*time.Second)
	defer sc.Stop()

	if err := sc.Start(); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Wait for initial collection
	time.Sleep(200 * time.Millisecond)

	snap := sc.GetSnapshot()
	if snap == nil {
		t.Fatal("Snapshot is nil")
	}

	// Check schema version
	if snap.SchemaVersion != 1 {
		t.Errorf("Expected schemaVersion 1, got %d", snap.SchemaVersion)
	}

	// Check host metrics are populated (CPU should have per-core data)
	if snap.Host.CPU.PercentPerCore == nil {
		t.Error("Host CPU metrics missing")
	}

	// Check that CPU has per-core data
	if len(snap.Host.CPU.PercentPerCore) == 0 {
		t.Error("CPU per-core data is empty")
	}

	// Check process list is populated
	if len(snap.Processes.Processes) == 0 {
		t.Error("Process list is empty")
	}

	// Verify uptime is reasonable (system should have been up > 1 second)
	if snap.Host.Uptime.Seconds < 1 {
		t.Errorf("System uptime seems unreasonable: %d seconds", snap.Host.Uptime.Seconds)
	}
}

func TestStateCollectorDeepCopy(t *testing.T) {
	sc := NewStateCollector(100*time.Millisecond, 1*time.Second)
	defer sc.Stop()

	if err := sc.Start(); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Wait for initial collection
	time.Sleep(200 * time.Millisecond)

	snap1 := sc.GetSnapshot()
	snap2 := sc.GetSnapshot()

	// Modify snap1's CPU data
	if len(snap1.Host.CPU.PercentPerCore) > 0 {
		snap1.Host.CPU.PercentPerCore[0] = 999.0
	}

	// snap2 should not be affected (deep copy verification)
	if len(snap2.Host.CPU.PercentPerCore) > 0 && snap2.Host.CPU.PercentPerCore[0] == 999.0 {
		t.Error("Modification to snap1 affected snap2 - deep copy failed")
	}
}
