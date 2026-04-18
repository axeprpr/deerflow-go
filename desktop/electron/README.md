# Deerflow-Go Electron (Local Runtime)

This desktop shell runs **Electron + local deerflow-go runtime + upstream UI frontend**.

It does not modify upstream UI code under `third_party/deerflow-ui/frontend`.

## What It Starts

1. Backend:
   - `single` mode: local `langgraph` server (`http://127.0.0.1:2026` by default)
   - `bundle` mode: `runtime-stack` host-plan launcher (`gateway/worker/state/sandbox`)
2. Frontend:
   - `dev` / `start`: upstream Next.js UI
   - `external`: open an existing URL without spawning frontend process
3. Electron window loading the selected frontend URL

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

Build bundled runtime binaries (needed for packaging and `bundle` mode):

```bash
pnpm build:runtime
```

## Packaging

Build local installer/archive artifacts:

```bash
pnpm dist
```

Platform-specific packaging:

```bash
pnpm dist:linux
pnpm dist:mac
pnpm dist:win
```

Artifacts are written to `desktop/electron/release/`.

GitHub Actions workflow: `.github/workflows/electron-release.yml`

- Trigger: `workflow_dispatch` or tag push (`v*`)
- Matrix build: Linux + macOS + Windows
- Output: uploaded artifacts and tag release assets

## Runtime Resolution Rules

Backend startup command priority:

1. `DEERFLOW_ELECTRON_BACKEND_BIN` (explicit binary path)
2. Runtime binary dirs:
   - `DEERFLOW_ELECTRON_RUNTIME_BINARY_DIR`
   - packaged `resources/runtime/bin`
   - `desktop/electron/runtime/bin`
   - `bin/`
3. Fallback (dev only): `go run ./cmd/langgraph`

Bundle mode backend flow:

1. resolve `runtime-stack`
2. validate existing bundle (`-validate-bundle`)
3. auto rebuild bundle when needed (`-write-bundle`)
4. launch bundle with host-plan policy (`-spawn-bundle -spawn-bundle-use-host-plan`)

Frontend startup command:

- Default: `pnpm dev -- --hostname <host> --port <port>`
- If `DEERFLOW_ELECTRON_FRONTEND_MODE=start`: `pnpm start -- --hostname <host> --port <port>`
- If `DEERFLOW_ELECTRON_FRONTEND_MODE=external`: do not spawn frontend process, open `DEERFLOW_ELECTRON_FRONTEND_EXTERNAL_URL`

## Environment Variables

- `DEERFLOW_ELECTRON_BACKEND_HOST` (default `127.0.0.1`)
- `DEERFLOW_ELECTRON_BACKEND_PORT` (default `2026`)
- `DEERFLOW_ELECTRON_RUNTIME_MODE` (`single` or `bundle`, default `single`)
- `DEERFLOW_ELECTRON_RUNTIME_BINARY_DIR` (optional runtime binary directory override)
- `DEERFLOW_ELECTRON_RUNTIME_BUNDLE_DIR` (bundle mode: runtime bundle directory)
- `DEERFLOW_ELECTRON_RUNTIME_BUNDLE_PRESET` (bundle mode preset, default `shared-remote`)
- `DEERFLOW_ELECTRON_RUNTIME_BUNDLE_REFRESH` (bundle mode: force rewrite bundle, default `false`)
- `DEERFLOW_ELECTRON_RUNTIME_DATA_ROOT` (bundle mode data root)
- `DEERFLOW_ELECTRON_WORKER_PORT` (bundle mode worker port; state/sandbox derive from worker)
- `DEERFLOW_ELECTRON_FRONTEND_HOST` (default `127.0.0.1`)
- `DEERFLOW_ELECTRON_FRONTEND_PORT` (default `3000`)
- `DEERFLOW_ELECTRON_FRONTEND_MODE` (`dev`, `start`, or `external`; default `dev`, packaged default `external`)
- `DEERFLOW_ELECTRON_FRONTEND_EXTERNAL_URL` (used when frontend mode is `external`)
- `DEERFLOW_ELECTRON_BACKEND_BIN` (optional explicit backend binary)
- `DEERFLOW_ELECTRON_WAIT_TIMEOUT_MS` (default `180000`)
- `DEERFLOW_ELECTRON_WAIT_INTERVAL_MS` (default `500`)

The launcher also loads root `.env` (if exists) and forwards values to backend/frontend processes.
