package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

// DiskMetrics contains per-mount disk usage information.
type DiskMetrics struct {
	Device      string    `json:"device"`      // e.g., "/dev/sda1"
	MountPoint  string    `json:"mountPoint"`  // e.g., "/"
	FSType      string    `json:"fsType"`      // e.g., "ext4"
	TotalBytes  uint64    `json:"totalBytes"`
	UsedBytes   uint64    `json:"usedBytes"`
	FreeBytes   uint64    `json:"freeBytes"`
	UsedPercent float64   `json:"usedPercent"` // 0-100
	UpdatedAt   time.Time `json:"updatedAt"`
}

// DiskIOMetrics contains IO statistics for a physical device.
type DiskIOMetrics struct {
	Device            string    `json:"device"`            // e.g., "sda", "nvme0n1"
	ReadsPerSec       float64   `json:"readsPerSec"`
	WritesPerSec      float64   `json:"writesPerSec"`
	ReadBytesPerSec   float64   `json:"readBytesPerSec"`
	WriteBytesPerSec  float64   `json:"writeBytesPerSec"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// DiskList contains all mounted filesystems and their metrics.
type DiskList struct {
	Mounts    []*DiskMetrics   `json:"mounts"`
	IOStats   []*DiskIOMetrics `json:"ioStats,omitempty"` // Only include if non-zero
	UpdatedAt time.Time        `json:"updatedAt"`
}

// DiskCollector periodically collects disk metrics.
type DiskCollector struct {
	mu          sync.RWMutex
	metrics     *DiskList
	ticker      *time.Ticker
	stopCh      chan struct{}
	wg          sync.WaitGroup
	allMounts   bool                    // If true, include pseudo-filesystems
	lastIOState map[string]diskIOState  // For calculating deltas
}

// diskIOState tracks previous IO counters for delta calculation.
type diskIOState struct {
	readsCompleted  uint64
	writesCompleted uint64
	readSectors     uint64
	writeSectors    uint64
	timestamp       time.Time
}

// Pseudo-filesystems to skip unless allMounts=true
var pseudoFilesystems = []string{
	"tmpfs",
	"devfs",
	"sysfs",
	"proc",
	"cgroup",
	"cgroup2",
	"pstore",
	"securityfs",
	"debugfs",
	"tracefs",
	"fuse.gvfsd-fuse",
}

// NewDiskCollector creates a new disk metrics collector.
func NewDiskCollector(interval time.Duration, allMounts bool) *DiskCollector {
	return &DiskCollector{
		metrics:     &DiskList{Mounts: []*DiskMetrics{}, IOStats: []*DiskIOMetrics{}},
		ticker:      time.NewTicker(interval),
		stopCh:      make(chan struct{}),
		allMounts:   allMounts,
		lastIOState: make(map[string]diskIOState),
	}
}

// Start begins collecting disk metrics.
func (d *DiskCollector) Start() error {
	// Collect immediately on start
	if err := d.collect(); err != nil {
		return err
	}

	// Start background collector
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			select {
			case <-d.ticker.C:
				_ = d.collect()
			case <-d.stopCh:
				return
			}
		}
	}()

	return nil
}

// Stop stops the collector.
func (d *DiskCollector) Stop() {
	close(d.stopCh)
	d.ticker.Stop()
	d.wg.Wait()
}

// Get returns the latest disk metrics.
func (d *DiskCollector) Get() *DiskList {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.metrics == nil {
		return &DiskList{Mounts: []*DiskMetrics{}, UpdatedAt: time.Now()}
	}

	// Return a copy to prevent external mutation
	mounts := make([]*DiskMetrics, len(d.metrics.Mounts))
	for i, m := range d.metrics.Mounts {
		metricsCopy := *m
		mounts[i] = &metricsCopy
	}

	ioStats := make([]*DiskIOMetrics, len(d.metrics.IOStats))
	for i, io := range d.metrics.IOStats {
		ioCopy := *io
		ioStats[i] = &ioCopy
	}

	return &DiskList{
		Mounts:    mounts,
		IOStats:   ioStats,
		UpdatedAt: d.metrics.UpdatedAt,
	}
}

// collect fetches the latest disk metrics from the system.
func (d *DiskCollector) collect() error {
	// Get all partitions
	partitions, err := disk.Partitions(!d.allMounts)
	if err != nil {
		return err
	}

	// Filter out pseudo-filesystems if needed
	var mounts []*DiskMetrics
	for _, partition := range partitions {
		if !d.allMounts && isPseudoFilesystem(partition.Fstype) {
			continue
		}

		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			continue // Skip mounts we can't read
		}

		metric := &DiskMetrics{
			Device:      partition.Device,
			MountPoint:  partition.Mountpoint,
			FSType:      partition.Fstype,
			TotalBytes:  usage.Total,
			UsedBytes:   usage.Used,
			FreeBytes:   usage.Free,
			UsedPercent: usage.UsedPercent,
			UpdatedAt:   time.Now(),
		}
		mounts = append(mounts, metric)
	}

	// Sort by mount point for consistent output
	sort.Slice(mounts, func(i, j int) bool {
		return mounts[i].MountPoint < mounts[j].MountPoint
	})

	// Collect IO stats (Linux only for now)
	ioStats := d.collectIOStats()

	// Update metrics
	d.mu.Lock()
	defer d.mu.Unlock()

	d.metrics = &DiskList{
		Mounts:    mounts,
		IOStats:   ioStats,
		UpdatedAt: time.Now(),
	}

	return nil
}

// collectIOStats reads IO statistics from /proc/diskstats on Linux.
// Returns a list of devices with non-zero IO activity.
func (d *DiskCollector) collectIOStats() []*DiskIOMetrics {
	// On non-Linux systems, skip IO stats collection
	if _, err := os.Stat("/proc/diskstats"); os.IsNotExist(err) {
		return []*DiskIOMetrics{}
	}

	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return []*DiskIOMetrics{}
	}
	defer file.Close()

	currentIOState := make(map[string]diskIOState)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		if len(fields) < 14 {
			continue
		}

		// Parse diskstats format: major minor name reads_completed reads_merged reads_sectors ... writes_completed writes_merged writes_sectors ...
		device := fields[2]

		// Skip loop devices and ram disks for cleaner output
		if strings.HasPrefix(device, "loop") || strings.HasPrefix(device, "ram") {
			continue
		}

		var reads, writes, readSectors, writeSectors uint64
		fmt.Sscanf(fields[3], "%d", &reads)
		fmt.Sscanf(fields[5], "%d", &readSectors)
		fmt.Sscanf(fields[7], "%d", &writes)
		fmt.Sscanf(fields[9], "%d", &writeSectors)

		currentIOState[device] = diskIOState{
			readsCompleted:  reads,
			writesCompleted: writes,
			readSectors:     readSectors,
			writeSectors:    writeSectors,
			timestamp:       time.Now(),
		}
	}

	// Calculate deltas and create IO metrics
	var ioMetrics []*DiskIOMetrics
	now := time.Now()

	for device, current := range currentIOState {
		previous, exists := d.lastIOState[device]
		if !exists {
			d.lastIOState[device] = current
			continue
		}

		timeDelta := current.timestamp.Sub(previous.timestamp).Seconds()
		if timeDelta <= 0 {
			continue
		}

		readDelta := current.readsCompleted - previous.readsCompleted
		writeDelta := current.writesCompleted - previous.writesCompleted
		readSectorDelta := current.readSectors - previous.readSectors
		writeSectorDelta := current.writeSectors - previous.writeSectors

		// Only include if there's actual activity
		if readDelta > 0 || writeDelta > 0 {
			metric := &DiskIOMetrics{
				Device:           device,
				ReadsPerSec:      float64(readDelta) / timeDelta,
				WritesPerSec:     float64(writeDelta) / timeDelta,
				ReadBytesPerSec:  float64(readSectorDelta*512) / timeDelta, // sectors are 512 bytes
				WriteBytesPerSec: float64(writeSectorDelta*512) / timeDelta,
				UpdatedAt:        now,
			}
			ioMetrics = append(ioMetrics, metric)
		}

		d.lastIOState[device] = current
	}

	// Sort for consistent output
	sort.Slice(ioMetrics, func(i, j int) bool {
		return ioMetrics[i].Device < ioMetrics[j].Device
	})

	return ioMetrics
}

// isPseudoFilesystem checks if a filesystem type is pseudo.
func isPseudoFilesystem(fstype string) bool {
	for _, pseudo := range pseudoFilesystems {
		if fstype == pseudo {
			return true
		}
	}
	return false
}
