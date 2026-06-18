<img width="256" height="256" alt="rimedeck-icon" src="https://github.com/user-attachments/assets/b6d42b07-33d3-4045-ac63-05b708c03025" />

# RimeDeck

RimeDeck is a local-first AI agent workbench with a built-in Kanban board — manage issues, orchestrate a team of AI coding agents, and track progress in one desktop app, with zero Docker and zero cloud dependency. It also supports compute sharing and remote collaboration across machines on a local network or VPN. Forked from [Multica](https://github.com/multica-ai/multica).

## Why RimeDeck

Multica's desktop app connects to a cloud backend. RimeDeck removes that dependency: it embeds PostgreSQL and the Go server as child processes inside the Electron app. Double-click to launch — the app starts the database, runs migrations, spawns the server, and opens the UI. No Docker, no remote API, no manual setup.

<details open>
<summary>📸 Screenshot 1</summary>
<img width="630" height="400" alt="image" src="https://github.com/user-attachments/assets/116bf358-e8bb-4b0a-a3dd-c553a5a86222" />
</details>

<details>
<summary>📸 Screenshot 2</summary>
<img width="630" height="427" alt="image" src="https://github.com/user-attachments/assets/e8f5227a-eaff-443e-a611-799f8722a0fa" />
</details>

<details>
<summary>📸 Screenshot 3</summary>
<img width="630" height="427" alt="image" src="https://github.com/user-attachments/assets/0811c125-5326-4d86-8ccc-b14c58cb6429" />
</details>


## Supported Runtimes

RimeDeck supports 15 AI coding tools as agent runtimes. The daemon auto-detects installed tools on your machine during setup.

| Runtime        | CLI            | Provider           |
| -------------- | -------------- | ------------------ |
| Antigravity    | `agy`          | Google             |
| Claude Code    | `claude`       | Anthropic          |
| Codex          | `codex`        | OpenAI             |
| Copilot        | `copilot`      | GitHub / Microsoft |
| Cursor         | `cursor-agent` | Cursor             |
| Gemini CLI     | `gemini`       | Google             |
| Hermes         | `hermes`       | NousResearch       |
| Kimi           | `kimi`         | Moonshot AI        |
| Kiro CLI       | `kiro-cli`     | Amazon             |
| OMP (oh-my-pi) | `omp`          | —                  |
| OpenCode       | `opencode`     | —                  |
| OpenClaw       | `openclaw`     | —                  |
| Pi             | `pi`           | —                  |
| Qoder          | `qoder`        | Qodo               |
| Qwen Code      | `qwen-code`    | Alibaba            |

> **OMP** ([oh-my-pi](https://github.com/can1357/oh-my-pi)) is a community fork of Pi with hash-anchored edits, LSP integration, subagents, and 40+ model providers. It shares the same JSON event-stream protocol as Pi, so it works out-of-the-box. Set `MULTICA_OMP_PATH` to point at a non-default binary, or `MULTICA_OMP_MODEL` to pin a default model.

## Architecture

### Core Concepts

```
┌─────────────────────────────────────────────────────────────────┐
│                        RimeDeck App                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────────┐   │
│  │ Electron │  │ Go Server│  │PostgreSQL│  │    Daemon     │   │
│  │   (UI)   │◄►│  (API)   │◄►│  (Data)  │  │ (Task Runner) │   │
│  └──────────┘  └──────────┘  └──────────┘  └───────┬───────┘   │
└─────────────────────────────────────────────────────┼───────────┘
                                                      │
                        ┌─────────────────────────────┼──────────────────┐
                        │                             │                  │
                        ▼                             ▼                  ▼
               ┌────────────────┐            ┌──────────────┐   ┌──────────────┐
               │     Squad      │            │    Agent      │   │    Agent      │
               │  (Team Unit)   │            │  "Reviewer"   │   │  "Coder"      │
               │                │            │               │   │               │
               │  Leader: ──────┼───────────►│  Runtime: ────┼┐  │  Runtime: ────┼┐
               │  Members: ─────┼───────────►│  claude CLI   ││  │  codex CLI    ││
               └────────────────┘            │               ││  │               ││
                                             │  Skills:      ││  │  Skills:      ││
                                             │  ┌──────────┐ ││  │  ┌──────────┐ ││
                                             │  │Go Review │ ││  │  │TS Expert │ ││
                                             │  │Security  │ ││  │  │Test-TDD  │ ││
                                             │  └──────────┘ ││  │  └──────────┘ ││
                                             └───────────────┘│  └───────────────┘│
                                              └───────────────┘   └───────────────┘
                                                      │                  │
                         ┌────────────────────────────┼──────────────────┘
                         │  Daemon spawns CLI process  │
                         ▼                             ▼
               ┌──────────────────┐          ┌──────────────────┐
               │  claude (CLI)    │          │  codex (CLI)     │
               │  omp / gemini /  │          │  copilot / kiro  │
               │  cursor-agent   │          │  hermes / ...    │
               └──────────────────┘          └──────────────────┘
```

**Squad** — A team unit with one leader agent and member agents/users. When an issue is assigned to a squad, the leader agent claims it, breaks the work down, and delegates sub-tasks to members via `@mention` links.

**Agent** — A named AI entity with custom instructions, environment, and MCP config. Each agent is bound to an **AgentRuntime** — one of the 15 supported CLI tools (claude, codex, qoder, etc.).

**Skill** — Reusable instruction files (e.g. code review checklists, language conventions) attached to an agent. At task time, the daemon writes them into the workspace so the CLI discovers them natively (`.claude/skills/`, `.opencode/skills/`, etc.).

**Daemon** — A background process that polls the task queue, prepares isolated workspaces, injects skills and runtime config, then spawns the agent CLI as a child process. It streams events back to the server via WebSocket.

### Remote Collaboration

RimeDeck supports two independent collaboration modes over a local network (or Tailscale / VPN). They can be combined freely.

#### Compute Collaboration (Runtime → Add a computer)

Add a remote machine as a headless compute node. The remote daemon executes agent tasks but has no access to the workspace UI — it only talks to `/api/daemon/*` endpoints via a scoped daemon token (`mdt_`).

```
    Machine A (Server)                    Machine C (Compute Node)
    ┌──────────────────────┐              ┌──────────────────────┐
    │  RimeDeck Desktop    │              │  RimeDeck Desktop    │
    │  ┌────────────────┐  │              │                      │
    │  │ UI (Electron)  │  │              │  (UI stays on local  │
    │  │ issues, agents │  │              │   workspace — unused │
    │  └────────────────┘  │              │   for this server)   │
    │  ┌────────────────┐  │   daemon     │  ┌────────────────┐  │
    │  │ Server + PG    │◄─┼── token ────┼──│ Daemon          │  │
    │  │ workspace data │  │  (mdt_)      │  │ claims & runs   │  │
    │  └────────────────┘  │              │  │ tasks only      │  │
    │  ┌────────────────┐  │              │  └────────────────┘  │
    │  │ Local Daemon   │  │              └──────────────────────┘
    │  └────────────────┘  │
    └──────────────────────┘

    ✅ Remote daemon runs agent tasks
    ✅ Server dispatches to both local + remote runtimes
    ❌ Remote user cannot see issues / agents / settings
```

**Setup flow**: Server shows IP + pairing code → remote machine enters them in "Connect to server" dialog → daemon token issued → daemon registers as a remote runtime.

#### Workspace Collaboration (Settings → Members → Invite member)

Invite a person as a workspace member. Their Desktop UI switches to the server's API and they get full workspace access — issues, agents, runtimes, settings — authenticated via JWT session, exactly like Multica Cloud.

```
    Machine A (Server)                    Machine B (Collaborator)
    ┌──────────────────────┐              ┌──────────────────────┐
    │  RimeDeck Desktop    │              │  RimeDeck Desktop    │
    │  ┌────────────────┐  │              │  ┌────────────────┐  │
    │  │ UI (Electron)  │  │   JWT /      │  │ UI (Electron)  │  │
    │  │ issues, agents │  │   session    │  │ issues, agents │  │
    │  └────────────────┘  │◄────────────►│  │ (same data!)   │  │
    │  ┌────────────────┐  │              │  └────────────────┘  │
    │  │ Server + PG    │◄─┼── all API ──┼──    /api/*           │
    │  │ workspace data │  │              │                      │
    │  └────────────────┘  │              │  Local server idles  │
    │  ┌────────────────┐  │              │  (data preserved)    │
    │  │ Local Daemon   │  │              └──────────────────────┘
    │  └────────────────┘  │
    └──────────────────────┘

    ✅ Remote user sees full workspace UI
    ✅ Can create issues, manage agents, view inbox
    ❌ Does not contribute compute (add runtime separately)
```

**Setup flow**: Server generates invite code → shares with collaborator → collaborator enters server address + invite code in "Join workspace" dialog → account created + member added → frontend switches to remote server API.

#### Combined: Full Collaboration

A collaborator who both operates the workspace UI *and* contributes compute performs both flows:

1. **Invite member** (get workspace UI access)
2. **Add computer** (contribute runtime compute)

```
    Machine A (Server)                    Machine B (Full Collaborator)
    ┌──────────────────────┐              ┌──────────────────────┐
    │  Server + PG         │              │  ┌────────────────┐  │
    │  ┌────────────────┐  │   JWT        │  │ UI → A's API   │  │
    │  │ workspace data │◄─┼─────────────┼──│ (full access)   │  │
    │  └────────────────┘  │              │  └────────────────┘  │
    │                      │   mdt_       │  ┌────────────────┐  │
    │  Task queue ─────────┼─────────────┼──│ Daemon → A      │  │
    │                      │              │  │ (runs tasks)    │  │
    └──────────────────────┘              │  └────────────────┘  │
                                          └──────────────────────┘
```

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

| Directory                 | Content                      |
| ------------------------- | ---------------------------- |
| `~/.rimedeck/config.json` | CLI configuration            |
| `~/.rimedeck/pg/data/`    | PostgreSQL data              |
| `~/.rimedeck/workspaces/` | Agent execution environments |

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
