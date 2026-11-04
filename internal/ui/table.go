package ui

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/nebulacsf/ncsf-sentinel/internal/config"
	"github.com/nebulacsf/ncsf-sentinel/internal/sshx"
)

type colKey int

const (
	colName colKey = iota
	colType
	colStatus
	colUptime
	colMem
	colDisk
	colLat
)

type column struct {
	key      colKey
	title    string
	width    int
	priority int
	align    lipgloss.Position
}

var tableColumns = []column{
	{key: colName, title: "NAME", priority: 0, align: lipgloss.Left},
	{key: colType, title: "TYPE", width: 9, priority: 5, align: lipgloss.Left},
	{key: colStatus, title: "STATUS", width: 9, priority: 0, align: lipgloss.Left},
	{key: colUptime, title: "UPTIME", width: 13, priority: 4, align: lipgloss.Left},
	{key: colMem, title: "MEM", width: 17, priority: 2, align: lipgloss.Right},
	{key: colDisk, title: "DISK", width: 16, priority: 3, align: lipgloss.Right},
	{key: colLat, title: "LAT", width: 9, priority: 1, align: lipgloss.Right},
}

type tableLayout struct {
	columns   []column
	nameWidth int
}

func (l tableLayout) contentWidth() int {
	width := l.nameWidth
	for _, col := range l.columns {
		if col.key != colName {
			width += col.width
		}
	}
	if len(l.columns) > 1 {
		width += 2 * (len(l.columns) - 1)
	}
	return width
}

// computeLayout drops low-priority columns until the table fits in avail.
func computeLayout(avail int) tableLayout {
	if avail < 20 {
		avail = 20
	}
	cols := append([]column(nil), tableColumns...)
	for {
		fixed := 0
		for _, col := range cols {
			if col.key != colName {
				fixed += col.width
			}
		}
		separators := 2 * (len(cols) - 1)
		nameWidth := avail - fixed - separators
		if nameWidth >= 14 || len(cols) <= 2 {
			nameWidth = maxInt(nameWidth, 10)
			nameWidth = minInt(nameWidth, 32)
			return tableLayout{columns: cols, nameWidth: nameWidth}
		}
		drop := -1
		for i, col := range cols {
			if col.key == colName || col.key == colStatus {
				continue
			}
			if drop == -1 || col.priority > cols[drop].priority {
				drop = i
			}
		}
		if drop == -1 {
			return tableLayout{columns: cols, nameWidth: 10}
		}
		cols = append(cols[:drop], cols[drop+1:]...)
	}
}

func headerCells(layout tableLayout, th theme) string {
	style := th.muted.Bold(true)
	parts := make([]string, 0, len(layout.columns))
	for i, col := range layout.columns {
		width := col.width
		if col.key == colName {
			width = layout.nameWidth
		}
		if i > 0 {
			parts = append(parts, "  ")
		}
		parts = append(parts, renderCell(col.title, width, col.align, style))
	}
	line := strings.Join(parts, "")
	return padLine(line, layout.contentWidth())
}

func (m Model) renderRow(index int, layout tableLayout, contentWidth int, th theme) string {
	srv := m.servers[index]
	state := m.state(srv.Name)
	selected := index == m.cursor

	var b strings.Builder
	if selected {
		b.WriteString(th.selected(th.accent.Bold(true), true).Render("▸ "))
	} else {
		b.WriteString("  ")
	}

	for i, col := range layout.columns {
		if i > 0 {
			if selected {
				b.WriteString(th.selected(th.text, true).Render("  "))
			} else {
				b.WriteString("  ")
			}
		}
		width := col.width
		if col.key == colName {
			width = layout.nameWidth
		}
		text, style := m.cellValue(col.key, srv, state, selected, th)
		b.WriteString(renderCell(text, width, col.align, style))
	}

	// contentWidth is the cell area; b also contains the two-column marker.
	target := contentWidth + 2
	used := lipgloss.Width(b.String())
	if used < target {
		pad := strings.Repeat(" ", target-used)
		if selected {
			b.WriteString(th.selected(th.text, true).Render(pad))
		} else {
			b.WriteString(pad)
		}
	}
	return "  " + b.String() + "  "
}

func (m Model) cellValue(key colKey, srv config.Server, state serverState, selected bool, th theme) (string, lipgloss.Style) {
	stale := state.HasMetrics && state.Status != sshx.StatusOnline

	switch key {
	case colName:
		style := th.text
		if selected {
			style = style.Bold(true)
		}
		return srv.Name, th.selected(style, selected)
	case colType:
		return strings.ToUpper(srv.Type), th.selected(th.muted, selected)
	case colStatus:
		return "● " + statusLabel(state.Status), th.selected(th.status(state.Status), selected)
	case colUptime:
		if !state.HasMetrics {
			return "—", th.selected(th.dim, selected)
		}
		style := th.text
		if stale {
			style = th.dim
		}
		return sshx.CompactUptime(state.Metrics.Uptime), th.selected(style, selected)
	case colMem:
		if !state.HasMetrics {
			return "—", th.selected(th.dim, selected)
		}
		style := th.text
		if stale {
			style = th.dim
		}
		mem := state.Metrics.Memory
		return fmt.Sprintf("%s/%s %d%%", formatMB(mem.UsedMB), formatMB(mem.TotalMB), int(math.Round(mem.UsedPct))), th.selected(style, selected)
	case colDisk:
		if !state.HasMetrics {
			return "—", th.selected(th.dim, selected)
		}
		style := th.text
		if stale {
			style = th.dim
		}
		disk := state.Metrics.Disk
		return fmt.Sprintf("%s/%s %d%%", disk.Used, disk.Size, disk.UsedPct), th.selected(style, selected)
	case colLat:
		switch state.Status {
		case sshx.StatusOnline:
			return formatLatency(state.Latency), th.selected(th.text, selected)
		case sshx.StatusUnknown:
			return "—", th.selected(th.dim, selected)
		case sshx.StatusOffline:
			if state.HasMetrics {
				if state.LastSuccess.IsZero() {
					return "stale", th.selected(th.dim, selected)
				}
				return age(state.LastSuccess, m.now) + " stale", th.selected(th.dim, selected)
			}
			reason := state.Reason
			if reason == "" {
				reason = "offline"
			}
			return reason, th.selected(th.status(state.Status), selected)
		default:
			reason := state.Reason
			if reason == "" {
				reason = "error"
			}
			return reason, th.selected(th.status(state.Status), selected)
		}
	}
	return "", th.selected(th.text, selected)
}

func statusLabel(status sshx.Status) string {
	switch status {
	case sshx.StatusOnline:
		return "ONLINE"
	case sshx.StatusOffline:
		return "OFFLINE"
	case sshx.StatusError:
		return "ERROR"
	default:
		return "LOADING"
	}
}
