package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/nebulacsf/sentinel/internal/config"
	"github.com/nebulacsf/sentinel/internal/monitor"
	"github.com/nebulacsf/sentinel/internal/sshx"
)

func sampleModel(t *testing.T) Model {
	t.Helper()
	servers := []config.Server{
		{Name: "web-1", Type: "web", Host: "203.0.113.10", User: "root", Port: 22, IdentityFile: "~/.ssh/id_ed25519"},
		{Name: "vici-1", Type: "vicidial", Host: "198.51.100.20", User: "root", Port: 2222, IdentityFile: "~/.ssh/ocean"},
		{Name: "software-1", Type: "software", Host: "192.0.2.30", User: "deploy", Port: 22},
	}
	m := New(Options{
		Servers:    servers,
		Collector:  monitor.DemoCollector{},
		ConfigPath: "/home/test/.sentinel/servers.conf",
		Interval:   30 * time.Second,
		Timeout:    5 * time.Second,
		Parallel:   4,
	})
	m.width = 100
	m.height = 30
	now := time.Now()
	metrics := sshx.Metrics{
		Uptime: "12 days, 4 hours",
		Memory: sshx.Memory{TotalMB: 7982, UsedMB: 2145, AvailableMB: 5837, UsedPct: 26.9},
		Disk:   sshx.Disk{Size: "80G", Used: "18G", Avail: "62G", UsedPct: 23},
	}
	m.states["web-1"] = serverState{
		Status: sshx.StatusOnline, Metrics: metrics, HasMetrics: true,
		Latency: 142 * time.Millisecond, LastSuccess: now, UpdatedAt: now,
	}
	m.states["vici-1"] = serverState{
		Status: sshx.StatusOffline, Metrics: metrics, HasMetrics: true,
		Reason: "timeout", Err: errString("timeout after 5s"),
		LastSuccess: now.Add(-3 * time.Minute), UpdatedAt: now,
	}
	m.states["software-1"] = serverState{
		Status: sshx.StatusError, Reason: "auth",
		Err: errString("ssh: handshake failed: unable to authenticate"), UpdatedAt: now,
	}
	return m
}

type errString string

func (e errString) Error() string { return string(e) }

func TestComputeLayout(t *testing.T) {
	tests := []struct {
		avail     int
		columns   []colKey
		wantWidth int
	}{
		{120, []colKey{colName, colType, colStatus, colUptime, colMem, colDisk, colLat}, 120},
		{100, []colKey{colName, colType, colStatus, colUptime, colMem, colDisk, colLat}, 100},
		{80, []colKey{colName, colStatus, colMem, colDisk, colLat}, 80},
		{50, []colKey{colName, colStatus, colLat}, 50},
		{40, []colKey{colName, colStatus, colLat}, 40},
	}
	for _, tt := range tests {
		layout := computeLayout(tt.avail)
		var keys []colKey
		for _, col := range layout.columns {
			keys = append(keys, col.key)
		}
		if len(keys) != len(tt.columns) {
			t.Fatalf("avail %d: columns = %v, want %v", tt.avail, keys, tt.columns)
		}
		for i := range keys {
			if keys[i] != tt.columns[i] {
				t.Fatalf("avail %d: columns = %v, want %v", tt.avail, keys, tt.columns)
			}
		}
		if got := layout.contentWidth(); got > tt.wantWidth {
			t.Errorf("avail %d: content width = %d, want <= %d", tt.avail, got, tt.wantWidth)
		}
	}
}

func TestRenderFitsTerminal(t *testing.T) {
	m := sampleModel(t)
	defer m.cancel()

	for _, width := range []int{40, 50, 80, 100, 120} {
		for _, height := range []int{24, 30} {
			m.width = width
			m.height = height
			for _, screen := range []screen{screenTable, screenDetail, screenHelp} {
				m.screen = screen
				out := m.render()
				lines := strings.Split(out, "\n")
				if len(lines) > height {
					t.Errorf("width %d height %d screen %d: %d lines, want <= %d", width, height, screen, len(lines), height)
				}
				for i, line := range lines {
					if got := lipgloss.Width(line); got > width {
						t.Errorf("width %d height %d screen %d line %d: width %d: %q", width, height, screen, i, got, line)
					}
				}
			}
		}
	}
}

func TestTableAndDetailContent(t *testing.T) {
	m := sampleModel(t)
	defer m.cancel()
	m.width = 120
	m.height = 30

	out := m.render()
	for _, want := range []string{"SENTINEL", "web-1", "ONLINE", "OFFLINE", "ERROR", "12d 4h", "2.1G/7.8G 27%", "3m stale"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}

	m.screen = screenDetail
	m.cursor = 1
	out = m.render()
	for _, want := range []string{"vici-1", "LAST KNOWN METRICS", "timeout after 5s", "up 12 days, 4 hours", "CONNECTION"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}

	m.height = 24
	out = m.render()
	for _, want := range []string{"CONNECTION", "status", "~/.ssh/ocean", "esc back"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail at height 24 missing %q:\n%s", want, out)
		}
	}
}

func TestTerminalAffordance(t *testing.T) {
	m := sampleModel(t)
	defer m.cancel()
	m.width = 120
	m.height = 40

	m.screen = screenDetail
	if out := m.render(); !strings.Contains(out, "t terminal") {
		t.Errorf("detail footer missing terminal hint:\n%s", out)
	}

	m.screen = screenHelp
	if out := m.render(); !strings.Contains(out, "SSH terminal") {
		t.Errorf("help missing terminal entry:\n%s", out)
	}
}

func TestTerminalDoneShowsErrorAndRefreshes(t *testing.T) {
	m := sampleModel(t)
	defer m.cancel()

	updated, cmd := m.Update(terminalDoneMsg{server: "web-1", err: errString("exit status 255")})
	got := updated.(Model)
	if !strings.Contains(got.warning(), "terminal web-1: exit status 255") {
		t.Fatalf("warning = %q, want terminal error", got.warning())
	}
	if cmd == nil {
		t.Fatal("expected a refresh command after returning from the terminal")
	}
}

func TestTerminalDisabledInDemo(t *testing.T) {
	m := New(Options{
		Servers: []config.Server{{Name: "a", Type: "web", Host: "h", User: "u", Port: 22}},
		Demo:    true,
	})
	defer m.cancel()
	m.screen = screenDetail

	cmd := m.terminalCmd()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg, ok := cmd().(terminalDoneMsg)
	if !ok || msg.err == nil {
		t.Fatalf("expected terminalDoneMsg with an error, got %#v", msg)
	}
}

func TestEmptyState(t *testing.T) {
	m := New(Options{ConfigPath: "/tmp/servers.conf"})
	defer m.cancel()
	m.width = 80
	m.height = 24
	out := m.render()
	for _, want := range []string{"No servers configured", "sentinel add", "/tmp/servers.conf"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty state missing %q:\n%s", want, out)
		}
	}
}

func TestApplyResultsKeepsStaleMetrics(t *testing.T) {
	m := New(Options{
		Servers:   []config.Server{{Name: "a", Type: "web", Host: "h", User: "u", Port: 22}},
		Collector: monitor.DemoCollector{},
	})
	defer m.cancel()

	onlineAt := time.Now().Add(-time.Minute)
	m.applyResults([]sshx.Result{{
		Server:      m.servers[0],
		Status:      sshx.StatusOnline,
		Metrics:     sshx.Metrics{Uptime: "1 day", Memory: sshx.Memory{TotalMB: 1024}},
		CollectedAt: onlineAt,
	}})
	state := m.state("a")
	if !state.HasMetrics || state.LastSuccess.IsZero() {
		t.Fatalf("online result not applied: %+v", state)
	}

	m.applyResults([]sshx.Result{{
		Server:      m.servers[0],
		Status:      sshx.StatusOffline,
		Reason:      "timeout",
		Err:         errString("timeout"),
		CollectedAt: time.Now(),
	}})
	state = m.state("a")
	if state.Status != sshx.StatusOffline || !state.HasMetrics || state.Metrics.Memory.TotalMB != 1024 {
		t.Fatalf("stale metrics not preserved: %+v", state)
	}
	if state.LastSuccess.IsZero() {
		t.Fatal("last success was cleared")
	}
}

func TestNavigationKeepsCursorInView(t *testing.T) {
	m := sampleModel(t)
	defer m.cancel()
	m.width = 80
	m.height = 12
	m.offset = 0
	m.cursor = 0

	for i := 0; i < 3; i++ {
		m.moveCursor(1)
	}
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.cursor)
	}
	if m.offset != 0 {
		t.Fatalf("offset = %d, want 0", m.offset)
	}
	m.moveCursor(-1)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
}
