package sysinfo

import (
	"fmt"
	"os/user"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// ProcessInfo contains information about a single process.
type ProcessInfo struct {
	PID        int32   `json:"pid"`
	PPID       int32   `json:"ppid"`
	Name       string  `json:"name"`
	CmdLine    string  `json:"cmdline"`
	User       string  `json:"user"`
	Status     string  `json:"status"` // S=sleeping, R=running, Z=zombie, etc.
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
	MemMB      uint64  `json:"mem_mb"`
	CreateTime int64   `json:"create_time"` // Unix timestamp
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ProcessList contains all current processes.
type ProcessList struct {
	Processes []*ProcessInfo `json:"processes"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// ProcessCollector periodically collects process information.
type ProcessCollector struct {
	mu        sync.RWMutex
	list      *ProcessList
	ticker    *time.Ticker
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// NewProcessCollector creates a new process collector.
func NewProcessCollector(interval time.Duration) *ProcessCollector {
	return &ProcessCollector{
		list:   &ProcessList{Processes: make([]*ProcessInfo, 0)},
		ticker: time.NewTicker(interval),
		stopCh: make(chan struct{}),
	}
}

// Start begins collecting process information.
func (p *ProcessCollector) Start() error {
	// Collect immediately on start
	if err := p.collect(); err != nil {
		return err
	}

	// Start background collector
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			select {
			case <-p.ticker.C:
				_ = p.collect()
			case <-p.stopCh:
				return
			}
		}
	}()

	return nil
}

// Stop stops the collector.
func (p *ProcessCollector) Stop() {
	close(p.stopCh)
	p.ticker.Stop()
	p.wg.Wait()
}

// Get returns the latest process list.
func (p *ProcessCollector) Get() *ProcessList {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.list == nil {
		return &ProcessList{Processes: make([]*ProcessInfo, 0)}
	}

	// Return a copy to prevent external mutation
	copy := &ProcessList{
		Processes: make([]*ProcessInfo, len(p.list.Processes)),
		UpdatedAt: p.list.UpdatedAt,
	}
	for i, proc := range p.list.Processes {
		procCopy := *proc
		copy.Processes[i] = &procCopy
	}
	return copy
}

// collect fetches the latest process list from the system.
func (p *ProcessCollector) collect() error {
	procs, err := process.Processes()
	if err != nil {
		return err
	}

	processList := make([]*ProcessInfo, 0, len(procs))

	for _, proc := range procs {
		info := &ProcessInfo{
			PID: proc.Pid,
		}

		// Get PPID
		if ppid, err := proc.Ppid(); err == nil {
			info.PPID = ppid
		}

		// Get process name
		if name, err := proc.Name(); err == nil {
			info.Name = name
		}

		// Get command line
		if cmdline, err := proc.Cmdline(); err == nil {
			info.CmdLine = cmdline
		}

		// Get user
		if uids, err := proc.Uids(); err == nil && len(uids) > 0 {
			if u, err := user.LookupId(fmt.Sprintf("%d", uids[0])); err == nil {
				info.User = u.Username
			} else {
				info.User = fmt.Sprintf("uid:%d", uids[0])
			}
		}

		// Get status
		if status, err := proc.Status(); err == nil && len(status) > 0 {
			info.Status = status[0]
		}

		// Get CPU percent
		if cpu, err := proc.CPUPercent(); err == nil {
			info.CPUPercent = cpu
		}

		// Get memory info
		if mem, err := proc.MemoryInfo(); err == nil {
			memPct, _ := proc.MemoryPercent()
			info.MemPercent = float64(memPct)
			info.MemMB = mem.RSS / 1024 / 1024
		}

		// Get creation time
		if ct, err := proc.CreateTime(); err == nil {
			info.CreateTime = ct / 1000 // Convert from milliseconds to seconds
		}

		info.UpdatedAt = time.Now()
		processList = append(processList, info)
	}

	// Update list
	p.mu.Lock()
	defer p.mu.Unlock()

	p.list = &ProcessList{
		Processes: processList,
		UpdatedAt: time.Now(),
	}

	return nil
}

// SortBy sorts processes by the given field.
// Valid fields: "pid", "cpu", "mem", "name", "user", "age"
func (pl *ProcessList) SortBy(field string) {
	sort.Slice(pl.Processes, func(i, j int) bool {
		pi, pj := pl.Processes[i], pl.Processes[j]
		switch field {
		case "cpu":
			return pi.CPUPercent > pj.CPUPercent
		case "mem":
			return pi.MemPercent > pj.MemPercent
		case "age":
			return pi.CreateTime < pj.CreateTime // Older processes first
		case "name":
			return pi.Name < pj.Name
		case "user":
			return pi.User < pj.User
		case "pid":
			return pi.PID < pj.PID
		default:
			return pi.PID < pj.PID
		}
	})
}

// FilterBy returns processes matching the filter.
// Filters by user or command substring match.
func (pl *ProcessList) FilterBy(filterType, filterValue string) []*ProcessInfo {
	if filterValue == "" {
		return pl.Processes
	}

	var result []*ProcessInfo
	for _, proc := range pl.Processes {
		switch filterType {
		case "user":
			if proc.User == filterValue {
				result = append(result, proc)
			}
		case "cmd":
			if contains(proc.CmdLine, filterValue) || contains(proc.Name, filterValue) {
				result = append(result, proc)
			}
		default:
			result = append(result, proc)
		}
	}
	return result
}

// TopByField returns the top N processes sorted by the given field.
// Default sort is by CPU % descending.
func (pl *ProcessList) TopByField(limit int, field string) []*ProcessInfo {
	if limit <= 0 {
		limit = 20
	}
	if limit > len(pl.Processes) {
		limit = len(pl.Processes)
	}

	// Make a copy and sort
	procs := make([]*ProcessInfo, len(pl.Processes))
	copy(procs, pl.Processes)

	switch field {
	case "cpu":
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].CPUPercent > procs[j].CPUPercent
		})
	case "mem":
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].MemPercent > procs[j].MemPercent
		})
	case "pid":
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].PID < procs[j].PID
		})
	case "name":
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].Name < procs[j].Name
		})
	case "age":
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].CreateTime < procs[j].CreateTime
		})
	default:
		// Default: sort by CPU descending
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].CPUPercent > procs[j].CPUPercent
		})
	}

	return procs[:limit]
}

// contains checks if str contains substr (case-insensitive).
func contains(str, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(str) == 0 {
		return false
	}
	for i := 0; i <= len(str)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(str[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower converts a byte to lowercase.
func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

// KillProcess sends a signal to a process.
// signal: "TERM" (15), "KILL" (9), or other signal names.
func KillProcess(pid int32, signal string) error {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %d", pid)
	}

	sig := "TERM" // Default to SIGTERM
	if signal != "" {
		sig = signal
	}

	return proc.SendSignal(parseSignal(sig))
}

// parseSignal converts a signal name to syscall signal.
func parseSignal(sig string) syscall.Signal {
	switch sig {
	case "TERM", "15":
		return syscall.SIGTERM
	case "KILL", "9":
		return syscall.SIGKILL
	case "HUP", "1":
		return syscall.SIGHUP
	case "INT", "2":
		return syscall.SIGINT
	case "STOP", "19":
		return syscall.SIGSTOP
	default:
		return syscall.SIGTERM
	}
}
