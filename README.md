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
