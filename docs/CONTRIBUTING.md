# Contributing

Do-It should feel like a useful local tool, not a landing page. The first screen is the product.

## Development Setup

```bash
go test ./...
go run .
```

Open:

```text
http://localhost:8080
```

After frontend changes, restart the Go server because static assets are embedded.

## Code Style

Backend:

- Prefer Go standard library until a dependency clearly pays rent.
- Keep handlers small and explicit.
- Keep storage rules in `store.go`.
- Keep HTTP/network rules in `server.go`.
- Add tests for behavior that touches persistence, uploads, sync, or concurrency.

Frontend:

- Keep the current dependency-free frontend until complexity demands a framework.
- Prefer clear text labels over clever symbols.
- Keep controls stable in size so the graph does not jump around.
- Avoid decorative UI that does not help task work.

## UI Principles

The visual direction is:

- Monochrome.
- Background-colored surfaces.
- Thin borders.
- Translucency where it supports depth.
- No colorful gradients.
- No decorative cards inside cards.
- No large marketing hero.
- No unclear icon-only controls.

Good controls:

- `add` for creating a task.
- `done` and `open` for status changes.
- `delete` for deletion.
- `Root` for tasks without a parent.
- `Parent task` for choosing where a new task connects in the graph.

## Product Vocabulary

Task:

The core item. It has a title, notes, status, optional parent, and optional attachments.

Root:

A task without a parent.

Connected task:

A task with `ParentID` pointing to another task.

Device:

A currently connected browser session grouped by IP address and user agent.

LAN:

Local area network. In this project, it usually means the Wi-Fi network inside the home.

## Testing Checklist

Run:

```bash
go test ./...
go build ./...
```

Manual checks:

- Add a root task.
- Add a child task using `Parent task`.
- Mark a task done and open again.
- Delete a task.
- Attach an image and confirm it previews.
- Open the app in two browser tabs and confirm both update.
- Confirm the Devices section shows live connections.

## Future Work Ideas

Good next issues:

- SQLite storage.
- Auth or device pairing.
- Drag-to-reparent nodes.
- Rename module from `awesomeProject` to a real import path.
- Export/import backup.
- mDNS discovery.
- Optional Connect-Go/gRPC layer.
