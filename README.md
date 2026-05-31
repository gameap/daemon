# GameAP Daemon

[![Coverage Status](https://coveralls.io/repos/github/gameap/daemon/badge.svg?branch=master)](https://coveralls.io/github/gameap/daemon?branch=master)

The server management daemon

## Configuration

Configuration file: gameap-daemon.yaml

### Base parameters

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| ds_id                     | yes                   | integer   | Dedicated Server ID
| listen_ip                 | no (default "0.0.0.0")| string    | Listen IP
| listen_port               | no (default 31717)    | integer   | Listen port
| api_host                  | yes                   | string    | API Host
| api_key                   | yes                   | string    | API Key
| log_level                 | no                    | string    | Logging level (verbose, debug, info, warning, error, fatal)


### SSL/TLS

Certificates can be specified either as file paths or as inline PEM values.
If both are set, inline values take precedence over file paths.

#### File paths

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| ca_certificate_file       | yes*                  | string    | Path to CA Certificate file
| certificate_chain_file    | yes*                  | string    | Path to Server Certificate file
| private_key_file          | yes*                  | string    | Path to Server Private Key file
| private_key_password      | no                    | string    | Server Private Key Password
| dh_file                   | no                    | string    | Path to Diffie-Hellman file

#### Inline PEM values

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| ca_certificate            | yes*                  | string    | CA Certificate PEM
| certificate_chain         | yes*                  | string    | Server Certificate PEM
| private_key               | yes*                  | string    | Server Private Key PEM

\* For each certificate, either the file path or the inline PEM value must be provided.

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

### Base Authentification

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| password_authentication   | no                    | boolean   | Login+password authentification
| daemon_login              | no                    | string     | Login. On Linux if empty or not set will be used Linux PAM
| daemon_password           | no                    | string    | Password. On Linux if empty or not set will be used Linux PAM

### Stats

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| if_list                   | no                    | list      | Network interfaces to report. Empty/unset = physical, non-loopback interfaces only
| drives_list               | no                    | list      | Disk mounts to report. Empty/unset = root `/` plus the work_path drive
| stats_update_period       | no                    | integer   | Stats update period
| stats_db_update_period    | no                    | integer   | Update database period

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
| 7zip_path                 | no                    | string    | Path to 7zip file archiver. Example: "C:\Program Files\7-Zip\7z.exe"
| starter_path              | no                    | string    | Path to GameAP Starter. Example: "C:\gameap\gameap-starter.exe"
