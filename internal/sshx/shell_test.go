package sshx

import (
	"slices"
	"testing"
	"time"

	"github.com/nebulacsf/ncsf-sentinel/internal/config"
)

func TestShellArgs(t *testing.T) {
	tests := []struct {
		name   string
		server config.Server
		opts   Options
		want   []string
	}{
		{
			name:   "defaults",
			server: config.Server{Host: "203.0.113.10", User: "root", Port: 22, Options: map[string]string{}},
			opts:   Options{Timeout: 5 * time.Second},
			want:   []string{"-t", "-p", "22", "-o", "ConnectTimeout=5", "root@203.0.113.10"},
		},
		{
			name: "identity and per-server timeout",
			server: config.Server{
				Host: "198.51.100.20", User: "deploy", Port: 2222,
				IdentityFile: "/tmp/id_ed25519",
				Options:      map[string]string{"timeout": "8s"},
			},
			opts: Options{Timeout: 5 * time.Second},
			want: []string{
				"-t", "-p", "2222",
				"-i", "/tmp/id_ed25519",
				"-o", "ConnectTimeout=8",
				"deploy@198.51.100.20",
			},
		},
		{
			name:   "zero port falls back to 22",
			server: config.Server{Host: "192.0.2.30", User: "root"},
			opts:   Options{Timeout: 0},
			want:   []string{"-t", "-p", "22", "-o", "ConnectTimeout=5", "root@192.0.2.30"},
		},
		{
			name:   "sub-second timeout rounds up",
			server: config.Server{Host: "192.0.2.30", User: "root", Port: 22, Options: map[string]string{}},
			opts:   Options{Timeout: 1500 * time.Millisecond},
			want:   []string{"-t", "-p", "22", "-o", "ConnectTimeout=2", "root@192.0.2.30"},
		},
		{
			name: "per-server insecure option",
			server: config.Server{
				Host: "203.0.113.10", User: "root", Port: 22,
				Options: map[string]string{"insecure": "1"},
			},
			opts: Options{Timeout: 5 * time.Second},
			want: []string{
				"-t", "-p", "22", "-o", "ConnectTimeout=5",
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"root@203.0.113.10",
			},
		},
		{
			name:   "global insecure option",
			server: config.Server{Host: "203.0.113.10", User: "root", Port: 22, Options: map[string]string{}},
			opts:   Options{Timeout: 5 * time.Second, Insecure: true},
			want: []string{
				"-t", "-p", "22", "-o", "ConnectTimeout=5",
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"root@203.0.113.10",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellArgs(tt.server, tt.opts)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("shellArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShellCommandMissingClient(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := ShellCommand(config.Server{Host: "h", User: "u", Port: 22}, Options{}); err == nil {
		t.Fatal("expected an error when the OpenSSH client is not installed")
	}
}
