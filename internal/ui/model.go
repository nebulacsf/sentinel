package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nebulacsf/sentinel/internal/config"
	"github.com/nebulacsf/sentinel/internal/monitor"
	"github.com/nebulacsf/sentinel/internal/sshx"
	"github.com/nebulacsf/sentinel/internal/version"
)

type screen int

const (
	screenTable screen = iota
	screenDetail
	screenHelp
)

// Options configures the Bubble Tea model.
type Options struct {
	Servers    []config.Server
	Collector  monitor.Collector
	ConfigPath string
	LoadErr    error
	Interval   time.Duration
	Timeout    time.Duration
	Parallel   int
	Insecure   bool
	Demo       bool
}

type serverState struct {
	Status      sshx.Status
	Metrics     sshx.Metrics
	HasMetrics  bool
	Err         error
	Reason      string
	Latency     time.Duration
	LastSuccess time.Time
	UpdatedAt   time.Time
}

// Model is the root Bubble Tea model.
type Model struct {
	ctx       context.Context
	cancel    context.CancelFunc
	collector monitor.Collector

	servers []config.Server
	states  map[string]serverState

	cursor      int
	offset      int
	screen      screen
	prevScreen  screen
	configPath  string
	loadErr     error
	editorError error
	terminalErr error

	interval    time.Duration
	timeout     time.Duration
	parallel    int
	insecure    bool
	demo        bool
	nextRefresh time.Time
	refreshing  bool
	gen         uint64
	lastRefresh time.Time

	width  int
	height int
	dark   bool
	now    time.Time
}

type tickMsg time.Time

type refreshDoneMsg struct {
	gen     uint64
	results []sshx.Result
	at      time.Time
}

type editorDoneMsg struct {
	err error
}

type terminalDoneMsg struct {
	server string
	err    error
}

// New creates the dashboard model. The first collection starts in Init.
func New(opts Options) Model {
	ctx, cancel := context.WithCancel(context.Background())
	interval := opts.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = sshx.DefaultTimeout
	}
	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = monitor.DefaultParallelism
	}

	m := Model{
		ctx:        ctx,
		cancel:     cancel,
		collector:  opts.Collector,
		servers:    append([]config.Server(nil), opts.Servers...),
		states:     make(map[string]serverState, len(opts.Servers)),
		configPath: opts.ConfigPath,
		loadErr:    opts.LoadErr,
		interval:   interval,
		timeout:    timeout,
		parallel:   parallel,
		insecure:   opts.Insecure,
		demo:       opts.Demo,
		width:      100,
		height:     30,
		dark:       true,
		now:        time.Now(),
	}
	for _, srv := range m.servers {
		m.states[srv.Name] = serverState{Status: sshx.StatusUnknown}
	}
	m.refreshing = len(m.servers) > 0 && m.collector != nil
	m.gen = 1
	m.nextRefresh = m.now.Add(interval)
	return m
}

// Init requests the terminal background color and starts the refresh loop.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.tickCmd(), m.refreshCmd())
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) refreshCmd() tea.Cmd {
	if len(m.servers) == 0 || m.collector == nil {
		return nil
	}
	gen := m.gen
	servers := append([]config.Server(nil), m.servers...)
	collector := m.collector
	parallel := m.parallel
	ctx := m.ctx
	return func() tea.Msg {
		results := monitor.CollectAll(ctx, collector, servers, parallel)
		return refreshDoneMsg{gen: gen, results: results, at: time.Now()}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureVisible()
		return m, nil

	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		cmds := []tea.Cmd{m.tickCmd()}
		if !m.refreshing && len(m.servers) > 0 && !m.nextRefresh.IsZero() && !m.now.Before(m.nextRefresh) {
			m.gen++
			m.refreshing = true
			m.nextRefresh = m.now.Add(m.interval)
			cmds = append(cmds, m.refreshCmd())
		}
		return m, tea.Batch(cmds...)

	case refreshDoneMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.refreshing = false
		m.lastRefresh = msg.at
		m.now = msg.at
		m.nextRefresh = msg.at.Add(m.interval)
		m.applyResults(msg.results)
		return m, nil

	case editorDoneMsg:
		return m.afterEditor(msg.err)

	case terminalDoneMsg:
		m.terminalErr = nil
		if msg.err != nil {
			m.terminalErr = fmt.Errorf("terminal %s: %w", msg.server, msg.err)
		}
		if len(m.servers) == 0 || m.collector == nil {
			return m, nil
		}
		m.gen++
		m.refreshing = true
		m.nextRefresh = time.Now().Add(m.interval)
		return m, m.refreshCmd()
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "Sentinel"
	return view
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "ctrl+c", "q":
		m.cancel()
		return m, tea.Quit
	case "?":
		if m.screen == screenHelp {
			m.screen = m.prevScreen
		} else {
			m.prevScreen = m.screen
			m.screen = screenHelp
		}
		return m, nil
	case "esc":
		switch m.screen {
		case screenHelp:
			m.screen = m.prevScreen
		case screenDetail:
			m.screen = screenTable
		}
		return m, nil
	case "r":
		return m.manualRefresh()
	case "+", "=", "-", "_":
		m.adjustInterval(key)
		return m, nil
	case "e":
		return m, m.editCmd()
	}

	if m.screen != screenTable && m.screen != screenDetail {
		return m, nil
	}

	switch key {
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-5)
	case "pgdown":
		m.moveCursor(5)
	case "home", "g":
		m.cursor = 0
		m.ensureVisible()
	case "end", "G":
		if len(m.servers) > 0 {
			m.cursor = len(m.servers) - 1
			m.ensureVisible()
		}
	case "enter":
		if m.screen == screenTable && len(m.servers) > 0 {
			m.screen = screenDetail
		}
	case "t":
		if m.screen == screenDetail {
			m.terminalErr = nil
			return m, m.terminalCmd()
		}
	}
	return m, nil
}

func (m Model) manualRefresh() (tea.Model, tea.Cmd) {
	if m.refreshing || len(m.servers) == 0 || m.collector == nil {
		return m, nil
	}
	m.gen++
	m.refreshing = true
	m.nextRefresh = time.Now().Add(m.interval)
	return m, m.refreshCmd()
}

func (m *Model) adjustInterval(key string) {
	delta := 5 * time.Second
	if key == "-" || key == "_" {
		delta = -delta
	}
	m.interval += delta
	if m.interval < 5*time.Second {
		m.interval = 5 * time.Second
	}
	if m.interval > 5*time.Minute {
		m.interval = 5 * time.Minute
	}
	m.nextRefresh = time.Now().Add(m.interval)
}

func (m *Model) moveCursor(delta int) {
	if len(m.servers) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.servers) {
		m.cursor = len(m.servers) - 1
	}
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	rows := m.visibleRows()
	if m.offset < 0 {
		m.offset = 0
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m Model) visibleRows() int {
	extra := 0
	if m.warning() != "" {
		extra = 1
	}
	rows := m.height - 7 - extra
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m *Model) applyResults(results []sshx.Result) {
	for _, result := range results {
		state := m.states[result.Server.Name]
		state.Status = result.Status
		state.Err = result.Err
		state.Reason = result.Reason
		state.Latency = result.Latency
		state.UpdatedAt = result.CollectedAt
		if result.Status == sshx.StatusOnline || result.Metrics.Uptime != "" ||
			result.Metrics.Memory.TotalMB > 0 || result.Metrics.Disk.Size != "" {
			state.Metrics = result.Metrics
			state.HasMetrics = true
		}
		if result.Status == sshx.StatusOnline {
			state.LastSuccess = result.CollectedAt
		}
		m.states[result.Server.Name] = state
	}
}

func (m Model) state(name string) serverState {
	if state, ok := m.states[name]; ok {
		return state
	}
	return serverState{Status: sshx.StatusUnknown}
}

func (m Model) warning() string {
	if m.terminalErr != nil {
		return m.terminalErr.Error()
	}
	if m.editorError != nil {
		return m.editorError.Error()
	}
	if m.loadErr != nil {
		return "config: " + m.loadErr.Error()
	}
	return ""
}

func (m Model) afterEditor(err error) (tea.Model, tea.Cmd) {
	if err != nil {
		m.editorError = fmt.Errorf("editor: %w", err)
		return m, nil
	}

	cfg, loadErr := config.Load(m.configPath)
	if loadErr != nil {
		m.loadErr = loadErr
		m.editorError = loadErr
		return m, nil
	}

	states := make(map[string]serverState, len(cfg.Servers))
	for _, srv := range cfg.Servers {
		if state, ok := m.states[srv.Name]; ok {
			states[srv.Name] = state
		} else {
			states[srv.Name] = serverState{Status: sshx.StatusUnknown}
		}
	}
	m.states = states
	m.servers = cfg.Servers
	m.loadErr = nil
	m.editorError = nil
	m.refreshing = false
	m.gen++

	if m.cursor >= len(m.servers) {
		m.cursor = len(m.servers) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.ensureVisible()

	if len(m.servers) == 0 || m.collector == nil {
		m.nextRefresh = time.Time{}
		return m, nil
	}
	m.refreshing = true
	m.nextRefresh = time.Now().Add(m.interval)
	return m, m.refreshCmd()
}

func (m Model) terminalCmd() tea.Cmd {
	if m.demo {
		return func() tea.Msg {
			return terminalDoneMsg{err: errors.New("terminal is disabled in demo mode")}
		}
	}
	if len(m.servers) == 0 {
		return nil
	}
	index := m.cursor
	if index < 0 {
		index = 0
	}
	if index >= len(m.servers) {
		index = len(m.servers) - 1
	}
	srv := m.servers[index]
	cmd, err := sshx.ShellCommand(srv, sshx.Options{
		Timeout:  m.timeout,
		Insecure: m.insecure,
	})
	if err != nil {
		return func() tea.Msg {
			return terminalDoneMsg{server: srv.Name, err: err}
		}
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return terminalDoneMsg{server: srv.Name, err: err}
	})
}

func (m Model) editCmd() tea.Cmd {
	if m.configPath == "" {
		return func() tea.Msg {
			return editorDoneMsg{err: errors.New("config path is not set")}
		}
	}
	fields := editorCommand()
	if len(fields) == 0 {
		return func() tea.Msg {
			return editorDoneMsg{err: errors.New("no editor configured; set $EDITOR")}
		}
	}
	if err := ensureConfigFile(m.configPath); err != nil {
		return func() tea.Msg {
			return editorDoneMsg{err: err}
		}
	}
	args := append(fields[1:], m.configPath)
	cmd := exec.Command(fields[0], args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{err: err}
	})
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

func (m Model) summary(th theme) string {
	counts := map[sshx.Status]int{}
	for _, srv := range m.servers {
		counts[m.state(srv.Name).Status]++
	}
	type item struct {
		status sshx.Status
		label  string
	}
	items := []item{
		{sshx.StatusOnline, "ONLINE"},
		{sshx.StatusOffline, "OFFLINE"},
		{sshx.StatusError, "ERROR"},
		{sshx.StatusUnknown, "LOADING"},
	}
	var parts []string
	for _, it := range items {
		if counts[it.status] == 0 {
			continue
		}
		parts = append(parts, th.status(it.status).Render(fmt.Sprintf("● %d %s", counts[it.status], it.label)))
	}
	if len(parts) == 0 {
		return th.dim.Render("no servers")
	}
	return strings.Join(parts, "   ")
}

func (m Model) refreshStatus(th theme) string {
	if m.refreshing {
		return th.accent.Render("refreshing…")
	}
	if len(m.servers) == 0 {
		return ""
	}
	return th.muted.Render("refresh in " + shortDuration(m.nextRefresh.Sub(m.now)))
}

func (m Model) headerLines(th theme) []string {
	width := m.width - 4
	title := th.accent.Bold(true).Render("SENTINEL") + " " + th.dim.Render(version.Version)
	clock := th.muted.Render(m.now.Format("15:04:05"))
	line1 := "  " + spread(title, clock, width) + "  "
	line2 := "  " + spread(m.summary(th), m.refreshStatus(th), width) + "  "
	return []string{line1, line2, ""}
}

func (m Model) footerText() string {
	if m.width < 70 {
		return "↑↓ move · enter details · ? help · q quit"
	}
	return "↑↓/jk move · enter details · r refresh · +/- interval · e edit config · ? help · q quit"
}

func (m Model) render() string {
	if m.width <= 0 {
		m.width = 80
	}
	if m.height <= 0 {
		m.height = 24
	}
	if m.width < 40 {
		return "Terminal too narrow (minimum 40 columns)."
	}
	switch m.screen {
	case screenDetail:
		return m.renderDetail()
	case screenHelp:
		return m.renderHelp()
	default:
		return m.renderTable()
	}
}
