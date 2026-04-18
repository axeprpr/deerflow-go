# Deerflow-Go Electron (Local Runtime)

This desktop shell runs **Electron + local deerflow-go runtime + upstream UI frontend**.

It does not modify upstream UI code under `third_party/deerflow-ui/frontend`.

## What It Starts

1. Backend: local `langgraph` server (`http://127.0.0.1:2026` by default)
2. Frontend: upstream Next.js UI (`http://127.0.0.1:3000` by default)
3. Electron window loading the frontend URL

## Prerequisites

- Go toolchain (if `bin/langgraph` is not already built)
- Node.js + pnpm
- Frontend dependencies installed in `third_party/deerflow-ui/frontend`

## Quick Start

Install Electron deps:

```bash
cd desktop/electron
pnpm install
```

Start desktop app:

```bash
pnpm dev
```

## Runtime Resolution Rules

Backend startup command priority:

1. `DEERFLOW_ELECTRON_BACKEND_BIN` (explicit binary path)
2. `bin/langgraph` (or `bin/langgraph.exe` on Windows)
3. Fallback: `go run ./cmd/langgraph`

Frontend startup command:

- Default: `pnpm dev -- --hostname <host> --port <port>`
- If `DEERFLOW_ELECTRON_FRONTEND_MODE=start`: `pnpm start -- --hostname <host> --port <port>`

## Environment Variables

- `DEERFLOW_ELECTRON_BACKEND_HOST` (default `127.0.0.1`)
- `DEERFLOW_ELECTRON_BACKEND_PORT` (default `2026`)
- `DEERFLOW_ELECTRON_FRONTEND_HOST` (default `127.0.0.1`)
- `DEERFLOW_ELECTRON_FRONTEND_PORT` (default `3000`)
- `DEERFLOW_ELECTRON_FRONTEND_MODE` (`dev` or `start`, default `dev`)
- `DEERFLOW_ELECTRON_BACKEND_BIN` (optional explicit backend binary)
- `DEERFLOW_ELECTRON_WAIT_TIMEOUT_MS` (default `180000`)
- `DEERFLOW_ELECTRON_WAIT_INTERVAL_MS` (default `500`)

The launcher also loads root `.env` (if exists) and forwards values to backend/frontend processes.
