package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

func spread(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if rightWidth >= width {
		return truncate(right, width)
	}
	if leftWidth+rightWidth+1 > width {
		left = truncate(left, maxInt(0, width-rightWidth-1))
		leftWidth = lipgloss.Width(left)
	}
	gap := width - leftWidth - rightWidth
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

func renderCell(text string, width int, align lipgloss.Position, style lipgloss.Style) string {
	text = truncate(text, width)
	gap := width - lipgloss.Width(text)
	if gap < 0 {
		gap = 0
	}
	switch align {
	case lipgloss.Right:
		text = strings.Repeat(" ", gap) + text
	case lipgloss.Center:
		left := gap / 2
		text = strings.Repeat(" ", left) + text + strings.Repeat(" ", gap-left)
	default:
		text += strings.Repeat(" ", gap)
	}
	return style.Render(text)
}

func padLine(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func formatMB(mb int) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1fG", float64(mb)/1024)
	}
	return fmt.Sprintf("%dM", mb)
}

func formatLatency(d time.Duration) string {
	switch {
	case d <= 0:
		return "—"
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(math.Ceil(d.Seconds()))
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
}

func age(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	secs := int(math.Ceil(d.Seconds()))
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh", secs/3600)
	default:
		return fmt.Sprintf("%dd", secs/86400)
	}
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	first := len(s) % 3
	if first == 0 {
		first = 3
	}
	var b strings.Builder
	b.WriteString(s[:first])
	for i := first; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := ""
	for _, word := range words {
		if current == "" {
			current = word
			continue
		}
		if lipgloss.Width(current)+1+lipgloss.Width(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current += " " + word
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func renderBar(pct float64, width int, th theme) string {
	if width < 1 {
		width = 1
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(math.Round(pct / 100 * float64(width)))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return th.bar(pct).Render(strings.Repeat("█", filled)) +
		th.barEmpty.Render(strings.Repeat("░", width-filled))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
