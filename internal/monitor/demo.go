package monitor

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/nebulacsf/ncsf-sentinel/internal/config"
	"github.com/nebulacsf/ncsf-sentinel/internal/sshx"
)

// DemoServers returns a small fleet used by --demo when no config exists.
func DemoServers() []config.Server {
	return []config.Server{
		{Name: "demo-web-1", Type: "web", Host: "203.0.113.10", User: "root", Port: 22, IdentityFile: "~/.ssh/id_ed25519"},
		{Name: "demo-web-2", Type: "web", Host: "203.0.113.11", User: "root", Port: 22, IdentityFile: "~/.ssh/id_ed25519"},
		{Name: "demo-vici-1", Type: "vicidial", Host: "198.51.100.20", User: "root", Port: 2222, IdentityFile: "~/.ssh/ocean"},
		{Name: "demo-software-1", Type: "software", Host: "192.0.2.30", User: "deploy", Port: 22},
		{Name: "demo-db-1", Type: "database", Host: "192.0.2.40", User: "postgres", Port: 22},
		{Name: "demo-node-3", Type: "software", Host: "192.0.2.50", User: "deploy", Port: 22},
	}
}

// DemoCollector produces deterministic, simulated metrics. It exists so the
// UI can be exercised without real servers; enable it with --demo.
type DemoCollector struct{}

func (DemoCollector) Collect(ctx context.Context, srv config.Server) sshx.Result {
	start := time.Now()
	seed := hashString(srv.Name)
	delay := time.Duration(60+seed%280) * time.Millisecond

	select {
	case <-ctx.Done():
		return sshx.Result{
			Server:      srv,
			Status:      sshx.StatusOffline,
			Reason:      "canceled",
			Err:         ctx.Err(),
			CollectedAt: time.Now(),
		}
	case <-time.After(delay):
	}

	total := 2048 + int(seed%14336)
	memPct := 15 + int(seed%70)
	used := total * memPct / 100
	diskSize := 40 + int(seed%220)
	diskPct := 10 + int(seed%82)
	diskUsed := diskSize * diskPct / 100

	days := 1 + int(seed%45)
	hours := int(seed / 7 % 24)
	uptime := fmt.Sprintf("%d days, %d hours", days, hours)
	if days == 1 {
		uptime = "1 day"
	}
	if hours == 1 {
		if days == 1 {
			uptime = "1 day, 1 hour"
		} else {
			uptime = fmt.Sprintf("%d days, 1 hour", days)
		}
	}

	metrics := sshx.Metrics{
		Uptime: uptime,
		Memory: sshx.Memory{
			TotalMB:     total,
			UsedMB:      used,
			AvailableMB: total - used,
			UsedPct:     float64(memPct),
		},
		Disk: sshx.Disk{
			Size:    fmt.Sprintf("%dG", diskSize),
			Used:    fmt.Sprintf("%dG", diskUsed),
			Avail:   fmt.Sprintf("%dG", diskSize-diskUsed),
			UsedPct: diskPct,
		},
	}

	if seed%11 == 0 {
		return sshx.Result{
			Server:      srv,
			Status:      sshx.StatusError,
			Metrics:     metrics,
			Reason:      "auth",
			Err:         fmt.Errorf("ssh: handshake failed: unable to authenticate (simulated)"),
			Latency:     time.Since(start),
			CollectedAt: time.Now(),
		}
	}

	if seed%7 == 0 {
		return sshx.Result{
			Server:      srv,
			Status:      sshx.StatusOffline,
			Metrics:     metrics,
			Reason:      "timeout",
			Err:         fmt.Errorf("timeout after %s (simulated)", 5*time.Second),
			Latency:     time.Since(start),
			CollectedAt: time.Now(),
		}
	}

	return sshx.Result{
		Server:      srv,
		Status:      sshx.StatusOnline,
		Metrics:     metrics,
		Latency:     time.Since(start),
		CollectedAt: time.Now(),
	}
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
