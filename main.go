package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nebulacsf/ncsf-sentinel/internal/config"
	"github.com/nebulacsf/ncsf-sentinel/internal/monitor"
	"github.com/nebulacsf/ncsf-sentinel/internal/sshx"
	"github.com/nebulacsf/ncsf-sentinel/internal/ui"
	"github.com/nebulacsf/ncsf-sentinel/internal/version"
)

type options struct {
	configPath  string
	interval    time.Duration
	timeout     time.Duration
	parallel    int
	insecure    bool
	noColor     bool
	demo        bool
	logPath     string
	showVersion bool
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exit *exitError
		if errors.As(err, &exit) {
			if exit.err != nil {
				fmt.Fprintln(os.Stderr, "ncsf-sentinel:", exit.err)
			}
			os.Exit(exit.code)
		}
		fmt.Fprintln(os.Stderr, "ncsf-sentinel:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts := defaultOptions()

	flags := flag.NewFlagSet("ncsf-sentinel", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() { printUsage(os.Stderr) }
	flags.StringVar(&opts.configPath, "config", opts.configPath, "config file")
	flags.DurationVar(&opts.interval, "interval", opts.interval, "refresh interval")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "per-server timeout")
	flags.IntVar(&opts.parallel, "parallel", opts.parallel, "maximum concurrent SSH connections")
	flags.BoolVar(&opts.insecure, "insecure-hostkey", false, "skip SSH host key verification")
	flags.BoolVar(&opts.noColor, "no-color", false, "disable colors")
	flags.BoolVar(&opts.demo, "demo", false, "use simulated servers")
	flags.StringVar(&opts.logPath, "log", "", "write connection logs to a file")
	flags.BoolVar(&opts.showVersion, "version", false, "print version")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	rest := flags.Args()

	if opts.showVersion {
		fmt.Printf("ncsf-sentinel %s\n", version.Version)
		return nil
	}
	if len(rest) == 0 {
		return runTUI(opts)
	}

	switch rest[0] {
	case "init":
		return cmdInit(opts, rest[1:])
	case "add":
		return cmdAdd(opts, rest[1:])
	case "remove":
		return cmdRemove(opts, rest[1:])
	case "list":
		return cmdList(opts, rest[1:])
	case "test":
		return cmdTest(opts, rest[1:])
	case "edit":
		return cmdEdit(opts, rest[1:])
	case "path":
		return cmdPath(opts, rest[1:])
	case "version":
		fmt.Printf("ncsf-sentinel %s\n", version.Version)
		return nil
	case "help":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q (try 'ncsf-sentinel help')", rest[0])
	}
}

func defaultOptions() options {
	path := os.Getenv("NCSF_SENTINEL_CONFIG")
	if path == "" {
		if p, err := config.DefaultPath(); err == nil {
			path = p
		}
	}
	return options{
		configPath: path,
		interval:   30 * time.Second,
		timeout:    sshx.DefaultTimeout,
		parallel:   monitor.DefaultParallelism,
	}
}

func resolvePath(path string) string {
	if path != "" {
		return config.ExpandPath(path)
	}
	if p, err := config.DefaultPath(); err == nil {
		return p
	}
	return path
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) (bool, error) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func runTUI(opts options) error {
	path := resolvePath(opts.configPath)
	cfg, err := config.Load(path)
	var servers []config.Server
	var loadErr error
	if err != nil {
		loadErr = err
	} else {
		servers = cfg.Servers
	}
	if opts.demo && len(servers) == 0 {
		servers = monitor.DemoServers()
		loadErr = nil
	}

	var collector monitor.Collector
	if opts.demo {
		collector = monitor.DemoCollector{}
	} else {
		logger, file, err := openLogger(opts.logPath)
		if err != nil {
			return err
		}
		if file != nil {
			defer file.Close()
		}
		collector = sshx.New(sshx.Options{
			Timeout:  opts.timeout,
			Insecure: opts.insecure,
			Logger:   logger,
		})
	}
	if opts.noColor {
		_ = os.Setenv("NO_COLOR", "1")
	}

	model := ui.New(ui.Options{
		Servers:    servers,
		Collector:  collector,
		ConfigPath: path,
		LoadErr:    loadErr,
		Interval:   opts.interval,
		Timeout:    opts.timeout,
		Parallel:   opts.parallel,
		Insecure:   opts.insecure,
		Demo:       opts.demo,
	})
	program := tea.NewProgram(model)
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}

func cmdInit(opts options, args []string) error {
	fs := newFlagSet("init")
	configPath := fs.String("config", opts.configPath, "config file")
	force := fs.Bool("force", false, "overwrite an existing config")
	help, err := parseFlags(fs, args)
	if help || err != nil {
		return err
	}
	path := resolvePath(*configPath)
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("config already exists: %s (use --force to overwrite)", path)
	}
	cfg := &config.Config{Path: path}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Created %s\n", path)
	return nil
}

func cmdAdd(opts options, args []string) error {
	fs := newFlagSet("add")
	configPath := fs.String("config", opts.configPath, "config file")
	name := fs.String("name", "", "server name")
	host := fs.String("host", "", "hostname or IP")
	user := fs.String("user", currentUser(), "SSH user")
	port := fs.Int("port", 22, "SSH port")
	typ := fs.String("type", "web", "server type label (web, vicidial, software)")
	key := fs.String("key", "", "identity file")
	insecure := fs.Bool("insecure", false, "skip host key verification for this server")
	serverTimeout := fs.Duration("timeout", 0, "per-server timeout (0 uses the global timeout)")
	help, err := parseFlags(fs, args)
	if help || err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("--name is required")
	}
	if strings.TrimSpace(*host) == "" {
		return errors.New("--host is required")
	}

	path := resolvePath(*configPath)
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	srv := config.Server{
		Name:         *name,
		Type:         *typ,
		Host:         *host,
		User:         *user,
		Port:         *port,
		IdentityFile: *key,
		Options:      map[string]string{},
	}
	if *insecure {
		srv.Options["insecure"] = "1"
	}
	if *serverTimeout > 0 {
		srv.Options["timeout"] = serverTimeout.String()
	}
	if err := cfg.Add(srv); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Added %s (%s) to %s\n", srv.Name, srv.Addr(), path)
	return nil
}

func cmdRemove(opts options, args []string) error {
	fs := newFlagSet("remove")
	configPath := fs.String("config", opts.configPath, "config file")
	help, err := parseFlags(fs, args)
	if help || err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ncsf-sentinel remove <name>")
	}
	path := resolvePath(*configPath)
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	name := fs.Arg(0)
	if err := cfg.Remove(name); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Removed %s from %s\n", name, path)
	return nil
}

func cmdList(opts options, args []string) error {
	fs := newFlagSet("list")
	configPath := fs.String("config", opts.configPath, "config file")
	help, err := parseFlags(fs, args)
	if help || err != nil {
		return err
	}
	path := resolvePath(*configPath)
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if len(cfg.Servers) == 0 {
		fmt.Println("No servers configured.")
		fmt.Printf("Config: %s\n", path)
		return nil
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "NAME\tTYPE\tHOST\tUSER\tPORT\tIDENTITY\tOPTIONS")
	for _, srv := range cfg.Servers {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			srv.Name, srv.Type, srv.Host, srv.User, srv.Port,
			orDash(srv.IdentityFile), orDash(srv.OptionsString()))
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	fmt.Printf("\nConfig: %s\n", path)
	return nil
}

func cmdTest(opts options, args []string) error {
	fs := newFlagSet("test")
	configPath := fs.String("config", opts.configPath, "config file")
	timeout := fs.Duration("timeout", opts.timeout, "per-server timeout")
	parallel := fs.Int("parallel", opts.parallel, "maximum concurrent SSH connections")
	insecure := fs.Bool("insecure-hostkey", opts.insecure, "skip host key verification")
	logPath := fs.String("log", opts.logPath, "write connection logs to a file")
	help, err := parseFlags(fs, args)
	if help || err != nil {
		return err
	}

	path := resolvePath(*configPath)
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	servers := cfg.Servers
	if names := fs.Args(); len(names) > 0 {
		wanted := make(map[string]bool, len(names))
		for _, name := range names {
			wanted[name] = true
		}
		var filtered []config.Server
		for _, srv := range cfg.Servers {
			if wanted[srv.Name] {
				filtered = append(filtered, srv)
				delete(wanted, srv.Name)
			}
		}
		if len(wanted) > 0 {
			var missing []string
			for name := range wanted {
				missing = append(missing, name)
			}
			sort.Strings(missing)
			return fmt.Errorf("unknown server(s): %s", strings.Join(missing, ", "))
		}
		servers = filtered
	}
	if len(servers) == 0 {
		fmt.Println("No servers configured.")
		return nil
	}

	logger, file, err := openLogger(*logPath)
	if err != nil {
		return err
	}
	if file != nil {
		defer file.Close()
	}
	collector := sshx.New(sshx.Options{
		Timeout:  *timeout,
		Insecure: *insecure,
		Logger:   logger,
	})
	results := monitor.CollectAll(context.Background(), collector, servers, *parallel)

	failed := 0
	for _, result := range results {
		printResult(result)
		if result.Status != sshx.StatusOnline {
			failed++
		}
	}
	fmt.Printf("%d server(s) checked: %d online, %d failed\n", len(results), len(results)-failed, failed)
	if failed > 0 {
		return &exitError{code: 3}
	}
	return nil
}

func cmdEdit(opts options, args []string) error {
	fs := newFlagSet("edit")
	configPath := fs.String("config", opts.configPath, "config file")
	help, err := parseFlags(fs, args)
	if help || err != nil {
		return err
	}
	path := resolvePath(*configPath)
	if err := ensureConfigFile(path); err != nil {
		return err
	}
	fields := editorCommand()
	cmd := exec.Command(fields[0], append(fields[1:], path)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	return nil
}

func cmdPath(opts options, args []string) error {
	fs := newFlagSet("path")
	configPath := fs.String("config", opts.configPath, "config file")
	help, err := parseFlags(fs, args)
	if help || err != nil {
		return err
	}
	fmt.Println(resolvePath(*configPath))
	return nil
}

func printResult(result sshx.Result) {
	fmt.Printf("%-24s %-8s %s\n", result.Server.Name, result.Status.String(), latencyString(result.Latency))
	if result.Status == sshx.StatusOnline {
		mem := result.Metrics.Memory
		disk := result.Metrics.Disk
		fmt.Printf("  uptime   up %s\n", result.Metrics.Uptime)
		fmt.Printf("  memory   %d/%d MB (%.0f%%)\n", mem.UsedMB, mem.TotalMB, mem.UsedPct)
		fmt.Printf("  disk     %s / %s (%d%%)\n", disk.Used, disk.Size, disk.UsedPct)
	} else {
		if result.Reason != "" {
			fmt.Printf("  reason   %s\n", result.Reason)
		}
		if result.Err != nil {
			fmt.Printf("  error    %v\n", result.Err)
		}
	}
	fmt.Println()
}

func latencyString(d time.Duration) string {
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

func openLogger(path string) (*log.Logger, *os.File, error) {
	if path == "" {
		return nil, nil, nil
	}
	path = config.ExpandPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log: %w", err)
	}
	return log.New(file, "", log.LstdFlags), file, nil
}

func ensureConfigFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(config.Format(nil)), 0o600)
}

func editorCommand() []string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			if fields := strings.Fields(value); len(fields) > 0 {
				return fields
			}
		}
	}
	return []string{"vi"}
}

func currentUser() string {
	for _, name := range []string{"USER", "LOGNAME"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return "root"
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `NCSF Sentinel - real-time SSH server dashboard

Usage:
  ncsf-sentinel [flags]
  ncsf-sentinel <command> [flags]

Commands:
  init        Create a config file
  add         Add a server
  remove      Remove a server by name
  list        List configured servers
  test        Check servers once and print results (exit 3 on failure)
  edit        Open the config in $EDITOR
  path        Print the config file path
  version     Print the version
  help        Show this help

Global flags:
  --config path        Config file (default ~/.ncsf-sentinel/servers.conf)
  --interval duration  Refresh interval (default 30s)
  --timeout duration   Per-server timeout (default 5s)
  --parallel int       Maximum concurrent SSH connections (default 16)
  --insecure-hostkey   Skip host key verification (not recommended)
  --no-color           Disable colors
  --demo               Use simulated servers (development aid)
  --log path           Write connection logs to a file
  --version            Print the version

Examples:
  ncsf-sentinel init
  ncsf-sentinel add --name web-1 --host 203.0.113.10 --user root --key ~/.ssh/id_ed25519
  ncsf-sentinel add --name vici-1 --type vicidial --host 198.51.100.20 --user root
  ncsf-sentinel list
  ncsf-sentinel test
  ncsf-sentinel --interval 15s
  ncsf-sentinel --demo
`)
}
