# GameAP Daemon

[![Coverage Status](https://coveralls.io/repos/github/gameap/daemon/badge.svg?branch=develop)](https://coveralls.io/github/gameap/daemon?branch=develop)

The server management daemon

## Configuration

Configuration file: daemon.cfg

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

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| ca_certificate_file       | yes                   | string    | CA Certificate
| certificate_chain_file    | yes                   | string    | Server Certificate
| private_key_file          | yes                   | string    | Server Private Key
| private_key_password      | no                    | string    | Server Private Key Password
| dh_file                   | yes                   | string    | Diffie-Hellman Certificate

### Base Authentification

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| password_authentication   | no                    | boolean   | Login+password authentification
| daemon_login              | no                    | string    | Login. On Linux if empty or not set will be used Linux PAM
| daemon_password           | no                    | string    | Password. On Linux if empty or not set will be used Linux PAM

### Stats

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| if_list                   | no                    | string    | Network interfaces list
| drives_list               | no                    | string    | Disk drivers list
| stats_update_period       | no                    | integer   | Stats update period
| stats_db_update_period    | no                    | integer   | Update database period

### Other

#### Only on Windows

| Parameter                 | Required              | Type      | Info
|---------------------------|-----------------------|-----------|------------
| 7zip_path                 | no                    | string    | Path to 7zip file archiver. Example: "C:\Program Files\7-Zip\7z.exe"
| starter_path              | no                    | string    | Path to GameAP Starter. Example: "C:\gameap\gameap-starter.exe"
