package sshx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	ansiEscape     = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-_]`)
	uptimeReplacer = strings.NewReplacer(
		" weeks", "w", " week", "w",
		" days", "d", " day", "d",
		" hours", "h", " hour", "h",
		" minutes", "m", " minute", "m",
		" seconds", "s", " second", "s",
	)
)

// ParseOutput parses the marked sections produced by the remote command.
func ParseOutput(out string) (Metrics, error) {
	uptimeRaw, err := section(out, "@@SENTINEL_UPTIME@@", "@@SENTINEL_FREE@@")
	if err != nil {
		return Metrics{}, err
	}
	freeRaw, err := section(out, "@@SENTINEL_FREE@@", "@@SENTINEL_DF@@")
	if err != nil {
		return Metrics{}, err
	}
	dfRaw, err := section(out, "@@SENTINEL_DF@@", "")
	if err != nil {
		return Metrics{}, err
	}

	uptime, err := parseUptime(uptimeRaw)
	if err != nil {
		return Metrics{}, fmt.Errorf("parse uptime: %w", err)
	}
	mem, err := parseFree(freeRaw)
	if err != nil {
		return Metrics{}, fmt.Errorf("parse memory: %w", err)
	}
	disk, err := parseDF(dfRaw)
	if err != nil {
		return Metrics{}, fmt.Errorf("parse disk: %w", err)
	}
	return Metrics{Uptime: uptime, Memory: mem, Disk: disk}, nil
}

func section(out, start, end string) (string, error) {
	i := strings.Index(out, start)
	if i < 0 {
		return "", fmt.Errorf("missing section marker %s", start)
	}
	rest := out[i+len(start):]
	if end != "" {
		if j := strings.Index(rest, end); j >= 0 {
			rest = rest[:j]
		}
	}
	return rest, nil
}

func parseUptime(raw string) (string, error) {
	s := strings.TrimSpace(sanitize(raw))
	s = strings.TrimSpace(strings.TrimPrefix(s, "up "))
	if s == "" {
		return "", fmt.Errorf("empty output")
	}

	// Fallback to /proc/uptime, which prints "seconds idle_seconds".
	fields := strings.Fields(s)
	if len(fields) == 2 {
		secs, err1 := strconv.ParseFloat(fields[0], 64)
		_, err2 := strconv.ParseFloat(fields[1], 64)
		if err1 == nil && err2 == nil {
			return formatSeconds(secs), nil
		}
	}
	// Keep the human-readable value on one line: the detail view renders it
	// directly and a stray newline from a broken remote must not break layout.
	return strings.Join(strings.Fields(s), " "), nil
}

func formatSeconds(secs float64) string {
	total := int64(secs)
	if total < 0 {
		total = 0
	}
	days := total / 86400
	hours := (total % 86400) / 3600
	minutes := (total % 3600) / 60

	var parts []string
	add := func(n int64, singular, plural string) {
		if n <= 0 {
			return
		}
		unit := plural
		if n == 1 {
			unit = singular
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, unit))
	}
	add(days, "day", "days")
	add(hours, "hour", "hours")
	add(minutes, "minute", "minutes")
	if len(parts) == 0 {
		return "less than a minute"
	}
	return strings.Join(parts, ", ")
}

func parseFree(raw string) (Memory, error) {
	lines := nonEmptyLines(raw)
	var header, values []string
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], "total") {
			header = fields
			if i+1 < len(lines) {
				values = strings.Fields(lines[i+1])
			}
			break
		}
	}
	if len(header) == 0 || len(values) < 2 {
		return Memory{}, fmt.Errorf("unexpected free output: %q", truncateRaw(raw, 120))
	}

	index := make(map[string]int, len(header))
	for i, name := range header {
		index[strings.ToLower(name)] = i
	}
	// free -m prints a row label ("Mem:") before the numeric columns, while
	// the header line starts directly with "total". Account for that offset.
	offset := 0
	if len(values) > 0 {
		if _, err := strconv.Atoi(values[0]); err != nil {
			offset = 1
		}
	}
	read := func(name string) (int, bool) {
		i, ok := index[name]
		i += offset
		if !ok || i < 0 || i >= len(values) {
			return 0, false
		}
		n, err := strconv.Atoi(values[i])
		if err != nil {
			return 0, false
		}
		return n, true
	}

	total, ok := read("total")
	if !ok || total <= 0 {
		return Memory{}, fmt.Errorf("free output has no valid total")
	}
	available, ok := read("available")
	if !ok {
		if used, ok := read("used"); ok {
			available = total - used
		} else {
			return Memory{}, fmt.Errorf("free output has no available/used columns")
		}
	}
	used := total - available
	if used < 0 {
		used = 0
	}
	if used > total {
		used = total
	}
	return Memory{
		TotalMB:     total,
		UsedMB:      used,
		AvailableMB: available,
		UsedPct:     float64(used) / float64(total) * 100,
	}, nil
}

func parseDF(raw string) (Disk, error) {
	lines := nonEmptyLines(raw)
	for i := len(lines) - 1; i >= 0; i-- {
		fields := strings.Fields(lines[i])
		if len(fields) < 5 {
			continue
		}
		size := fields[len(fields)-5]
		used := fields[len(fields)-4]
		avail := fields[len(fields)-3]
		pctRaw := strings.TrimSuffix(fields[len(fields)-2], "%")
		pct, err := strconv.Atoi(pctRaw)
		if err != nil {
			continue
		}
		if pct < 0 {
			pct = 0
		}
		return Disk{
			Size:    sanitize(size),
			Used:    sanitize(used),
			Avail:   sanitize(avail),
			UsedPct: pct,
		}, nil
	}
	return Disk{}, fmt.Errorf("unexpected df output: %q", truncateRaw(raw, 120))
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// CompactUptime converts "12 days, 4 hours" to "12d 4h" for the table.
func CompactUptime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = uptimeReplacer.Replace(s)
	s = strings.ReplaceAll(s, ",", "")
	return strings.Join(strings.Fields(s), " ")
}

func sanitize(s string) string {
	s = ansiEscape.ReplaceAllString(s, "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == '\r':
			// Drop CR so terminal output cannot move the cursor.
		case unicode.IsPrint(r):
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	runes := []rune(out)
	if len(runes) > 512 {
		out = string(runes[:512]) + "…"
	}
	return out
}

func truncateRaw(s string, n int) string {
	s = sanitize(s)
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "…"
	}
	return s
}
