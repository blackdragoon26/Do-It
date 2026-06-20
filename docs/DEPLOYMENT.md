# Deployment

Do-It is designed to run on one always-on device inside your local network. Other devices use the browser UI.

## Recommended Hosts

Good:

- Old Android phone with Termux.
- Raspberry Pi.
- Mini PC.
- NAS.
- Old laptop.

Usually bad:

- Stock router firmware.
- Router internal flash storage.

Routers are built to route packets, not to store user uploads. Some OpenWrt routers can run Go binaries, but storage, RAM, and flash wear become real problems quickly.

## Local Development

```bash
go run .
```

Open:

```text
http://localhost:8080
```

For LAN testing:

```bash
DOIT_ADDR=0.0.0.0:8080 go run .
```

Then open the LAN URL printed by the server from another device.

## Android Phone With Termux

This is a strong homelab path if the phone battery is healthy and Android does not aggressively kill the terminal app.

Install packages:

```bash
pkg update
pkg install git golang
```

Clone and run:

```bash
git clone https://github.com/blackdragoon26/Do-It.git
cd Do-It
DOIT_ADDR=0.0.0.0:8080 DOIT_DATA_DIR=$HOME/doit-data go run .
```

From another device on the same Wi-Fi:

```text
http://PHONE_IP:8080
```

### Termux Storage Choices

Private Termux storage:

```bash
DOIT_DATA_DIR=$HOME/doit-data go run .
```

Pros:

- Simple permissions.
- Less Android storage friction.

Cons:

- Data can disappear if Termux is uninstalled.
- Harder to access with normal file managers.

Shared phone storage:

```bash
termux-setup-storage
DOIT_DATA_DIR=$HOME/storage/shared/DoItData go run .
```

Pros:

- Easier backup.
- Easier file access.

Cons:

- Android storage permissions can be annoying.
- Other apps may see the files.

### Keep It Running

Recommended:

- Disable battery optimization for Termux.
- Keep Wi-Fi enabled during sleep.
- Use `termux-wake-lock` if available.
- Keep the phone cool.
- Do not use a swollen or unsafe battery.
- Back up `state.json` and uploads periodically.

Optional startup tool:

- Termux:Boot can start scripts after the phone boots.

Example script idea:

```bash
cd $HOME/Do-It
DOIT_ADDR=0.0.0.0:8080 DOIT_DATA_DIR=$HOME/doit-data go run .
```

Later, build a binary instead of using `go run`:

```bash
go build -o doit .
DOIT_ADDR=0.0.0.0:8080 DOIT_DATA_DIR=$HOME/doit-data ./doit
```

## Raspberry Pi Or Linux Box

```bash
git clone https://github.com/blackdragoon26/Do-It.git
cd Do-It
go build -o doit .
DOIT_ADDR=0.0.0.0:8080 DOIT_DATA_DIR=/var/lib/doit ./doit
```

For a real always-on setup, run it under systemd.

Example unit:

```ini
[Unit]
Description=Do-It local task graph
After=network-online.target

[Service]
WorkingDirectory=/opt/doit
ExecStart=/opt/doit/doit
Environment=DOIT_ADDR=0.0.0.0:8080
Environment=DOIT_DATA_DIR=/var/lib/doit
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

## Environment Variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `DOIT_ADDR` | `:8080` | HTTP listen address |
| `DOIT_DATA_DIR` | `data` | Directory for state and uploads |

Examples:

```bash
DOIT_ADDR=127.0.0.1:9000 go run .
DOIT_ADDR=0.0.0.0:8080 DOIT_DATA_DIR=/mnt/storage/doit ./doit
```

## Firewall And Network Notes

Devices must be on the same network and allowed to talk to the server port.

If another device cannot open the app:

1. Confirm the server is running.
2. Confirm both devices are on the same Wi-Fi.
3. Use the LAN URL printed by the server.
4. Check that the host firewall allows port `8080`.
5. Try `DOIT_ADDR=0.0.0.0:8080`.
6. Make sure the Wi-Fi does not use client isolation.

Client isolation is common on guest networks. It prevents devices from talking to each other.

## Backups

Back up:

```text
state.json
uploads/
```

Simple backup command:

```bash
tar -czf doit-backup.tgz /path/to/doit-data
```

Before relying on this daily, the next recommended upgrade is SQLite plus a backup/export command.
