// Package config loads and saves the NCSF Sentinel server list.
//
// The on-disk format is deliberately simple and human editable:
//
//	# name|type|host|user|port|identity|options
//	web-1|web|203.0.113.10|root|22|~/.ssh/id_ed25519|
//
// Options are comma separated key=value pairs, for example:
//
//	timeout=8s,insecure=1
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// DirName is the directory under the user's home used for all state.
	DirName = ".ncsf-sentinel"
	// FileName is the default server list file.
	FileName = "servers.conf"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Server describes one monitored host.
type Server struct {
	Name         string
	Type         string
	Host         string
	User         string
	Port         int
	IdentityFile string
	Options      map[string]string
}

// Config is a server list plus the path it was loaded from.
type Config struct {
	Path    string
	Servers []Server
}

// DefaultPath returns ~/.ncsf-sentinel/servers.conf.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, DirName, FileName), nil
}

// ExpandPath expands a leading "~" or "~/" using the current user's home.
func ExpandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

// Normalize fills in defaults and validates the server.
func (s *Server) Normalize() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Type = strings.ToLower(strings.TrimSpace(s.Type))
	s.Host = strings.TrimSpace(s.Host)
	s.User = strings.TrimSpace(s.User)
	s.IdentityFile = strings.TrimSpace(s.IdentityFile)
	if s.Options == nil {
		s.Options = map[string]string{}
	}
	if s.Type == "" {
		s.Type = "web"
	}
	if s.Port == 0 {
		s.Port = 22
	}
	if s.Name == "" {
		return errors.New("server name is required")
	}
	if !validName.MatchString(s.Name) {
		return fmt.Errorf("invalid server name %q (use letters, digits, '.', '_' or '-')", s.Name)
	}
	if s.Host == "" {
		return errors.New("host is required")
	}
	if s.User == "" {
		return errors.New("user is required")
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("invalid port %d", s.Port)
	}
	if raw := strings.TrimSpace(s.Options["timeout"]); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid timeout option %q", raw)
		}
	}
	return nil
}

// Addr returns host:port suitable for net.Dial.
func (s Server) Addr() string {
	port := s.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(s.Host, strconv.Itoa(port))
}

// Option returns an option value, or "" when unset.
func (s Server) Option(key string) string {
	return s.Options[strings.ToLower(key)]
}

// Timeout returns the per-server timeout override or def.
func (s Server) Timeout(def time.Duration) time.Duration {
	raw := strings.TrimSpace(s.Option("timeout"))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// Insecure reports whether host key verification is disabled for this server.
func (s Server) Insecure() bool {
	switch strings.ToLower(strings.TrimSpace(s.Option("insecure"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// OptionsString serializes options deterministically.
func (s Server) OptionsString() string {
	if len(s.Options) == 0 {
		return ""
	}
	keys := make([]string, 0, len(s.Options))
	for k := range s.Options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+s.Options[k])
	}
	return strings.Join(parts, ",")
}

// Load reads a config file. A missing file is not an error: it yields an empty
// config so the TUI can show an onboarding message.
func Load(path string) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	path = ExpandPath(path)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{Path: path}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	servers, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Config{Path: path, Servers: servers}, nil
}

// Parse reads the pipe separated server list.
func Parse(r io.Reader) ([]Server, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var servers []Server
	seen := make(map[string]int)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 6 || len(parts) > 7 {
			return nil, fmt.Errorf("line %d: expected 6 or 7 fields separated by '|', got %d", lineNo, len(parts))
		}
		port := 22
		if raw := strings.TrimSpace(parts[4]); raw != "" {
			p, err := strconv.Atoi(raw)
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid port %q", lineNo, raw)
			}
			port = p
		}
		srv := Server{
			Name:         strings.TrimSpace(parts[0]),
			Type:         strings.TrimSpace(parts[1]),
			Host:         strings.TrimSpace(parts[2]),
			User:         strings.TrimSpace(parts[3]),
			Port:         port,
			IdentityFile: strings.TrimSpace(parts[5]),
		}
		if len(parts) == 7 {
			opts, err := parseOptions(parts[6])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			srv.Options = opts
		}
		if err := srv.Normalize(); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if first, ok := seen[srv.Name]; ok {
			return nil, fmt.Errorf("line %d: duplicate server %q (first defined at line %d)", lineNo, srv.Name, first)
		}
		seen[srv.Name] = lineNo
		servers = append(servers, srv)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return servers, nil
}

func parseOptions(raw string) (map[string]string, error) {
	opts := make(map[string]string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return opts, nil
	}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("invalid option %q (expected key=value)", item)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return nil, errors.New("empty option key")
		}
		opts[key] = strings.TrimSpace(value)
	}
	return opts, nil
}

// Format renders the canonical on-disk representation.
func Format(servers []Server) string {
	var b strings.Builder
	b.WriteString("# NCSF Sentinel - remote servers\n")
	b.WriteString("# name|type|host|user|port|identity|options\n")
	for _, s := range servers {
		if err := s.Normalize(); err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s|%s|%s|%s|%d|%s|%s\n",
			s.Name, s.Type, s.Host, s.User, s.Port, s.IdentityFile, s.OptionsString())
	}
	return b.String()
}

// Save writes the config atomically with 0600 permissions.
func (c *Config) Save() error {
	if c.Path == "" {
		return errors.New("config path is empty")
	}
	c.Path = ExpandPath(c.Path)
	seen := make(map[string]bool, len(c.Servers))
	for i := range c.Servers {
		if err := c.Servers[i].Normalize(); err != nil {
			return fmt.Errorf("server %q: %w", c.Servers[i].Name, err)
		}
		if seen[c.Servers[i].Name] {
			return fmt.Errorf("duplicate server %q", c.Servers[i].Name)
		}
		seen[c.Servers[i].Name] = true
	}
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".servers.conf-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set config permissions: %w", err)
	}
	if _, err := io.WriteString(tmp, Format(c.Servers)); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tmpName, c.Path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// Find returns the server with the given name.
func (c *Config) Find(name string) (Server, bool) {
	for _, s := range c.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return Server{}, false
}

// Add validates and appends a server without saving.
func (c *Config) Add(s Server) error {
	if err := s.Normalize(); err != nil {
		return err
	}
	if _, ok := c.Find(s.Name); ok {
		return fmt.Errorf("server %q already exists", s.Name)
	}
	c.Servers = append(c.Servers, s)
	return nil
}

// Remove deletes a server by name without saving.
func (c *Config) Remove(name string) error {
	for i, s := range c.Servers {
		if s.Name == name {
			c.Servers = append(c.Servers[:i], c.Servers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("server %q not found", name)
}
