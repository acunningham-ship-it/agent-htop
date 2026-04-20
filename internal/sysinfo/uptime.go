package sysinfo

import (
	"github.com/shirou/gopsutil/v4/host"
)

// GetUptime returns system uptime in seconds.
func GetUptime() (uint64, error) {
	return host.Uptime()
}
