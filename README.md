# GameAP Daemon

[![Coverage Status](https://coveralls.io/repos/github/gameap/daemon/badge.svg?branch=master)](https://coveralls.io/github/gameap/daemon?branch=master)

The server management daemon

The daemon communicates with the GameAP panel over gRPC only: it opens an
outbound connection to the panel and keeps a bidirectional stream for tasks,
server statuses, file transfers, console access and metrics. The daemon does
not listen for incoming connections from the panel.

## Enrollment

The easiest way to connect a node is the enroll command. It contacts the
panel with a setup key, downloads the TLS certificates and writes a ready
config file:

```bash
gameap-daemon enroll --connect grpc://panel.example.com:31718/<setup-key>
```

| Flag            | Default                                  | Info
|-----------------|------------------------------------------|------------
| --connect       | (required)                               | Connect URL (grpc://host:port/setupKey)
| --config-path   | /etc/gameap-daemon/gameap-daemon.yaml    | Path to write the config file
| --certs-dir     | /etc/gameap-daemon/certs                 | Directory to save TLS certificates
| --listen-ip     | 0.0.0.0 (auto-detected outbound IP)      | Node IP reported to the panel
| --listen-port   | 31717                                    | Node port reported to the panel
| --work-path     | /srv/gameap                              | Working directory for game servers

## Configuration

Configuration file: gameap-daemon.yaml

### Base parameters

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| ds_id                     | yes                   | integer   | Dedicated Server ID
| api_key                   | yes                   | string    | API Key (sent in the gRPC registration)
| api_host                  | deprecated            | string    | Fallback source for the gRPC address (host:31718) and insecure transport detection (`http://` prefix). Prefer `grpc.address` / `grpc.insecure`
| log_level                 | no                    | string    | Logging level (trace, debug, info, warning, error, fatal)

### gRPC connection

Either `grpc.address` or the deprecated `api_host` must be set.

| Parameter                     | Required              | Type      | Info
|-------------------------------|-----------------------|-----------|------------
| grpc.address                  | yes*                  | string    | Panel gRPC endpoint (host:port)
| grpc.insecure                 | no (default false)    | boolean   | Disable TLS (plaintext connection)
| grpc.heartbeat_interval       | no (default 30s)      | duration  | Heartbeat period
| grpc.connect_timeout          | no (default 30s)      | duration  | Dial timeout
| grpc.initial_reconnect_delay  | no (default 1s)       | duration  | First reconnect delay
| grpc.max_reconnect_delay      | no (default 60s)      | duration  | Reconnect delay cap

\* If `grpc.address` is empty, the address is derived from `api_host` as host:31718.

### SSL/TLS (mTLS for the gRPC connection)

Certificates can be specified either as file paths or as inline PEM values.
If both are set, inline values take precedence over file paths.

#### File paths

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| ca_certificate_file       | yes*                  | string    | Path to CA Certificate file
| certificate_chain_file    | yes*                  | string    | Path to Server Certificate file
| private_key_file          | yes*                  | string    | Path to Server Private Key file
| private_key_password      | no                    | string    | Server Private Key Password

#### Inline PEM values

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| ca_certificate            | yes*                  | string    | CA Certificate PEM
| certificate_chain         | yes*                  | string    | Server Certificate PEM
| private_key               | yes*                  | string    | Server Private Key PEM

\* For each certificate, either the file path or the inline PEM value must be
provided. Not required when the connection is insecure (`grpc.insecure: true`
or an `http://` `api_host`).

Inline PEM example:
```yaml
ca_certificate: |
  -----BEGIN CERTIFICATE-----
  MIIDPTCCAiWgAwIBAgIRAIy/eAu45373SY5SxmS8HsowDQYJKoZIhvcNAQELBQAw
  ...
  -----END CERTIFICATE-----
certificate_chain: |
  -----BEGIN CERTIFICATE-----
  MIIDPTCCAiWgAwIBAgIRAIy/eAu45373SY5SxmS8HsowDQYJKoZIhvcNAQELBQAw
  ...
  -----END CERTIFICATE-----
private_key: |
  -----BEGIN PRIVATE KEY-----
  MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCaJGeZltblsjgD
  ...
  -----END PRIVATE KEY-----
```

### Metrics filters

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| if_list                   | no                    | list      | Network interfaces to report. Empty/unset = physical, non-loopback interfaces only
| drives_list               | no                    | list      | Disk mounts to report. Empty/unset = root `/` plus the work_path drive

### Steam

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| steamcmd_path             | no                    | string    | Path to the directory that contains steamcmd
| steam_config.login        | no                    | string    | Steam account login (anonymous when empty)
| steam_config.password     | no                    | string    | Steam account password
| steam_config.group        | no                    | string    | Shared OS group for the steamcmd directory (see below)

When the daemon runs as `root` and a game server has its own `su_user`, steamcmd is
executed under that unprivileged user (least privilege; files end up owned correctly
for both install and updates). Because `steamcmd.sh` self-updates and writes into its
own directory, that directory must be writable by every `su_user`.

Before running steamcmd the daemon applies, recursively, the setgid bit plus group
`rwx`/`rw` to `steamcmd_path`, changing only the group (the owner is preserved). The
group is taken from `steam_config.group`, falling back to the `su_user` primary group
when empty.

On a node with several different `su_user`s, set `steam_config.group` to a shared
group and add every `su_user` to it (e.g. `usermod -aG <group> <su_user>`). The
daemon then keeps the steamcmd directory consistently group-shared so self-updates
succeed regardless of which server triggers them. `steam_config` is read from the
yaml config only (it is not pushed from the API). This whole step is a no-op when
the daemon does not run as `root`.

### Other

#### Only on Windows

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| path_7zip                 | no                    | string    | Path to 7zip file archiver. Example: "C:\Program Files\7-Zip\7z.exe"
| path_starter              | no                    | string    | Path to GameAP Starter. Example: "C:\gameap\gameap-starter.exe"

### Removed configuration keys

The legacy protocols (the inbound binn/TLS listener and the HTTP REST API
client) have been removed, the daemon is gRPC-only now. The following keys
are ignored if present in a config file (unknown keys do not cause errors):

`listen_ip`, `listen_port`, `daemon_login`, `daemon_password`,
`password_authentication`, `dh_file`, `stats_update_period`,
`stats_db_update_period`, `grpc.enabled`, `task_manager.update_period`

## Game server working directory

By default a game server process starts in the server directory
(`work_path` + the server `dir` from the panel). Some games keep their server
binary in a subdirectory and must be started from there, for example Ground
Branch ships `GroundBranch/Binaries/Win64/GroundBranchServer-Win64-Shipping.exe`.
The working directory of the process can be moved into such a subdirectory
without changing the server directory itself.

The directory is read from these keys, in this order, and the first non-empty
value wins:

1. server variables (per-server override, edited on the server page in the panel);
2. game mod metadata;
3. game metadata.

| Key                | Applies to                                            |
|--------------------|-------------------------------------------------------|
| `work_dir_linux`   | Linux nodes (and any OS other than Windows and macOS) |
| `work_dir_windows` | Windows nodes                                         |
| `work_dir_macos`   | macOS nodes                                           |
| `work_dir`         | Any OS, used when the OS-specific key is not set      |

On each level the key for the node OS is checked before the generic `work_dir`.
macOS does not fall back to `work_dir_linux`.

Rules for the value:

- it is a path relative to the server directory, forward slashes work on every OS
  (`GroundBranch/Binaries/Win64`);
- an absolute path (`/srv/x`, `C:\x`, `\\host\share`) or a path that leaves the
  server directory (`../x`) is rejected and the server does not start;
- control characters and `%` are rejected as well, because they cannot be
  written safely into a systemd unit;
- an empty value, `.` or `./` means the server directory, which is the historical
  behaviour;
- when the configured directory does not exist at start time, the start fails with
  `server process work directory does not exist` instead of quietly running the
  command somewhere else. Installation and updates run before that check, so a
  directory created by the installer is fine.

Only the process working directory moves. Installation, updates, deletion, the
after-install script, the panel file manager, the `{dir}` placeholder and the
Docker/Podman bind mount keep working with the server directory. The new
`{work_dir}` placeholder expands to the absolute process working directory
(inside a Docker/Podman container: the container path, like `{dir}`).

Example for Ground Branch (game mod metadata):

```
work_dir_windows: GroundBranch/Binaries/Win64
work_dir_linux:   GroundBranch/Binaries/Linux
work_dir_macos:   GroundBranch/Binaries/Mac
```

with the start command `GroundBranchServer-Win64-Shipping.exe ?MaxPlayers={max_players} ...`.

A `work_dir` set as a server variable is also exported to the process
environment as `WORK_DIR`, like every other server variable. Values from
metadata are not exported.

## Game server home directory

Every game server on a node normally runs as the same system user, so every
game that stores its configuration under `$HOME` shares one directory. SCP:
Secret Laboratory is the clearest case: it keeps per-server configs in
`$HOME/.config/SCP Secret Laboratory/config/<port>/`, and everything outside
that per-port subdirectory — accepted EULA, global plugins, permissions, key
caches — is shared between all servers on the node.

`home_dir` gives one server its own `HOME` inside its server directory. It is
read from the same three sources as `work_dir`, in the same order, and the first
non-empty value wins:

| Key                | Applies to                                            |
|--------------------|-------------------------------------------------------|
| `home_dir_linux`   | Linux nodes (and any OS other than Windows and macOS) |
| `home_dir_windows` | Windows nodes                                         |
| `home_dir_macos`   | macOS nodes                                           |
| `home_dir`         | Any OS, used when the OS-specific key is not set      |

The value follows the same rules as `work_dir`: a path relative to the server
directory, with absolute paths, paths leaving the server directory, control
characters and `%` rejected. `.` means the server directory itself.

When nothing is configured, `HOME` is left exactly as it is today: systemd
derives it from `User=`, and the other process managers inherit it from the
daemon or from the server user. This is deliberate — `hlds_run` and `srcds_run`
look for `$HOME/.steam/sdk32/steamclient.so`, so a private `HOME` must be opted
into per game.

`home_dir` is applied after the server variables, so it wins over a variable
that happens to be named `home`. Inside a Docker or Podman container `HOME`
points at the container path, like `{dir}`.

Example for SCP: Secret Laboratory (game mod metadata):

```
home_dir: .
```

with the start command `./LocalAdmin {port} --useDefault`. The configuration
then lands in `<server directory>/.config/SCP Secret Laboratory/config/<port>/`.
