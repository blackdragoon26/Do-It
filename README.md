# Do-It

Do-It is a local-first task graph for a personal homelab. It runs one Go server on a device inside your Wi-Fi network, then every laptop or phone on that network opens the same browser UI.

The product goal is simple: tasks, notes, files, and images stay synced across local devices without needing a cloud account. The UI is intentionally quiet: monochrome, translucent, no stupid decorative noise, and connected mind map instead of boring flat list.

<img width="1449" height="875" alt="Screenshot of my homelab on Vivo Phone" src="https://github.com/user-attachments/assets/be59e0f7-30ea-49b8-892f-37f157ca7ee3" />
<b>Screenshot of my homelab on Vivo Y71(3GB RAM 16GB ROM)Phone</b>


## What it does
- Go HTTP server with embedded static frontend.
- Task graph with parent-child connections.
- Live sync across open browsers using Server-Sent Events.
- File and image uploads stored on the host device.
- Connected device status for active browser sessions.
- JSON persistence for tasks and attachment metadata.
- Unit tests for task storage, uploads, and device presence.

## Quick Start

```bash
go test ./...
go run .
```

Open:

```text
http://localhost:8080
```

To make the server reachable from other devices on the same Wi-Fi:

```bash
DOIT_ADDR=0.0.0.0:8080 go run .
```

The app prints LAN URLs like:

```text
http://192.168.1.22:8080
```

Open that address from another device connected to the same Wi-Fi.

## Data Storage

By default, Do-It writes local app data under:

```text
data/state.json
data/uploads/
```

Use `DOIT_DATA_DIR` to choose a safer persistent location:

```bash
DOIT_DATA_DIR=/path/to/doit-data go run .
```

For an always-on phone or mini server, put `DOIT_DATA_DIR` somewhere backed up and large enough for uploads.

## Homelab Shape

Recommended always-on hosts:

- Old Android phone running Termux(which I did on Vivo).
- Raspberry Pi.
- Mini PC.
- NAS or small Linux box.

Most stock routers cannot run this directly. If a router runs OpenWrt and supports custom packages or containers, it might be possible, but router flash storage is usually a bad place for uploads.

More detail: [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)

## Project Layout

```text
main.go              server boot, config, static file embedding
server.go            HTTP routes, SSE live sync, uploads, device presence
store.go             task data model, mutex-protected store, JSON persistence
static/index.html    app shell
static/app.css       visual system
static/app.js        browser state, graph layout, API calls
*_test.go            backend tests
docs/                architecture, deployment, and contributor notes
```

## API Summary

```text
GET    /api/tasks          current task snapshot
POST   /api/tasks          create task with optional uploads
PATCH  /api/tasks/{id}     update task fields
DELETE /api/tasks/{id}     delete task
GET    /api/events         live SSE stream
GET    /api/devices        active connected devices
POST   /api/client-status  browser-reported battery/network health
GET    /api/network        LAN URLs detected by the server
GET    /uploads/{file}     uploaded file serving
```

Detailed design: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## Development

```bash
go test ./...
go build ./...
go run .
```

When static files change, restart `go run .` because the frontend is embedded into the Go binary.

UI/contributor rules: [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md)
