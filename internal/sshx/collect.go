package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/nebulacsf/ncsf-sentinel/internal/config"
)

const (
	// DefaultTimeout is the maximum time spent on one server per refresh.
	DefaultTimeout = 5 * time.Second

	outputLimit = 64 * 1024
	stderrLimit = 16 * 1024

	remoteCommand = `export LC_ALL=C
printf '\n@@NCSF_UPTIME@@\n'
uptime -p 2>/dev/null || cat /proc/uptime
printf '\n@@NCSF_FREE@@\n'
free -m
printf '\n@@NCSF_DF@@\n'
df -P -h /`
)

// Options configures the SSH collector.
type Options struct {
	Timeout     time.Duration
	Insecure    bool
	KnownHosts  []string
	AgentSocket string
	Logger      *log.Logger
}

// Collector collects metrics over short-lived SSH connections.
type Collector struct {
	opts Options
}

// New returns a collector with sensible defaults applied.
func New(opts Options) *Collector {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	return &Collector{opts: opts}
}

// Collect connects to one server, runs the read-only metric command and parses
// the result. It never returns an error to the caller: failures are encoded in
// Result so one bad host cannot stop the rest of the dashboard.
func (c *Collector) Collect(parent context.Context, srv config.Server) Result {
	started := time.Now()
	timeout := srv.Timeout(c.opts.Timeout)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	res := Result{Server: srv, Status: StatusOffline, CollectedAt: started}

	methods, closeAuth, err := c.authMethods(srv)
	if closeAuth != nil {
		defer closeAuth()
	}
	if err != nil {
		return classify(res, fmt.Errorf("authentication setup: %w", err), timeout, started, "auth")
	}

	client, err := c.dial(ctx, srv, methods)
	if err != nil {
		c.logf("dial %s: %v", srv.Name, err)
		return classify(res, err, timeout, started, "")
	}
	defer client.Close()

	stdout, stderr, err := c.run(ctx, client)
	res.Latency = time.Since(started)
	res.CollectedAt = time.Now()
	if err != nil {
		c.logf("run %s: %v", srv.Name, err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			res.Status = StatusOffline
			res.Reason = "timeout"
			res.Err = fmt.Errorf("timeout after %s", timeout)
			return res
		}
		if detail := strings.TrimSpace(sanitize(string(stderr))); detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
		res.Status = StatusError
		res.Reason = "command"
		res.Err = err
		return res
	}

	metrics, err := ParseOutput(string(stdout))
	if err != nil {
		res.Status = StatusError
		res.Reason = "parse"
		res.Err = err
		return res
	}
	res.Status = StatusOnline
	res.Metrics = metrics
	return res
}

func (c *Collector) logf(format string, args ...any) {
	if c.opts.Logger != nil {
		c.opts.Logger.Printf(format, args...)
	}
}

func (c *Collector) dial(ctx context.Context, srv config.Server, methods []ssh.AuthMethod) (*ssh.Client, error) {
	callback, err := c.hostKeyCallback(srv)
	if err != nil {
		return nil, err
	}
	timeout := srv.Timeout(c.opts.Timeout)
	cfg := &ssh.ClientConfig{
		User:            srv.User,
		Auth:            methods,
		HostKeyCallback: callback,
		Timeout:         timeout,
	}

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", srv.Addr())
	if err != nil {
		return nil, err
	}

	type handshake struct {
		conn  ssh.Conn
		chans <-chan ssh.NewChannel
		reqs  <-chan *ssh.Request
		err   error
	}
	done := make(chan handshake, 1)
	go func() {
		cc, chans, reqs, err := ssh.NewClientConn(conn, srv.Addr(), cfg)
		done <- handshake{cc, chans, reqs, err}
	}()

	select {
	case <-ctx.Done():
		_ = conn.Close()
		return nil, ctx.Err()
	case hs := <-done:
		if hs.err != nil {
			_ = conn.Close()
			return nil, hs.err
		}
		return ssh.NewClient(hs.conn, hs.chans, hs.reqs), nil
	}
}

func (c *Collector) run(ctx context.Context, client *ssh.Client) ([]byte, []byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	defer session.Close()

	var stdout, stderr limitedBuffer
	stdout.limit = outputLimit
	stderr.limit = stderrLimit
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- session.Run(remoteCommand)
	}()

	select {
	case <-ctx.Done():
		_ = client.Close()
		return nil, nil, ctx.Err()
	case err := <-done:
		return stdout.Bytes(), stderr.Bytes(), err
	}
}

func (c *Collector) authMethods(srv config.Server) ([]ssh.AuthMethod, func(), error) {
	var methods []ssh.AuthMethod
	var closers []func()
	closeAll := func() {
		for _, closer := range closers {
			closer()
		}
	}

	passphraseOnly := false
	passphrasePath := ""
	load := func(path, label string, required bool) error {
		signer, err := loadSigner(path)
		if err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				passphraseOnly = true
				passphrasePath = path
				c.logf("%s: key is passphrase protected; relying on ssh-agent", label)
				return nil
			}
			if required {
				return fmt.Errorf("%s: %w", label, err)
			}
			c.logf("%s: %v", label, err)
			return nil
		}
		methods = append(methods, ssh.PublicKeys(signer))
		return nil
	}

	if srv.IdentityFile != "" {
		if err := load(config.ExpandPath(srv.IdentityFile), "identity", true); err != nil {
			return nil, closeAll, err
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			path := filepath.Join(home, ".ssh", name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			if err := load(path, name, false); err != nil {
				return nil, closeAll, err
			}
		}
	}

	socket := c.opts.AgentSocket
	if socket == "" {
		socket = os.Getenv("SSH_AUTH_SOCK")
	}
	if socket != "" {
		conn, err := net.DialTimeout("unix", socket, 2*time.Second)
		if err != nil {
			c.logf("ssh-agent: %v", err)
		} else {
			client := agent.NewClient(conn)
			methods = append(methods, ssh.PublicKeysCallback(client.Signers))
			closers = append(closers, func() { _ = conn.Close() })
		}
	}

	if len(methods) == 0 {
		msg := "no usable SSH authentication methods (checked identity, default keys and ssh-agent)"
		if passphraseOnly {
			msg = "the SSH key is passphrase protected; add it to ssh-agent with 'ssh-add " + passphrasePath + "'"
		}
		return nil, closeAll, errors.New(msg)
	}
	return methods, closeAll, nil
}

func loadSigner(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

func (c *Collector) hostKeyCallback(srv config.Server) (ssh.HostKeyCallback, error) {
	if c.opts.Insecure || srv.Insecure() {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	files := make([]string, 0, len(c.opts.KnownHosts)+2)
	for _, path := range c.opts.KnownHosts {
		files = append(files, config.ExpandPath(path))
	}
	if len(files) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			files = append(files, filepath.Join(home, ".ssh", "known_hosts"))
		}
		files = append(files, "/etc/ssh/ssh_known_hosts")
	}

	existing := make([]string, 0, len(files))
	for _, path := range files {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			existing = append(existing, path)
		}
	}
	if len(existing) == 0 {
		return nil, fmt.Errorf("no known_hosts file found; connect once with 'ssh %s@%s' or set insecure=1 for this server", srv.User, srv.Host)
	}
	return knownhosts.New(existing...)
}

func classify(res Result, err error, timeout time.Duration, started time.Time, forcedReason string) Result {
	res.Latency = time.Since(started)
	res.CollectedAt = time.Now()
	res.Err = err

	var hostKeyErr *knownhosts.KeyError
	if errors.As(err, &hostKeyErr) {
		res.Status = StatusError
		res.Reason = "host key"
		if len(hostKeyErr.Want) == 0 {
			res.Err = fmt.Errorf("host key for %s is not in known_hosts; connect once with ssh or set insecure=1", res.Server.Host)
		} else {
			res.Err = fmt.Errorf("host key mismatch for %s: possible MITM or server reinstall", res.Server.Host)
		}
		return res
	}

	var passphrase *ssh.PassphraseMissingError
	if errors.As(err, &passphrase) {
		res.Status = StatusError
		res.Reason = "auth"
		return res
	}

	if forcedReason != "" {
		res.Status = StatusError
		res.Reason = forcedReason
		return res
	}

	if isTimeout(err) {
		res.Status = StatusOffline
		res.Reason = "timeout"
		res.Err = fmt.Errorf("timeout after %s", timeout)
		return res
	}

	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, syscall.ECONNREFUSED), strings.Contains(msg, "connection refused"):
		res.Status = StatusOffline
		res.Reason = "refused"
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "server misbehaving"):
		res.Status = StatusOffline
		res.Reason = "dns"
	case strings.Contains(msg, "network is unreachable"), strings.Contains(msg, "no route to host"):
		res.Status = StatusOffline
		res.Reason = "unreachable"
	case strings.Contains(msg, "known_hosts"), strings.Contains(msg, "knownhosts"):
		res.Status = StatusError
		res.Reason = "host key"
	case strings.Contains(msg, "unable to authenticate"), strings.Contains(msg, "no supported methods remain"):
		res.Status = StatusError
		res.Reason = "auth"
	case strings.Contains(msg, "handshake failed"):
		res.Status = StatusError
		res.Reason = "handshake"
	default:
		res.Status = StatusError
		res.Reason = "ssh"
	}
	return res
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "i/o timeout")
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.limit > 0 {
		remain := b.limit - b.buf.Len()
		if remain <= 0 {
			return n, nil
		}
		if len(p) > remain {
			p = p[:remain]
		}
	}
	_, _ = b.buf.Write(p)
	return n, nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}
