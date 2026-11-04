package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/nebulacsf/ncsf-sentinel/internal/sshx"
)

func (m Model) renderTable() string {
	th := newTheme(m.dark)
	lines := m.headerLines(th)
	inner := m.width - 4

	if len(m.servers) == 0 {
		lines = append(lines, m.emptyState(th, inner)...)
		for len(lines) < m.height-1 {
			lines = append(lines, "")
		}
		lines = append(lines, "  "+th.dim.Render(truncate("q quit · e edit config · ? help", inner))+"  ")
		return strings.Join(lines, "\n")
	}

	layout := computeLayout(inner - 2)
	lines = append(lines, "  "+"  "+headerCells(layout, th)+"  ")
	lines = append(lines, "  "+th.border.Render(strings.Repeat("─", inner))+"  ")

	rows := m.visibleRows()
	offset := m.offset
	if offset > len(m.servers)-1 {
		offset = maxInt(0, len(m.servers)-1)
	}
	end := minInt(offset+rows, len(m.servers))
	for i := offset; i < end; i++ {
		lines = append(lines, m.renderRow(i, layout, inner-2, th))
	}

	if warning := m.warning(); warning != "" {
		lines = append(lines, "  "+th.red.Render(truncate("! "+warning, inner))+"  ")
	}
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, "  "+th.dim.Render(truncate(m.footerText(), inner))+"  ")
	return strings.Join(lines, "\n")
}

func (m Model) emptyState(th theme, inner int) []string {
	lines := []string{
		"",
		"  " + th.text.Bold(true).Render("No servers configured."),
	}
	if m.loadErr != nil {
		lines = append(lines, "", "  "+th.red.Render(truncate("Config error: "+m.loadErr.Error(), inner)))
	}
	lines = append(lines,
		"",
		"  "+th.muted.Render("Add one with:"),
		"  "+th.accent.Render(truncate("ncsf-sentinel add --name web-1 --host 203.0.113.10 --user root --key ~/.ssh/id_ed25519", inner)),
		"",
		"  "+th.dim.Render(truncate("Config: "+m.configPath, inner)),
	)
	return lines
}

func (m Model) renderDetail() string {
	th := newTheme(m.dark)
	inner := m.width - 4
	if len(m.servers) == 0 {
		return m.renderTable()
	}
	index := m.cursor
	if index < 0 {
		index = 0
	}
	if index >= len(m.servers) {
		index = len(m.servers) - 1
	}
	srv := m.servers[index]
	state := m.state(srv.Name)

	left := "← " + th.text.Bold(true).Render(srv.Name) + " " + th.muted.Render("["+strings.ToUpper(srv.Type)+"]")
	right := th.status(state.Status).Render(statusLabel(state.Status))
	body := []string{"  " + spread(left, right, inner) + "  ", ""}

	identity := srv.IdentityFile
	if identity == "" {
		identity = "ssh-agent / default keys"
	}
	endpoint := srv.User + "@" + srv.Addr() + " · " + identity
	body = append(body, "  "+th.muted.Render(truncate(endpoint, inner))+"  ")

	var timing []string
	if !state.UpdatedAt.IsZero() {
		timing = append(timing, "updated "+age(state.UpdatedAt, m.now)+" ago")
	}
	if state.Latency > 0 {
		timing = append(timing, "latency "+formatLatency(state.Latency))
	}
	if !m.nextRefresh.IsZero() && len(m.servers) > 0 {
		timing = append(timing, "next in "+shortDuration(m.nextRefresh.Sub(m.now)))
	}
	if len(timing) == 0 {
		timing = append(timing, "waiting for first refresh")
	}
	body = append(body, "  "+th.dim.Render(truncate(strings.Join(timing, " · "), inner))+"  ", "")

	if state.HasMetrics && state.Status != sshx.StatusOnline {
		label := "LAST KNOWN METRICS · age unknown"
		if !state.LastSuccess.IsZero() {
			label = "LAST KNOWN METRICS · last success " + age(state.LastSuccess, m.now) + " ago"
		}
		body = append(body, "  "+th.amber.Render(truncate(label, inner))+"  ", "")
	}

	if state.Err != nil || (state.Status != sshx.StatusOnline && state.Status != sshx.StatusUnknown) {
		message := "unknown error"
		if state.Err != nil {
			message = state.Err.Error()
		} else if state.Reason != "" {
			message = state.Reason
		}
		body = append(body, "  "+th.red.Bold(true).Render("ERROR"))
		for _, line := range wrapText(message, maxInt(20, inner-6)) {
			body = append(body, "    "+th.red.Render(truncate(line, inner-4)))
		}
		body = append(body, "")
	}

	if state.HasMetrics {
		body = append(body, "  "+th.accent.Bold(true).Render("SYSTEM"))
		body = append(body, "    "+th.text.Render(truncate("Uptime   up "+state.Metrics.Uptime, inner-4)))

		mem := state.Metrics.Memory
		body = append(body, "  "+th.accent.Bold(true).Render("MEMORY"))
		body = append(body, "    "+th.text.Render(truncate(fmt.Sprintf(
			"total %s MB · used %s MB · available %s MB",
			thousands(mem.TotalMB), thousands(mem.UsedMB), thousands(mem.AvailableMB),
		), inner-4)))
		body = append(body, "    "+th.text.Render(fmt.Sprintf(
			"%s / %s   %d%%",
			formatMB(mem.UsedMB), formatMB(mem.TotalMB), int(math.Round(mem.UsedPct)),
		)))
		body = append(body, "    "+renderBar(mem.UsedPct, m.barWidth(inner), th))

		disk := state.Metrics.Disk
		body = append(body, "  "+th.accent.Bold(true).Render("DISK  /"))
		body = append(body, "    "+th.text.Render(truncate(fmt.Sprintf(
			"size %s · used %s · available %s", disk.Size, disk.Used, disk.Avail,
		), inner-4)))
		body = append(body, "    "+th.text.Render(fmt.Sprintf(
			"%s / %s   %d%%", disk.Used, disk.Size, disk.UsedPct,
		)))
		body = append(body, "    "+renderBar(float64(disk.UsedPct), m.barWidth(inner), th))
	} else {
		body = append(body,
			"  "+th.accent.Bold(true).Render("SYSTEM"),
			"    "+th.dim.Render("No data yet — waiting for the first successful refresh."),
			"",
		)
	}

	body = append(body, "  "+th.accent.Bold(true).Render("CONNECTION"))
	conn := [][2]string{
		{"status", statusLabel(state.Status)},
		{"endpoint", srv.Addr()},
		{"user", srv.User},
		{"identity", identity},
		{"timeout", srv.Timeout(m.timeout).String()},
	}
	if !state.LastSuccess.IsZero() {
		conn = append(conn, [2]string{"last success", state.LastSuccess.Format("2006-01-02 15:04:05")})
	}
	for _, row := range conn {
		line := fmt.Sprintf("%-12s %s", row[0], row[1])
		body = append(body, "    "+th.text.Render(truncate(line, inner-4)))
	}

	body = append(body, "", "  "+th.dim.Render(truncate("esc back · ↑↓ browse · r refresh · e edit · q quit", inner))+"  ")
	return fitScreen(body, m.height)
}

func (m Model) barWidth(inner int) int {
	return minInt(maxInt(inner-8, 12), 48)
}

func (m Model) renderHelp() string {
	th := newTheme(m.dark)
	inner := m.width - 4

	lines := []string{
		"  " + th.accent.Bold(true).Render("NCSF SENTINEL") + " " + th.dim.Render("HELP"),
		"",
		"  " + th.text.Bold(true).Render("NAVIGATION"),
		"    " + th.muted.Render("↑/↓, j/k      move selection"),
		"    " + th.muted.Render("g / G         first / last server"),
		"    " + th.muted.Render("enter         open server details"),
		"    " + th.muted.Render("esc           back to the table"),
		"    " + th.muted.Render("?             toggle this help"),
		"",
		"  " + th.text.Bold(true).Render("REFRESH"),
		"    " + th.muted.Render("r             refresh now"),
		"    " + th.muted.Render("+ / -         interval ±5s (5s–5m)"),
		"",
		"  " + th.text.Bold(true).Render("CONFIG"),
		"    " + th.muted.Render("e             edit servers.conf"),
		"    " + th.muted.Render("q, ctrl+c     quit"),
		"",
		"  " + th.text.Bold(true).Render("CONFIG FILE"),
		"    " + th.dim.Render(truncate(m.configPath, inner-4)),
		"",
		"  " + th.dim.Render("Press esc to return."),
	}
	for i, line := range lines {
		lines[i] = truncate(line, m.width)
	}
	return fitScreen(lines, m.height)
}

func fitScreen(lines []string, height int) string {
	if height < 3 {
		height = 3
	}
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	footer := lines[len(lines)-1]
	lines = lines[:height-1]
	lines = append(lines, footer)
	return strings.Join(lines, "\n")
}
