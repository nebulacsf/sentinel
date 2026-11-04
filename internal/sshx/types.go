// Package sshx collects read-only system metrics from remote hosts over SSH.
package sshx

import (
	"time"

	"github.com/nebulacsf/ncsf-sentinel/internal/config"
)

// Status describes the outcome of one collection attempt.
type Status int

const (
	StatusUnknown Status = iota
	StatusOnline
	StatusOffline
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusOnline:
		return "ONLINE"
	case StatusOffline:
		return "OFFLINE"
	case StatusError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Memory is the parsed output of free -m.
type Memory struct {
	TotalMB     int
	UsedMB      int
	AvailableMB int
	UsedPct     float64
}

// Disk is the parsed output of df -P -h /.
type Disk struct {
	Size    string
	Used    string
	Avail   string
	UsedPct int
}

// Metrics is the information rendered by the UI.
type Metrics struct {
	Uptime string
	Memory Memory
	Disk   Disk
}

// Result is the outcome for one server. Metrics are populated on success; the
// UI retains the last successful values when a later collection fails.
type Result struct {
	Server      config.Server
	Status      Status
	Metrics     Metrics
	Err         error
	Reason      string
	Latency     time.Duration
	CollectedAt time.Time
}
