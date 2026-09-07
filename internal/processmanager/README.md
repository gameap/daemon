# Process Managers

This package provides process manager implementations for managing game server lifecycles. Each process manager implements the `contracts.ProcessManager` interface.

## Available Process Managers

| Name      | Platforms    | Description                                    |
|-----------|--------------|------------------------------------------------|
| `tmux`    | Linux, macOS | Terminal multiplexer-based process management  |
| `systemd` | Linux        | Systemd service-based process management       |
| `simple`  | All          | Basic script-based process management          |
| `winsw`   | Windows      | Windows Service Wrapper                        |
| `shawl`   | Windows      | Windows service wrapper for arbitrary programs |
| `docker`  | All          | Docker container-based process management      |
| `podman`  | Linux, macOS | Podman container-based process management      |

## Who restarts a crashed server

A game server that dies can be brought back either by the process manager or by
the daemon's own servers loop, and exactly one of them must own that decision.
Both honour the server's `autostart` setting from the panel: a server the
operator asked to stay down is never resurrected.

| Process manager                      | Restarted by        | Mechanism when `autostart` is on                   | When it is off |
|--------------------------------------|---------------------|----------------------------------------------------|----------------|
| `systemd`                            | the unit            | `Restart=always`, `RestartSec`, raised start limit | `Restart=no`   |
| `shawl`                              | the supervisor      | `--restart`                                        | `--no-restart` |
| `winsw`                              | the service manager | `onfailure` restart actions                        | no actions     |
| `tmux`, `simple`, `docker`, `podman` | the daemon          | the servers loop, with a growing delay             | never started  |

The restart policy is written from the persistent `autostart` setting, never from
`autostart_current`. The latter also tracks whether the server is *currently*
meant to be running and is 0 for the whole duration of a deliberate stop, so
reading it would strip supervision from the unit on every stop/start cycle.

Where the process manager supervises restarts, the daemon stays out of the way
while the supervisor is working: `Status` reports a systemd unit in
`activating`/`auto-restart` as running, so the loop does not race it. The loop
takes over only once the supervisor has given up and left the unit inactive or
failed — and because a unit that exhausted its start limit refuses every further
start, the daemon runs `systemctl reset-failed` before starting one.

A status check that cannot be evaluated — a probe killed by its own deadline, an
unreachable container runtime — is reported as undetermined rather than as a
stopped server. The loop leaves such a server alone: treating an unreadable probe
as "down" would restart a healthy server.

## Metrics support

The `Metrics(ctx, server)` method returns Prometheus-style samples (see
`internal/app/metrics`) that the daemon's metrics collector aggregates and
forwards to the panel via gRPC.

| PM                                    | `gameap_server_up` | `gameap_server_cpu_usage_percent` | `gameap_server_memory_*` | `gameap_server_network_*_bytes_total` | `gameap_server_block_io_*_bytes_total` | `gameap_server_process_pids` |
|---------------------------------------|:------------------:|:---------------------------------:|:------------------------:|:-------------------------------------:|:--------------------------------------:|:----------------------------:|
| `docker`                              |        yes         |                yes                |           yes            |                  yes                  |              yes (Linux)               |             yes              |
| `podman`                              |        yes         |                yes                |           yes            |                  yes                  |                  yes                   |             yes              |
| `systemd`                             |        yes         |                yes                |           yes            |                  yes                  |                  yes                   |             yes              |
| `tmux` / `simple` / `winsw` / `shawl` |        yes         |                 —                 |            —             |                   —                   |                   —                    |              —               |

Container-backed managers tag their metrics with `{server_id, server_uuid, container}`.
The systemd manager tags its metrics with `{server_id, server_uuid, service}`.

The systemd manager reads metrics from `systemctl show` and relies on the
`CPUAccounting=yes`, `MemoryAccounting=yes`, `IOAccounting=yes`,
`IPAccounting=yes` and `TasksAccounting=yes` directives that the daemon
writes into every generated unit file. Game servers running on units
created before these directives existed will report zeros (and a suppressed
CPU%) until the next start/restart regenerates the unit. Metrics are also
suppressed for the first sample after each restart, since the cumulative
CPU counter has no baseline yet.

PID-based stats for `tmux` / `simple` / `winsw` / `shawl` are tracked as a follow-up.

## SystemD scopes

The `systemd` backend supports two scopes selected via
`process_manager.config.scope`:

- `system` (default): writes units to `/etc/systemd/system` and calls
  `systemctl <action>`. Requires the daemon to run as root or to have
  polkit rules permitting unit management. Each generated `.service`
  carries `User=` / `Group=` derived from the panel-side `server.user`
  field, so the daemon can host multiple servers under different
  identities. `[Install] WantedBy=multi-user.target`.

- `user`: writes units to `~/.config/systemd/user/` and calls
  `systemctl --user <action>`. The daemon must run as a non-root regular
  user; **all game servers run under that same user** — user-mode
  systemd cannot switch identities. The user must have lingering enabled
  (`sudo loginctl enable-linger <user>`) so units survive logout, and
  `XDG_RUNTIME_DIR` (typically `/run/user/<uid>`) must be accessible. The
  daemon resolves it automatically from the process environment or
  `/run/user/<uid>` and passes it to every `systemctl --user` call. If a
  panel-side server has a `user` field that does not match the daemon's
  OS user, start/restart/uninstall fail with `ErrUserMismatch`. The
  generated unit omits `User=` / `Group=` (systemd rejects them in user
  units) and uses `[Install] WantedBy=default.target`.

The metrics suppression note above (units predating the `*Accounting=yes`
directives report zeros until restart) applies to both scopes equally.

Configuration example:

```yaml
process_manager:
  name: systemd
  config:
    scope: user      # default: system
```

PID-based stats and metric collection paths are identical in both scopes.

## Shawl (Windows)

`shawl` is the default process manager on Windows. Each game server becomes a Windows
service named `gameapServer<serverID>`, whose binary is the `shawl` supervisor wrapping the
server's start command. The daemon talks to the service control manager through its API
rather than through `sc.exe`, so service state is read as a number and failures carry a Win32
error code instead of a sentence in the system language.

### Service account

With `use_network_service_user: true` every service runs as
`NT AUTHORITY\NetworkService`; otherwise it runs as the panel-supplied `server.user`, whose
password is taken from the `users:` map.

That account name is spelled the way the service control manager requires, and only that
spelling works. Windows localizes the display names of the well-known accounts — on a
Norwegian system the Network Service account shows up as `NT AUTHORITY\NETTVERKSTJENESTE` —
and while such a name still resolves to the right SID, `CreateService` will not accept it.
Neither will the English display form `NT AUTHORITY\NETWORK SERVICE`. Account names are
therefore normalized through `oscore.NormalizeWindowsServiceAccount` before they reach the
service control manager, and converted to the well-known SID (`*S-1-5-20`) by
`oscore.Grant` before they reach `icacls`.

### Permissions

The service account is granted Modify on the server directory when the service is
registered, and on `C:\gameap\services\logs` on every start. The grant is recursive, so it
also covers a `work_dir` subdirectory the service starts in (passed to shawl as `--cwd`). shawl writes its log as the
service account, so without the second grant the supervisor cannot open its log file and the
service dies during startup — which the service control manager reports only as an opaque
start failure.

### Registering and re-registering

`C:\gameap\services\gameapServer<ID>.yaml` records how a service was set up. It is
informational: the daemon compares against the registered service itself, never against this
file, because the file says nothing about a service that was removed or reconfigured behind
the daemon's back. (The `.yaml` extension is historical; the content is not YAML.)

A service is registered again when it is missing, when its `ServiceStartName` is not the
account the config asks for, or when its `BinaryPathName` is not the command line the config
produces. Anything else leaves a running server alone. This is what repairs an installation
whose services were registered by an older daemon under a name the service control manager
will not start.

A start that fails with `ERROR_SERVICE_LOGON_FAILED` triggers one re-registration and retry,
which recovers a password that was rotated in the daemon config alone. No password is written
to the marker file.

### Diagnostics

`Start` waits for the service to reach `RUNNING` rather than reporting success as soon as the
service control manager accepts the request, and tails the shawl log when a service stops
immediately. Because shawl restarts the game process itself, a running service proves the
supervisor came up, not that the game stayed up.

Metrics are liveness-only; see the table above.

### Changing the restart policy

The `--restart` / `--no-restart` flag is part of the service command line, so
toggling `autostart` in the panel makes the registered service differ from the
one the config describes. The service is then registered again on the server's
next start, which is when the change takes effect.

## Configuration

Process manager is configured in the daemon configuration file:

```yaml
process_manager:
  name: docker  # or: tmux, systemd, simple, winsw, shawl, podman
  config:
    # Process manager specific configuration
    image: "debian:bookworm-slim"
```

---

## Docker Process Manager

The Docker process manager runs game servers inside Docker containers using the Docker SDK.

### Features

- Container lifecycle management (create, start, stop, remove)
- Automatic image pulling
- Port mapping for game server ports
- Resource limits (memory, CPU)
- Volume mounting
- Custom installation scripts
- Log streaming
- Input sending via container attach

### Configuration Priority

Configuration values are resolved in the following priority order:

1. **Server Variables** (`server.Vars()`) - Highest priority
2. **GameMod Metadata** (`server.GameMod().Metadata`)
3. **Game Metadata** (`server.Game().Metadata`)
4. **ProcessManager Config** (`process_manager.config`) - Lowest priority

### Metadata Keys

#### Runtime Configuration

| Key                     | Description                                        | Example               | Default                |
|-------------------------|----------------------------------------------------|-----------------------|------------------------|
| `docker_image`          | Docker image for running the server                | `gameap/csgo:latest`  | `debian:bookworm-slim` |
| `docker_container_name` | Custom container name                              | `my-cs-server`        | Server UUID            |
| `docker_memory_limit`   | Memory limit                                       | `2g`, `512m`, `1024k` | No limit               |
| `docker_cpu_limit`      | CPU limit (cores)                                  | `2.0`, `0.5`          | No limit               |
| `docker_network_mode`   | Network mode                                       | `bridge`, `host`      | `bridge`               |
| `docker_capabilities`   | Linux capabilities (comma-separated)               | `NET_RAW,SYS_NICE`    | None                   |
| `docker_privileged`     | Run in privileged mode                             | `true`, `false`       | `false`                |
| `docker_volumes`        | Additional volumes (JSON array or comma-separated) | `["/data:/data:ro"]`  | None                   |
| `docker_dns`            | Custom DNS servers (comma-separated)               | `8.8.8.8,8.8.4.4`     | System default         |
| `docker_workdir`        | Mount path of the server directory (see below)     | `/home/container`     | `/server`              |

The server directory is always mounted at `docker_workdir`, which is also the
container working directory by default. When the server has a process work
directory (`work_dir`, `work_dir_linux`, `work_dir_windows`, `work_dir_macos`; see
the root README), the container working directory becomes `docker_workdir` joined
with that relative path, e.g. `/server/GroundBranch/Binaries/Linux`. In the start
command `{dir}` expands to `docker_workdir` and `{work_dir}` to that container
working directory, not to the host paths.

#### Installation Configuration

| Key                              | Description                       | Example                 | Default       |
|----------------------------------|-----------------------------------|-------------------------|---------------|
| `docker_installation_image`      | Image for installation phase      | `node:18-bookworm-slim` | None          |
| `docker_installation_script`     | Script to run during installation | See example below       | None          |
| `docker_installation_entrypoint` | Shell interpreter for the script  | `ash`, `/bin/sh`        | Auto-detected |
| `docker_installation_user`       | User to run installation as       | `1000:1000`, `root`     | `root`        |

> **Note:** If `docker_installation_entrypoint` is not set, the shell is auto-detected from the script's shebang line (e.g., `#!/bin/ash` → `/bin/ash`). Falls back to `/bin/sh` if no shebang is found.

> **Note:** Installation runs as `root` by default because most scripts need root permissions to install packages (apt, yum, etc.). If your script doesn't need root, set `docker_installation_user` to match your server user. Remember to `chown` files to the server user at the end of your installation script if running as root.

### Examples

#### Basic Game Configuration (Game Metadata)

```json
{
  "docker_image": "gameap/srcds:latest",
  "docker_memory_limit": "4g",
  "docker_cpu_limit": "2.0"
}
```

#### Server-Specific Override (Server Variables)

```json
{
  "docker_image": "gameap/csgo:latest",
  "docker_memory_limit": "8g",
  "docker_container_name": "csgo-competitive-server"
}
```

#### Installation Script Example

```json
{
  "docker_installation_image": "ghcr.io/parkervcp/installers:alpine",
  "docker_installation_script": "#!/bin/ash\nset -e\napk add --no-cache curl\ncurl -sL https://example.com/install.sh | ash\n"
}
```

The shell is auto-detected from the shebang (`#!/bin/ash`). To override explicitly:

```json
{
  "docker_installation_image": "ghcr.io/parkervcp/installers:alpine",
  "docker_installation_script": "...",
  "docker_installation_entrypoint": "ash"
}
```

#### Additional Volumes

JSON array format:
```json
{
  "docker_volumes": "[\"/shared/maps:/server/maps:ro\", \"/shared/configs:/server/configs\"]"
}
```

Comma-separated format:
```json
{
  "docker_volumes": "/shared/maps:/server/maps:ro,/shared/configs:/server/configs"
}
```

#### Capabilities and Privileged Mode

```json
{
  "docker_capabilities": "NET_RAW,NET_ADMIN,SYS_NICE",
  "docker_privileged": "false"
}
```

### Port Mapping

Ports are automatically mapped based on server configuration:

| Server Port  | Container Mapping                                              |
|--------------|----------------------------------------------------------------|
| Connect Port | `{IP}:{ConnectPort}:{ConnectPort}/tcp` and `/udp`              |
| Query Port   | `{IP}:{QueryPort}:{QueryPort}/udp` (if different from Connect) |
| RCON Port    | `{IP}:{RCONPort}:{RCONPort}/tcp` (if different from Connect)   |

### Container Lifecycle

```
Install:
  └─> If docker_installation_image && docker_installation_script:
      └─> Pull installation image
      └─> Create temp container with script
      └─> Mount server.WorkDir -> /mnt/server
      └─> Run container, wait for completion
      └─> Remove temp container
  └─> Else: Pull docker_image (optional)

Start:
  └─> Remove existing container (if any)
  └─> Pull image (if missing)
  └─> Create container
  └─> Start container

Stop:
  └─> Stop container (30s timeout)
  └─> Remove container

Status:
  └─> Inspect container
  └─> Return Running/NotRunning

GetOutput:
  └─> Get container logs (last 500 lines)

SendInput:
  └─> Attach to container stdin
  └─> Write input
```

### Process Manager Config Options

```yaml
process_manager:
  name: docker
  config:
    image: "debian:bookworm-slim"      # Default base image
    memory_limit: "2g"                  # Default memory limit
    cpu_limit: "1.0"                    # Default CPU limit
    host: "tcp://remote-docker:2376"   # Docker daemon address (DOCKER_HOST)
    cert_path: "/path/to/certs"        # TLS certificates directory (DOCKER_CERT_PATH)
    api_version: "1.41"                # Docker API version (DOCKER_API_VERSION)
```

#### Docker Connection Options

| Config Key    | Env Var Equivalent   | Description                                                                      |
|---------------|----------------------|----------------------------------------------------------------------------------|
| `host`        | `DOCKER_HOST`        | Docker daemon address (e.g., `tcp://remote:2376`, `unix:///var/run/docker.sock`) |
| `cert_path`   | `DOCKER_CERT_PATH`   | Directory containing `ca.pem`, `cert.pem`, `key.pem` for TLS                     |
| `api_version` | `DOCKER_API_VERSION` | Docker API version to use                                                        |

If none of these keys are set, the client falls back to `client.FromEnv` (reads from environment variables).

---

## Podman Process Manager

The Podman process manager runs game servers inside Podman containers using the Podman REST API.

### Features

- Compatible with Docker metadata keys (uses same `docker_*` prefix)
- Container lifecycle management
- Automatic image pulling
- Port mapping
- Resource limits
- Volume mounting
- Rootless container support

### Prerequisites

Podman socket must be running:

```bash
# For rootless Podman
systemctl --user start podman.socket

# For root Podman
sudo systemctl start podman.socket
```

### Configuration Priority

Same as Docker - see [Configuration Priority](#configuration-priority) above.

### Metadata Keys

Podman uses the same metadata keys as Docker for compatibility:

| Key                              | Description                   | Example              | Default                    |
|----------------------------------|-------------------------------|----------------------|----------------------------|
| `docker_image`                   | Container image               | `gameap/csgo:latest` | `debian:bookworm-slim`     |
| `docker_container_name`          | Custom container name         | `my-server`          | Server UUID                |
| `docker_memory_limit`            | Memory limit                  | `2g`, `512m`         | No limit                   |
| `docker_cpu_limit`               | CPU limit (cores)             | `2.0`, `0.5`         | No limit                   |
| `docker_network_mode`            | Network mode                  | `bridge`, `host`     | `bridge`                   |
| `docker_capabilities`            | Linux capabilities            | `NET_RAW,SYS_NICE`   | None                       |
| `docker_privileged`              | Privileged mode               | `true`, `false`      | `false`                    |
| `docker_volumes`                 | Additional volumes            | `["/data:/data:ro"]` | None                       |
| `docker_dns`                     | DNS servers                   | `8.8.8.8,8.8.4.4`    | System default             |
| `docker_workdir`                 | Server directory mount path   | `/home/container`    | `/server`                  |
| `docker_installation_image`      | Installation image            | `node:18`            | None                       |
| `docker_installation_script`     | Installation script           | `#!/bin/bash\n...`   | None                       |
| `docker_installation_entrypoint` | Shell for installation script | `ash`, `/bin/sh`     | Auto-detected from shebang |
| `docker_installation_user`       | User to run installation as   | `1000:1000`, `root`  | `root`                     |

As with Docker, the server directory is mounted at `docker_workdir`, which is the
container working directory by default; a configured process work directory
(`work_dir*` keys, see the root README) is joined onto it. `{dir}` and `{work_dir}`
in the start command expand to those container paths.

### Socket Configuration

```yaml
process_manager:
  name: podman
  config:
    socket_path: "unix:///run/user/1000/podman/podman.sock"
```

Default socket paths:
- Rootless: `unix:///run/user/{UID}/podman/podman.sock`
- Root: `unix:///run/podman/podman.sock`

### Examples

#### Basic Configuration

```yaml
process_manager:
  name: podman
  config:
    image: "debian:bookworm-slim"
```

#### Game Metadata Example

```json
{
  "docker_image": "gameap/minecraft:latest",
  "docker_memory_limit": "4g",
  "docker_cpu_limit": "2.0",
  "docker_dns": "8.8.8.8,1.1.1.1"
}
```

---

## Comparison: Docker vs Podman

| Feature         | Docker                 | Podman                    |
|-----------------|------------------------|---------------------------|
| Windows Support | Yes                    | No                        |
| macOS Support   | Yes                    | Yes                       |
| Linux Support   | Yes                    | Yes                       |
| Rootless        | Requires setup         | Native                    |
| Daemon          | Required               | Daemonless                |
| SDK             | Docker Go SDK          | REST API                  |
| Socket          | `/var/run/docker.sock` | `/run/podman/podman.sock` |

---

## Error Handling

Both Docker and Podman process managers implement:

- **Retry logic**: Connection errors are retried with exponential backoff (100ms to 5s, max 3 retries)
- **Graceful container removal**: Containers are force-removed on stop/uninstall
- **Image auto-pull**: Missing images are automatically pulled on start
- **Stop timeout**: 30 seconds default timeout for graceful container stop

---

## Troubleshooting

### Docker

**Connection refused**
```bash
# Check Docker daemon is running
sudo systemctl status docker

# Check socket permissions
ls -la /var/run/docker.sock
```

**Permission denied**
```bash
# Add user to docker group
sudo usermod -aG docker $USER
# Re-login required
```

### Podman

**Socket not found**
```bash
# Start Podman socket (rootless)
systemctl --user enable --now podman.socket

# Verify socket exists
ls -la /run/user/$(id -u)/podman/podman.sock
```

**Connection refused**
```bash
# Check Podman socket status
systemctl --user status podman.socket

# Test socket
curl --unix-socket /run/user/$(id -u)/podman/podman.sock http://d/v4.0.0/libpod/info
```
