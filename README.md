# NCSF Sentinel

A small, fast terminal dashboard for keeping an eye on multiple remote Linux
servers over SSH.

```text
NCSF SENTINEL  v0.1.0                                                    23:58:14
● 4 ONLINE   ● 1 OFFLINE   ● 1 ERROR                        refresh in 18s
────────────────────────────────────────────────────────────────────────────────
  NAME                 TYPE      STATUS    UPTIME       MEM            DISK      LAT
▸ web-1                web       ● ONLINE  12d 4h       2.1G/7.8G 27%  18G/80G 23%  142ms
  vici-1               vicidial  ● OFFLINE 12d 4h       3.4G/15.6G 22% 31G/160G 19%  2m stale
  software-1           software  ● ONLINE  4d 2h        1.2G/3.9G 31%  9G/40G 22%  89ms
  web-2                web       ● ERROR   —            —              —          auth
────────────────────────────────────────────────────────────────────────────────
↑↓/jk move · enter details · r refresh · +/- interval · e edit config · ? help · q quit
```

Press `enter` on a row to open a full-screen detail view with uptime, memory,
disk usage, connection information and the last error. `esc` returns to the
table without losing the selection.

NCSF Sentinel is a single static Go binary. It talks to every server in
parallel, never blocks the UI on a slow host, and keeps the last known metrics
visible (dimmed) when a server goes offline.

## Features

- Table with one row per server, built for at-a-glance monitoring.
- Parallel SSH collection with a 5 second default timeout per server.
- Background refresh (30s by default) that never freezes rendering.
- Uptime, RAM and root filesystem usage for each host.
- Detail view and inline help.
- Responsive columns: low-priority columns disappear on narrow terminals
  instead of introducing horizontal scrolling.
- Last known metrics are retained and marked stale when a host is offline.
- Strict `known_hosts` verification by default, with a per-server opt-out.
- Private key files and `ssh-agent` authentication.
- Human-editable pipe-delimited config, saved atomically with `0600`
  permissions.
- Demo mode for exploring the UI without any servers.

## Requirements

- Go 1.27.3 or newer to build.
- Remote hosts running Linux with a POSIX shell and these standard tools:
  - `uptime -p` (falls back to `/proc/uptime`)
  - `free -m`
  - `df -P -h /`
- SSH key authentication (a private key file and/or `ssh-agent`). Password
  prompts are intentionally not supported in the dashboard.

## Install

```sh
make build
./bin/ncsf-sentinel --version
```

Install into `$GOBIN` (or `$GOPATH/bin`):

```sh
make install
```

For a static binary without make:

```sh
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X github.com/nebulacsf/ncsf-sentinel/internal/version.Version=0.1.0" \
  -o ncsf-sentinel .
```

## Quick start

```sh
# Create ~/.ncsf-sentinel/servers.conf
ncsf-sentinel init

# Add your hosts
ncsf-sentinel add --name web-1 --host 203.0.113.10 --user root \
  --key ~/.ssh/id_ed25519

ncsf-sentinel add --name vici-1 --type vicidial --host 198.51.100.20 \
  --user root --port 2222

# Check connectivity and metrics once
ncsf-sentinel test

# Open the dashboard
ncsf-sentinel
```

If a host has never been connected to before, its key will not be in
`known_hosts`. Connect once with the regular OpenSSH client first:

```sh
ssh root@203.0.113.10
```

Then NCSF Sentinel will trust the recorded key. The alternative is to opt out
of host key verification for that host with `insecure=1` (see below); this is
not recommended outside of a lab.

## Configuration

Default path: `~/.ncsf-sentinel/servers.conf`.
Override it with `--config path` or the `NCSF_SENTINEL_CONFIG` environment
variable.

The format is one server per line, fields separated by `|`:

```text
# name|type|host|user|port|identity|options
web-1|web|203.0.113.10|root|22|~/.ssh/id_ed25519|
vici-1|vicidial|198.51.100.20|root|2222||insecure=1,timeout=8s
```

| Field      | Required | Notes                                                        |
|------------|----------|--------------------------------------------------------------|
| `name`     | yes      | Unique; letters, digits, `.`, `_`, `-`.                      |
| `type`     | no       | Label shown in the UI (`web`, `vicidial`, `software`, ...).   |
| `host`     | yes      | Hostname or IP address.                                      |
| `user`     | yes      | SSH user.                                                    |
| `port`     | no       | Defaults to `22`.                                            |
| `identity` | no       | Private key path. `~` is expanded. Empty uses default keys and/or `ssh-agent`. |
| `options`  | no       | Comma-separated `key=value` pairs.                           |

Supported options:

| Option     | Example      | Description                                              |
|------------|--------------|----------------------------------------------------------|
| `timeout`  | `timeout=8s` | Per-server timeout. Overrides the global `--timeout`.     |
| `insecure` | `insecure=1` | Disable host key verification for this server only.      |

Comments start with `#`; blank lines are ignored. The file is rewritten in
canonical form when you use `add` or `remove`.

## Commands

```text
ncsf-sentinel [flags]                 Open the dashboard
ncsf-sentinel init [--force]          Create the config file
ncsf-sentinel add ...                 Add a server
ncsf-sentinel remove <name>           Remove a server
ncsf-sentinel list                    List configured servers
ncsf-sentinel test [name ...]         Check servers once; exit 3 on failure
ncsf-sentinel edit                    Open the config in $VISUAL/$EDITOR
ncsf-sentinel path                    Print the config path
ncsf-sentinel version                 Print the version
ncsf-sentinel help                    Show help
```

### Global flags

| Flag                  | Default                              | Description                         |
|-----------------------|--------------------------------------|-------------------------------------|
| `--config path`       | `~/.ncsf-sentinel/servers.conf`      | Config file.                        |
| `--interval duration` | `30s`                                | Background refresh interval.        |
| `--timeout duration`  | `5s`                                 | Per-server timeout.                 |
| `--parallel int`      | `16`                                 | Maximum concurrent SSH connections. |
| `--insecure-hostkey`  | `false`                              | Skip host key verification globally.|
| `--no-color`          | `false`                              | Disable colors.                     |
| `--demo`              | `false`                              | Simulated servers for UI testing.   |
| `--log path`          | none                                 | Append connection errors to a file. |
| `--version`           |                                      | Print the version.                  |

### `add` flags

```text
--name string        Server name (required)
--host string        Hostname or IP (required)
--user string        SSH user (defaults to $USER)
--port int           SSH port (default 22)
--type string        Label (default "web")
--key string         Private key path
--insecure           Disable host key verification for this server
--timeout duration   Per-server timeout (0 uses the global value)
```

`test` exits with status `3` when at least one server is not online, which makes
it usable from scripts and cron:

```sh
ncsf-sentinel test web-1 vici-1
```

## Keys

| Key              | Action                                           |
|------------------|--------------------------------------------------|
| `↑` / `↓`, `j` / `k` | Move selection.                              |
| `g` / `G`        | First / last server.                             |
| `enter`          | Open server details.                             |
| `esc`            | Back to the table.                               |
| `?`              | Toggle help.                                     |
| `r`              | Refresh now.                                     |
| `+` / `-`        | Change refresh interval by 5s (5s to 5m).        |
| `e`              | Edit the config, then reload it on exit.         |
| `q`, `ctrl+c`    | Quit.                                            |

## How it works

Each refresh fans out one goroutine per server through a bounded semaphore
(`--parallel`). Each collection opens a short-lived SSH connection, runs a
single read-only command, parses the output and closes the connection. Results
are delivered over a channel and merged back into the UI in server order.

The remote command is:

```sh
export LC_ALL=C
printf '\n@@NCSF_UPTIME@@\n'
uptime -p 2>/dev/null || cat /proc/uptime
printf '\n@@NCSF_FREE@@\n'
free -m
printf '\n@@NCSF_DF@@\n'
df -P -h /
```

Every server has its own deadline. A dead host or timeout is reported as
`OFFLINE` without delaying the rest of the dashboard. Authentication, host key,
command and parse failures are reported as `ERROR` with a short reason
(`auth`, `host key`, `parse`, `command`, ...).

The UI runs on [Bubble Tea](https://github.com/charmbracelet/bubbletea) and
[Lip Gloss](https://github.com/charmbracelet/lipgloss). It respects the
terminal's light/dark background. On narrow terminals, columns are removed in
this order: `TYPE`, `UPTIME`, `DISK`, `MEM`, `LAT`; `NAME` and `STATUS` are
always visible.

## Security notes

- Host keys are verified by default using `~/.ssh/known_hosts` and
  `/etc/ssh/ssh_known_hosts`.
- `insecure=1` (or `--insecure-hostkey`) disables that verification. Use it
  only for disposable lab machines.
- Passphrase-protected private keys are not prompted for. Load them into
  `ssh-agent` (`ssh-add ~/.ssh/id_ed25519`) or configure an unencrypted key.
- The config file is written with `0600` permissions inside a `0700` directory.
- The remote command is read-only and does not require `sudo`.

## Development

```sh
make test      # unit tests
make race      # tests with the race detector
make vet       # go vet
make build     # static binary in ./bin
```

The UI can be explored without SSH access:

```sh
go run . --demo
# or, after building:
make build && ./bin/ncsf-sentinel --demo
```

`--demo` uses deterministic simulated metrics and includes offline and failing
servers so stale-data rendering and the detail view can be checked easily. If
no config file exists, it seeds a sample fleet automatically.

## Limitations

- Linux metrics only.
- No password authentication, jump hosts or `~/.ssh/config` parsing yet.
- Each refresh uses a new SSH connection; it is designed for small fleets and
  low-frequency polling rather than high-frequency telemetry.
- The dashboard is read-only: it never changes remote state.
