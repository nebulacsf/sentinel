package sshx

import (
	"strings"
	"testing"
)

const sampleOutput = `
@@SENTINEL_UPTIME@@
up 12 days, 4 hours
@@SENTINEL_FREE@@
               total        used        free      shared  buff/cache   available
Mem:            7982        2143        1200         123        4638        5837
Swap:           2047           0        2047
@@SENTINEL_DF@@
Filesystem      Size  Used Avail Use% Mounted on
/dev/vda1        80G   18G   62G  23% /
`

func TestParseOutput(t *testing.T) {
	metrics, err := ParseOutput(sampleOutput)
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if metrics.Uptime != "12 days, 4 hours" {
		t.Errorf("uptime = %q", metrics.Uptime)
	}
	if metrics.Memory.TotalMB != 7982 || metrics.Memory.UsedMB != 2145 || metrics.Memory.AvailableMB != 5837 {
		t.Errorf("memory = %+v", metrics.Memory)
	}
	if pct := metrics.Memory.UsedPct; pct < 26.8 || pct > 26.9 {
		t.Errorf("memory pct = %f", pct)
	}
	if metrics.Disk.Size != "80G" || metrics.Disk.Used != "18G" || metrics.Disk.Avail != "62G" || metrics.Disk.UsedPct != 23 {
		t.Errorf("disk = %+v", metrics.Disk)
	}
}

func TestParseOutputMissingSection(t *testing.T) {
	if _, err := ParseOutput("@@SENTINEL_UPTIME@@\nup 1 hour\n"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseUptimeProcFallback(t *testing.T) {
	got, err := parseUptime("1058400.55 999999.00\n")
	if err != nil {
		t.Fatalf("parseUptime: %v", err)
	}
	if got != "12 days, 6 hours" {
		t.Errorf("got %q, want %q", got, "12 days, 6 hours")
	}
}

func TestParseUptimeMinutes(t *testing.T) {
	got, err := parseUptime("up 3 minutes\n")
	if err != nil {
		t.Fatalf("parseUptime: %v", err)
	}
	if got != "3 minutes" {
		t.Errorf("got %q", got)
	}
}

func TestParseUptimeFlattensWhitespace(t *testing.T) {
	got, err := parseUptime("up  3 days,\n 4 hours\n")
	if err != nil {
		t.Fatalf("parseUptime: %v", err)
	}
	if got != "3 days, 4 hours" {
		t.Errorf("got %q", got)
	}
}

func TestParseFreeWithoutAvailable(t *testing.T) {
	raw := `
             total       used       free     shared    buffers     cached
Mem:          7982       2143       1200        123        200       4438
`
	mem, err := parseFree(raw)
	if err != nil {
		t.Fatalf("parseFree: %v", err)
	}
	if mem.TotalMB != 7982 || mem.UsedMB != 2143 {
		t.Errorf("memory = %+v", mem)
	}
}

func TestParseFreeErrors(t *testing.T) {
	if _, err := parseFree("not free output"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseDFErrors(t *testing.T) {
	if _, err := parseDF("Filesystem Size Used Avail Use% Mounted on"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCompactUptime(t *testing.T) {
	tests := map[string]string{
		"12 days, 4 hours": "12d 4h",
		"3 minutes":        "3m",
		"1 week, 2 days":   "1w 2d",
		"":                 "",
	}
	for in, want := range tests {
		if got := CompactUptime(in); got != want {
			t.Errorf("CompactUptime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitize(t *testing.T) {
	in := "up \x1b[31m3\x1b[0m days\x07\r\nsecond line"
	got := sanitize(in)
	if strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\x07') || strings.ContainsRune(got, '\r') {
		t.Fatalf("sanitize left control characters: %q", got)
	}
	if !strings.Contains(got, "3 days") || !strings.Contains(got, "second line") {
		t.Fatalf("sanitize lost content: %q", got)
	}
}

func TestLimitedBuffer(t *testing.T) {
	var b limitedBuffer
	b.limit = 5
	n, err := b.Write([]byte("123456789"))
	if err != nil || n != 9 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if got := string(b.Bytes()); got != "12345" {
		t.Fatalf("got %q", got)
	}
}
