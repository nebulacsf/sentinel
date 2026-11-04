package sshx

import (
	"errors"
	"math"
	"os/exec"
	"strconv"

	"github.com/nebulacsf/sentinel/internal/config"
)

// ShellCommand builds an interactive OpenSSH client invocation for srv. The
// returned command inherits the current environment, so ~/.ssh/config,
// SSH_AUTH_SOCK and interactive password/passphrase prompts keep working.
func ShellCommand(srv config.Server, opts Options) (*exec.Cmd, error) {
	path, err := exec.LookPath("ssh")
	if err != nil {
		return nil, errors.New("OpenSSH client not found (install the ssh client)")
	}
	return exec.Command(path, shellArgs(srv, opts)...), nil
}

func shellArgs(srv config.Server, opts Options) []string {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}

	port := srv.Port
	if port == 0 {
		port = 22
	}
	args := []string{"-t", "-p", strconv.Itoa(port)}

	if srv.IdentityFile != "" {
		args = append(args, "-i", config.ExpandPath(srv.IdentityFile))
	}

	if timeout := srv.Timeout(opts.Timeout); timeout > 0 {
		secs := int(math.Ceil(timeout.Seconds()))
		if secs < 1 {
			secs = 1
		}
		args = append(args, "-o", "ConnectTimeout="+strconv.Itoa(secs))
	}

	if opts.Insecure || srv.Insecure() {
		args = append(args,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
		)
	}

	return append(args, srv.User+"@"+srv.Host)
}
