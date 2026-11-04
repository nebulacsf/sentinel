package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	input := `
# comment
nebula-web|web|45.55.215.238|root|22|~/.ssh/id_ed25519|
ocean-vici|vicidial|68.183.49.220|root|2222|~/.ssh/ocean|timeout=8s,insecure=1
lab||192.168.0.10|jose|||
`
	servers, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(servers) != 3 {
		t.Fatalf("got %d servers, want 3", len(servers))
	}
	if servers[0].Type != "web" || servers[0].Port != 22 {
		t.Errorf("unexpected defaults: %+v", servers[0])
	}
	if got := servers[1].Timeout(5 * time.Second); got != 8*time.Second {
		t.Errorf("timeout = %v, want 8s", got)
	}
	if !servers[1].Insecure() {
		t.Error("expected insecure option")
	}
	if servers[2].Type != "web" || servers[2].Port != 22 {
		t.Errorf("empty optional fields not defaulted: %+v", servers[2])
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"too few fields", "a|web|host|root", "expected 6 or 7 fields"},
		{"too many fields", "a|web|host|root|22|key|opts|extra", "expected 6 or 7 fields"},
		{"bad port", "a|web|host|root|nope|key|", "invalid port"},
		{"bad name", "bad name|web|host|root|22|key|", "invalid server name"},
		{"missing host", "a|web||root|22|key|", "host is required"},
		{"missing user", "a|web|host||22|key|", "user is required"},
		{"bad option", "a|web|host|root|22|key|nope", "expected key=value"},
		{"bad timeout", "a|web|host|root|22|key|timeout=soon", "invalid timeout option"},
		{"duplicate", "a|web|h|u|22|k|\na|web|h2|u|22|k|", "duplicate server"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	in := []Server{
		{
			Name:         "web-1",
			Type:         "web",
			Host:         "203.0.113.10",
			User:         "root",
			Port:         22,
			IdentityFile: "~/.ssh/id_ed25519",
			Options:      map[string]string{"insecure": "1", "timeout": "7s"},
		},
		{
			Name:    "vici-1",
			Type:    "vicidial",
			Host:    "198.51.100.20",
			User:    "admin",
			Port:    2222,
			Options: map[string]string{},
		},
	}
	out := Format(in)
	got, err := Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(in, got) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, in)
	}
}

func TestSaveLoadPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "servers.conf")
	cfg := &Config{Path: path, Servers: []Server{{
		Name: "web-1", Type: "web", Host: "203.0.113.10", User: "root", Port: 22,
	}}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Servers) != 1 || loaded.Servers[0].Name != "web-1" {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.conf"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected empty config, got %+v", cfg.Servers)
	}
}

func TestAddRemove(t *testing.T) {
	cfg := &Config{}
	s := Server{Name: "a", Host: "h", User: "u"}
	if err := cfg.Add(s); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := cfg.Add(s); err == nil {
		t.Fatal("expected duplicate error")
	}
	if _, ok := cfg.Find("a"); !ok {
		t.Fatal("Find failed")
	}
	if err := cfg.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := cfg.Remove("a"); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandPath("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("ExpandPath(~/x) = %q", got)
	}
	if got := ExpandPath("/abs"); got != "/abs" {
		t.Errorf("ExpandPath(/abs) = %q", got)
	}
}
