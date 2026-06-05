<img width="256" height="256" alt="rimedeck-icon" src="https://github.com/user-attachments/assets/b6d42b07-33d3-4045-ac63-05b708c03025" />

# RimeDeck

RimeDeck is a local-first AI agent workbench — organize a productive group of agents on your desktop, with zero Docker and zero cloud dependency. Forked from [Multica](https://github.com/multica-ai/multica).

## Why RimeDeck

Multica's desktop app connects to a cloud backend. RimeDeck removes that dependency: it embeds PostgreSQL and the Go server as child processes inside the Electron app. Double-click to launch — the app starts the database, runs migrations, spawns the server, and opens the UI. No Docker, no remote API, no manual setup.

<img width="630" height="400" alt="image" src="https://github.com/user-attachments/assets/116bf358-e8bb-4b0a-a3dd-c553a5a86222" /> 

## Supported Runtimes

RimeDeck supports 12 AI coding tools as agent runtimes. The daemon auto-detects installed tools on your machine during setup.

| Runtime | CLI | Provider |
|---------|-----|----------|
| Antigravity | `antigravity` | Google |
| Claude Code | `claude` | Anthropic |
| Codex | `codex` | OpenAI |
| Copilot | `github-copilot` | GitHub / Microsoft |
| Cursor | `cursor` | Cursor |
| Gemini CLI | `gemini` | Google |
| Hermes | `hermes` | — |
| Kimi | `kimi` | Moonshot AI |
| Kiro CLI | `kiro` | Amazon |
| OpenCode | `opencode` | — |
| OpenClaw | `openclaw` | — |
| Pi | `pi` | — |

## Architecture

### Launch Sequence

```
RimeDeck App Launch
  │
  ▼
[Splash Screen] — "Starting RimeDeck..."
  │
  ▼
[PostgresManager]
  │  1. Resolve PG binary (bundled > managed > PATH)
  │  2. initdb (first run only)
  │  3. pg_ctl start
  │  4. createdb + pgcrypto extension
  │  5. Health check: pg_isready
  │
  ▼
[MigrationRunner]
  │  Shell out: `multica-migrate up` with DATABASE_URL
  │
  ▼
[BackendManager]
  │  1. Spawn Go server as child process
  │  2. Pass DATABASE_URL, PORT, JWT_SECRET via env
  │  3. Health check: GET /health
  │
  ▼
[DaemonManager] — existing upstream code, unchanged
  │  Connects to localhost:{backendPort}
  │
  ▼
[Renderer loads] — API URL injected via runtime config IPC
```

### Data Directories

All user data lives under `~/.rimedeck/`:

| Directory | Content |
| --- | --- |
| `~/.rimedeck/config.json` | CLI configuration |
| `~/.rimedeck/pg/data/` | PostgreSQL data |
| `~/.rimedeck/workspaces/` | Agent execution environments |

### Key Components

**PostgresManager** (`apps/desktop/src/main/local-backend/postgres-manager.ts`)

- Binary resolution: bundled with app → managed (auto-downloaded on first run) → system PATH
- Data directory: `~/.rimedeck/pg/data/`
- Localhost-only, auto port allocation, graceful shutdown via `pg_ctl stop`

**MigrationRunner** (`apps/desktop/src/main/local-backend/migration-runner.ts`)

- Reuses the bundled `multica-migrate` binary
- Runs `multica-migrate up` with the local `DATABASE_URL`

**BackendManager** (`apps/desktop/src/main/local-backend/backend-manager.ts`)

- Spawns the Go server as a child process
- SIGTERM → 5s grace → SIGKILL on quit

**Shutdown chain** (on `before-quit`): stop daemon → stop Go backend → stop PostgreSQL.

## Prerequisites

- **Node.js** 22+
- **pnpm** 10+ (`corepack enable && corepack prepare pnpm@latest --activate`)
- **Go** 1.24+ (for the backend server and CLI)
- **PostgreSQL** 17 (the packaged app bundles its own)

## Quick Start

```bash
# Install dependencies
pnpm install

# One-command dev (auto-creates env, starts DB, migrates, launches everything)
make dev
```

## Desktop App

```bash
# Dev mode (with HMR)
pnpm dev:desktop

# Build
pnpm --filter @multica/desktop build

# Package for current platform
pnpm --filter @multica/desktop package

# Package for all platforms
pnpm --filter @multica/desktop package:all
```

The desktop build bundles the Go CLI (`multica`) and an embedded PostgreSQL, so the app runs fully offline with no external dependencies.

## Project Structure

```
apps/
  desktop/    — Electron desktop app (electron-vite)
packages/
  core/       — Headless business logic (zero react-dom)
  ui/         — Atomic UI components (shadcn/Base UI)
  views/      — Shared business pages/components
  tsconfig/   — Shared TypeScript configuration
  eslint-config/ — Shared ESLint configuration
server/       — Go backend (Chi router, sqlc, gorilla/websocket)
scripts/      — Monorepo tooling (version bump, etc.)
```

## Useful Commands

```bash
# Backend
make server           # Run Go server (port 8080)
make build            # Build server + CLI binaries
make test             # Go tests
make migrate-up       # Run database migrations

# Frontend
pnpm dev:desktop      # Electron dev server (with HMR)
pnpm build            # Build all frontend apps
pnpm typecheck        # TypeScript check across all packages
pnpm test             # Unit tests (Vitest)
pnpm lint             # ESLint
```

The desktop app checks for updates automatically via GitHub Releases. Users can also manually check in Settings → Updates.

## License

See [LICENSE](LICENSE).
