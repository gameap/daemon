# Process Managers

This package provides process manager implementations for managing game server lifecycles. Each process manager implements the `contracts.ProcessManager` interface.

## Available Process Managers

| Name | Platforms | Description |
|------|-----------|-------------|
| `tmux` | Linux, macOS | Terminal multiplexer-based process management |
| `systemd` | Linux | Systemd service-based process management |
| `simple` | All | Basic script-based process management |
| `winsw` | Windows | Windows Service Wrapper |
| `shawl` | Windows | Windows service wrapper for arbitrary programs |
| `docker` | All | Docker container-based process management |
| `podman` | Linux, macOS | Podman container-based process management |

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

| Key | Description | Example | Default |
|-----|-------------|---------|---------|
| `docker_image` | Docker image for running the server | `gameap/csgo:latest` | `debian:bookworm-slim` |
| `docker_container_name` | Custom container name | `my-cs-server` | Server UUID |
| `docker_memory_limit` | Memory limit | `2g`, `512m`, `1024k` | No limit |
| `docker_cpu_limit` | CPU limit (cores) | `2.0`, `0.5` | No limit |
| `docker_network_mode` | Network mode | `bridge`, `host` | `bridge` |
| `docker_capabilities` | Linux capabilities (comma-separated) | `NET_RAW,SYS_NICE` | None |
| `docker_privileged` | Run in privileged mode | `true`, `false` | `false` |
| `docker_volumes` | Additional volumes (JSON array or comma-separated) | `["/data:/data:ro"]` | None |
| `docker_dns` | Custom DNS servers (comma-separated) | `8.8.8.8,8.8.4.4` | System default |
| `docker_workdir` | Container working directory | `/home/container` | `/server` |

#### Installation Configuration

| Key | Description | Example | Default |
|-----|-------------|---------|---------|
| `docker_installation_image` | Image for installation phase | `node:18-bookworm-slim` | None |
| `docker_installation_script` | Script to run during installation | See example below | None |
| `docker_installation_entrypoint` | Shell interpreter for the script | `ash`, `/bin/sh` | Auto-detected |
| `docker_installation_user` | User to run installation as | `1000:1000`, `root` | `root` |

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

| Server Port | Container Mapping |
|-------------|-------------------|
| Connect Port | `{IP}:{ConnectPort}:{ConnectPort}/tcp` and `/udp` |
| Query Port | `{IP}:{QueryPort}:{QueryPort}/udp` (if different from Connect) |
| RCON Port | `{IP}:{RCONPort}:{RCONPort}/tcp` (if different from Connect) |

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
```

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

| Key | Description | Example | Default |
|-----|-------------|---------|---------|
| `docker_image` | Container image | `gameap/csgo:latest` | `debian:bookworm-slim` |
| `docker_container_name` | Custom container name | `my-server` | Server UUID |
| `docker_memory_limit` | Memory limit | `2g`, `512m` | No limit |
| `docker_cpu_limit` | CPU limit (cores) | `2.0`, `0.5` | No limit |
| `docker_network_mode` | Network mode | `bridge`, `host` | `bridge` |
| `docker_capabilities` | Linux capabilities | `NET_RAW,SYS_NICE` | None |
| `docker_privileged` | Privileged mode | `true`, `false` | `false` |
| `docker_volumes` | Additional volumes | `["/data:/data:ro"]` | None |
| `docker_dns` | DNS servers | `8.8.8.8,8.8.4.4` | System default |
| `docker_workdir` | Container working directory | `/home/container` | `/server` |
| `docker_installation_image` | Installation image | `node:18` | None |
| `docker_installation_script` | Installation script | `#!/bin/bash\n...` | None |
| `docker_installation_entrypoint` | Shell for installation script | `ash`, `/bin/sh` | Auto-detected from shebang |
| `docker_installation_user` | User to run installation as | `1000:1000`, `root` | `root` |

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

| Feature | Docker | Podman |
|---------|--------|--------|
| Windows Support | Yes | No |
| macOS Support | Yes | Yes |
| Linux Support | Yes | Yes |
| Rootless | Requires setup | Native |
| Daemon | Required | Daemonless |
| SDK | Docker Go SDK | REST API |
| Socket | `/var/run/docker.sock` | `/run/podman/podman.sock` |

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
