# RimeDeck: Multica Local-Only Desktop Fork

> Status: Draft
> Date: 2026-06-03
> Upstream: https://github.com/multica-ai/multica
> Goal: Fork Multica into a standalone desktop product "RimeDeck" -- open the app, everything starts automatically, no Docker, no cloud dependency

## 1. Overview

RimeDeck is a rebranded fork of Multica's desktop app, reconfigured to run as a fully self-contained local application. The Go backend and PostgreSQL database launch automatically as embedded subprocesses when the user opens the app.

**Two parallel concerns**:
1. **Local-only runtime** -- embed PG + Go backend into the Electron app lifecycle
2. **Rebranding** -- rename the product surface from "Multica" to "RimeDeck" while keeping upstream merge friction minimal

## 2. Rebranding Strategy

### 2.1 The Two-Layer Rule

Upstream Multica will continue to ship features. We need to pull those patches regularly. A full find-and-replace of every `multica` string creates conflicts on every merge. Instead, we split the codebase into two layers:

| Layer | Naming | Rationale |
|-------|--------|-----------|
| **Product surface** (user-visible) | `rimedeck` / `RimeDeck` | Users see the new brand |
| **Internal code** (packages, imports, env vars, DB names) | Keep `multica` | Zero upstream merge conflicts |

The user never sees a package.json `name` field or a `MULTICA_*` env var. They see the app title, the dock icon, the deep-link protocol, the login screen, and the data directory. That is the rename surface.

### 2.2 What Gets Renamed

| Category | From | To | Files Affected |
|----------|------|----|----------------|
| App identity | `appId: ai.multica.desktop` | `appId: ai.rimedeck.app` | `electron-builder.yml` |
| Product name | `productName: Multica` | `productName: RimeDeck` | `electron-builder.yml`, `package.json` |
| Window title | `<title>Multica</title>` | `<title>RimeDeck</title>` | `renderer/index.html` |
| App name (code) | `app.setName("Multica")` | `app.setName("RimeDeck")` | `src/main/index.ts` |
| Dev app name | `"Multica Canary"` | `"RimeDeck Dev"` | `src/main/index.ts` |
| Deep-link protocol | `multica://` | `rimedeck://` | `index.ts`, `electron-builder.yml` |
| User data directory | `~/.multica/` | `~/.rimedeck/` | `daemon-manager.ts`, new local-backend code |
| Login page text | `"Sign in to Multica"` | `"Sign in to RimeDeck"` | `packages/views/auth/` i18n keys |
| Helper agent name | `"Multica Helper"` | `"RimeDeck Helper"` | `packages/views/workspace/` |
| Icon component | `MulticaIcon` | Keep (internal name) | No change -- it's just a CSS asterisk |
| Artifact names | `multica-desktop-*` | `rimedeck-*` | `electron-builder.yml` |
| macOS WM_CLASS | `StartupWMClass: Multica` | `StartupWMClass: RimeDeck` | `electron-builder.yml` |
| GitHub release target | `multica-ai/multica` | `{our-org}/rimedeck` | `electron-builder.yml`, `cli-bootstrap.ts` |
| Recovery dialog | `"Multica needs to reload"` | `"RimeDeck needs to reload"` | `renderer-recovery.ts` |
| Daemon status text | `"multica CLI not found"` | `"RimeDeck engine not found"` | `daemon-settings-tab.tsx` |
| Cloud default URLs | `api.multica.ai` | Remove (local-only, no cloud defaults) | `shared/runtime-config.ts` |

### 2.3 What Stays as `multica` (Internal)

These are high-churn upstream files. Renaming them creates merge conflicts on every sync:

| Keep As-Is | Why |
|-----------|-----|
| `@multica/*` package names | Root `package.json`, all `pnpm-workspace.yaml` references, every `import from "@multica/..."` across 200+ files. Renaming is 500+ line diff. |
| `MULTICA_*` env vars | Go backend reads these. Renaming means forking `server/cmd/server/main.go`. |
| `multica` DB name / user / password | PG credentials are internal. User never sees them. |
| `localStorage` keys (`multica_token`, `multica_*`) | Renderer-internal. Renaming causes logout on upgrade. |
| Go module path (`github.com/multica-ai/multica/server`) | Renaming means rewriting every Go import line (~100 files). |
| CLI binary name (`multica` / `multica.exe`) | Internal binary spawned by main process. User never invokes it directly. |
| sqlc types, handler names, service names | All server-internal. |

**Result**: ~30 files change for rebranding (product surface). ~0 files change in the Go backend or shared packages (internal layer).

### 2.4 Rebranding Conflict Surface

On upstream merge:

| File We Changed | Upstream Change Likelihood | Conflict Risk |
|----------------|--------------------------|---------------|
| `electron-builder.yml` | Low (stable config) | Low -- our changes are in value fields, not structure |
| `src/main/index.ts` | Medium (feature additions) | Low -- our changes are in string literals + the local-backend insertion |
| `renderer/index.html` | Very low | None |
| `shared/runtime-config.ts` | Low | Low -- we remove cloud defaults, they might add fields |
| `daemon-manager.ts` | Medium | Low -- we change `~/.multica` path string and one error message |
| `renderer-recovery.ts` | Very low | None |
| i18n / UI text files | Medium | Low -- we change specific string values |

**Estimated merge conflict rate**: ~10-15% of upstream merges touch a file we renamed strings in. Conflicts are trivial (re-apply our string constant change).

## 3. Design Principles

### 3.1 Upstream-Mergeable Fork

This is a living fork, not a one-time copy. Every design decision is evaluated against "how painful is the next `git merge upstream/main`?"

- **Zero changes to SQL queries, migrations, or sqlc-generated code.** These are the highest-churn upstream files.
- **Zero changes to Go backend business logic** (`server/internal/`). The server binary is consumed as-is.
- **New code lives in new files/directories**, namespaced under `apps/desktop/src/main/local-backend/`. Upstream additions never collide.
- **Rebranding changes are string-literal-only.** No structural refactors. A merge conflict is always "our string vs their string," never "our architecture vs their architecture."

### 3.2 Minimal Blast Radius

Only two areas gain meaningful changes:
1. `apps/desktop/src/main/local-backend/` -- new directory, ~10 files (local runtime)
2. ~30 files with string literal changes (rebranding)

### 3.3 Local-Only by Default

Unlike the original Multica desktop app (cloud-first with optional local daemon), RimeDeck is local-first. There is no cloud mode. This simplifies the design: no runtime mode detection needed. The app always starts the local backend stack.

## 4. Architecture

```
RimeDeck App Launch
  |
  v
[Splash Screen] -- "Starting RimeDeck..."
  |
  v
[PostgresManager]
  |  1. Resolve PG binary (bundled > managed > PATH)
  |  2. initdb (first run only)
  |  3. pg_ctl start -w
  |  4. createdb + pgcrypto extension
  |  5. Health check: pg_isready
  |
  v
[MigrationRunner]
  |  Shell out: `multica migrate up` with DATABASE_URL
  |
  v
[BackendManager]
  |  1. Spawn Go server binary as child process
  |  2. Pass DATABASE_URL, PORT, JWT_SECRET via env
  |  3. Health check: GET /health
  |
  v
[DaemonManager] -- existing upstream code, unchanged
  |  Connects to localhost:{backendPort}
  |
  v
[Renderer loads] -- existing upstream code
  |  API URL injected via runtime config IPC
```

### 4.1 Comparison with Upstream Desktop Flow

| Step | Upstream Multica Desktop | RimeDeck |
|------|------------------------|----------|
| PG database | External (Docker or remote) | Embedded subprocess |
| Go backend | External (remote cloud API) | Embedded subprocess |
| Daemon (CLI) | Subprocess (existing) | Subprocess (existing, unchanged) |
| Migrations | Manual (`make migrate-up`) | Auto on launch |
| Auth | Cloud email/OAuth | Local fixed verification code |
| Runtime config | `desktop.json` points to cloud | Hardcoded localhost URLs |

## 5. Component Design

### 5.1 PostgresManager

**File**: `apps/desktop/src/main/local-backend/postgres-manager.ts`

#### Binary Strategy

| Priority | Source | Path | Size |
|----------|--------|------|------|
| 1 | Bundled with app | `resources/pg/{platform}/bin/` | ~50-80MB compressed |
| 2 | Managed (auto-downloaded) | `{userData}/pg/bin/` | Downloaded on first run |
| 3 | System PATH | `which postgres` | 0 (user-installed) |

Initial implementation: system PATH + managed download. Bundled PG is a later optimization.

#### Data Directory

```
~/.rimedeck/
  config.json              # ports, jwt_secret, first-run timestamp
  pg/
    data/                  # PG data directory (initdb output)
    log/                   # PG server logs
  uploads/                 # LOCAL_UPLOAD_DIR for file attachments
  backups/                 # pg_dump output
  daemon/                  # CLI daemon profile (replaces ~/.multica/profiles/desktop-*)
    config.json
    daemon.log
```

#### Lifecycle

```typescript
interface PostgresManager {
  start(): Promise<{ port: number; connectionString: string }>;
  stop(): Promise<void>;
  connectionString(): string;
  status(): PostgresStatus;
}
```

**Startup sequence**:

1. **Resolve binary**: Check bundled -> managed -> PATH. If none found, show install instructions.
2. **Check data dir**: If `~/.rimedeck/pg/data/` doesn't exist, run `initdb --auth=trust --encoding=UTF8 -D {dataDir}`.
3. **Configure**: Write `postgresql.conf` overrides:
   - `listen_addresses = '127.0.0.1'` (localhost only)
   - `port = {configured_port}`
   - `max_connections = 30`
   - `shared_buffers = 128MB`
   - `unix_socket_directories = ''`
   - `logging_collector = on`
   - `log_directory = '{logDir}'`
4. **Start**: `pg_ctl start -D {dataDir} -w -t 30`
5. **Create database**: `createdb -h 127.0.0.1 -p {port} multica` (DB name stays `multica` -- internal, matches upstream migrations)
6. **Create extension**: `psql -c "CREATE EXTENSION IF NOT EXISTS pgcrypto"` on the `multica` database
7. **Health check**: `pg_isready -h 127.0.0.1 -p {port}` polling

**Shutdown**: `pg_ctl stop -D {dataDir} -m fast` on `before-quit`.

#### Error Handling

| Scenario | Behavior |
|----------|----------|
| Port in use | Increment port, retry, persist new port |
| Stale PID file | `pg_ctl stop` first, then start |
| Corrupted data dir | Show "Reset database?" dialog |
| initdb fails | Show error + install instructions |
| Out of disk space | Surface PG error log in UI |

### 5.2 MigrationRunner

**File**: `apps/desktop/src/main/local-backend/migration-runner.ts`

Reuses the bundled `multica` CLI binary (same one `daemon-manager.ts` resolves):

```typescript
async function runMigrations(connectionString: string): Promise<void> {
  const cliBinary = await resolveCliBinary(); // reuse from daemon-manager
  await execFileAsync(cliBinary, ["migrate", "up"], {
    env: { ...process.env, DATABASE_URL: connectionString },
    timeout: 60_000,
  });
}
```

CLI binary is still named `multica` internally -- the user never sees or invokes it.

### 5.3 BackendManager

**File**: `apps/desktop/src/main/local-backend/backend-manager.ts`

```typescript
interface BackendManager {
  start(pgConnectionString: string): Promise<{ port: number; apiUrl: string; wsUrl: string }>;
  stop(): Promise<void>;
  status(): BackendStatus;
}
```

**Environment variables passed to the Go server**:

```bash
# Internal names stay "multica" -- the Go binary reads these
DATABASE_URL=postgres://multica:multica@127.0.0.1:{pgPort}/multica?sslmode=disable
PORT={backendPort}
JWT_SECRET={auto-generated-on-first-run}
CORS_ALLOWED_ORIGINS=http://localhost:{backendPort}
LOCAL_UPLOAD_DIR=~/.rimedeck/uploads
MULTICA_DEV_VERIFICATION_CODE=000000
APP_ENV=local
ALLOW_SIGNUP=true
```

Note: `MULTICA_*` env var names are kept because the Go binary reads them. Renaming would require forking the Go code.

**Process management**:
- `child_process.spawn` for long-running process
- stdout/stderr piped to `~/.rimedeck/backend.log`
- On `before-quit`: SIGTERM -> 5s grace -> SIGKILL

### 5.4 Integration into Electron Main Process

**Modified file**: `apps/desktop/src/main/index.ts`

Since RimeDeck is local-only (no cloud mode), the integration is unconditional:

```typescript
// In app.whenReady(), BEFORE createWindow():
const localBackend = await setupLocalBackend();

// Inject local URLs into runtime config
runtimeConfigResult = {
  ok: true,
  config: {
    apiUrl: localBackend.apiUrl,      // http://localhost:{port}
    wsUrl: localBackend.wsUrl,        // ws://localhost:{port}/ws
    appUrl: localBackend.apiUrl,
  },
};

createWindow();
setupAutoUpdater(() => mainWindow);
setupDaemonManager(() => mainWindow); // unchanged -- reads injected runtime config
```

**Shutdown chain** (in `before-quit`):
1. Stop daemon (existing upstream code)
2. Stop Go backend (new)
3. Stop PostgreSQL (new)

### 5.5 DaemonManager Changes

The upstream `daemon-manager.ts` is mostly unchanged. Two string-level changes:

1. **Data directory**: `~/.multica` -> `~/.rimedeck` (the `homedir()` + `.multica` path references)
2. **Profile directory**: daemon profile lives under `~/.rimedeck/daemon/` instead of `~/.multica/profiles/desktop-*`

The daemon connects to `localhost:{backendPort}` via the runtime config. No logic changes.

### 5.6 Renderer Changes

**Zero structural changes.** The renderer reads API URLs from the runtime config IPC (injected by main process). String-level changes only:

- `<title>RimeDeck</title>`
- Login page header text (via i18n keys)
- Error messages referencing "Multica" -> "RimeDeck"

## 6. Upstream Sync Strategy

### 6.1 Repository Setup

```bash
# Fork structure
git clone https://github.com/{our-org}/rimedeck.git
cd rimedeck
git remote add upstream https://github.com/multica-ai/multica.git
```

### 6.2 Conflict Surface Analysis

| Area | What We Changed | Conflict Risk |
|------|----------------|---------------|
| `server/` (all Go code) | Nothing | None |
| `server/migrations/` | Nothing | None |
| `packages/core/` | Nothing | None |
| `packages/ui/` | Nothing | None |
| `packages/views/` | ~5 string literals (UI text) | Very low |
| `apps/desktop/src/main/local-backend/` | New directory (ours) | None (new files) |
| `apps/desktop/src/main/index.ts` | String renames + local-backend insertion | Low |
| `apps/desktop/src/main/daemon-manager.ts` | Path strings (`~/.multica` -> `~/.rimedeck`) | Low |
| `apps/desktop/electron-builder.yml` | App identity values | Low |
| `apps/desktop/src/renderer/index.html` | Title tag | None |
| `apps/desktop/src/shared/runtime-config.ts` | Remove cloud defaults | Low |
| `Makefile`, `scripts/`, `docker-compose.yml` | Nothing | None |

### 6.3 Merge Workflow

```bash
# Regular sync (weekly or on interesting upstream releases)
git fetch upstream
git merge upstream/main

# Expected conflict pattern:
# 1. String literals we renamed (trivial: re-apply our string)
# 2. Structural changes near our index.ts insertion point (rare, easy)
# 3. New files in apps/desktop/src/main/ (no conflict, auto-merge)
```

### 6.4 What Makes This Work

1. **Go backend is a black box.** We compile it, bundle it, run it. Never modify its source.
2. **Migrations are a black box.** We run `multica migrate up`. Never modify migration files.
3. **Shared packages are a black box.** `@multica/core`, `@multica/ui`, `@multica/views` imported as-is.
4. **Our additions are in a new directory.** `local-backend/` doesn't exist upstream.
5. **Rebranding is string-only.** No structural refactors, no file renames, no import path changes.

### 6.5 Upstream Feature Inheritance

When upstream adds a new feature (e.g., new agent type, new issue field):

| What Upstream Ships | What We Get Automatically |
|-------------------|--------------------------|
| New SQL migration | Applied on next app launch by MigrationRunner |
| New Go API endpoint | Available (server binary rebuilt from merged code) |
| New frontend component | Visible (shared packages merged in) |
| New env var required | Needs attention: add to BackendManager env if mandatory |
| New external dependency (Redis feature) | Gracefully degrades (Redis is already optional upstream) |

**The only thing that needs manual attention on merge**: new mandatory server env vars. We monitor `server/cmd/server/main.go` for new `os.Getenv()` calls that cause `os.Exit(1)` on missing values.

## 7. File Structure

### 7.1 New Files (Local Backend)

```
apps/desktop/src/main/local-backend/
  index.ts                 # setupLocalBackend() orchestrator
  postgres-manager.ts      # PG binary resolution + lifecycle
  backend-manager.ts       # Go server process management
  migration-runner.ts      # Database migration execution
  port-utils.ts            # Random port allocation + persistence
  config.ts                # LocalConfig read/write (~/.rimedeck/config.json)
  pg-binary-resolver.ts    # PG binary download/resolution

apps/desktop/src/main/local-backend/__tests__/
  port-utils.test.ts
  config.test.ts
```

### 7.2 Modified Files (Rebranding + Integration)

```
# Rebranding (string changes only)
apps/desktop/electron-builder.yml            # appId, productName, protocol, artifacts
apps/desktop/package.json                    # name, productName, description
apps/desktop/src/main/index.ts               # PROTOCOL, app name, appUserModelId + local-backend call
apps/desktop/src/main/daemon-manager.ts      # ~/.multica -> ~/.rimedeck paths
apps/desktop/src/main/renderer-recovery.ts   # dialog title
apps/desktop/src/main/cli-bootstrap.ts       # GitHub release URL -> our repo
apps/desktop/src/main/cli-release-asset.ts   # archive prefix if we rename CLI artifacts
apps/desktop/src/renderer/index.html         # <title>
apps/desktop/src/renderer/src/App.tsx         # error message text
apps/desktop/src/renderer/src/components/daemon-settings-tab.tsx  # status text
apps/desktop/src/shared/runtime-config.ts    # remove cloud defaults

# UI text
packages/views/auth/login-page.tsx (or i18n key)         # "Sign in to RimeDeck"
packages/views/workspace/welcome-after-onboarding.tsx     # "RimeDeck Helper"
```

**Total**: ~10 new files + ~15 files with string changes. Zero structural changes.

## 8. Packaging & Distribution

### 8.1 Binary Assets

| Asset | Platform | Size | Source |
|-------|----------|------|--------|
| `multica` CLI/server binary | per-platform | ~15-20MB | Built from merged upstream Go code |
| PostgreSQL 17 binaries | per-platform | ~50-80MB | Managed download or bundled |

### 8.2 electron-builder.yml (RimeDeck Version)

Key changes:

```yaml
appId: ai.rimedeck.app
productName: RimeDeck
protocols:
  - name: RimeDeck
    schemes:
      - rimedeck
mac:
  artifactName: rimedeck-${version}-mac-${arch}.${ext}
linux:
  executableName: rimedeck
  desktop:
    entry:
      StartupWMClass: RimeDeck
  artifactName: rimedeck-${version}-linux-${arch}.${ext}
win:
  artifactName: rimedeck-${version}-windows-${arch}.${ext}
publish:
  provider: github
  owner: {our-org}
  repo: rimedeck
```

### 8.3 PG Binary Procurement

**Phase 1** (managed download, recommended): First launch downloads PG binaries to `{userData}/pg/`. Same pattern as `cli-bootstrap.ts`.

**Phase 2** (bundled): Build-time script downloads and strips PG binaries into `resources/pg/{platform}/bin/`. Added to `extraResources` in electron-builder.yml.

### 8.4 App Icons

New icon assets in `apps/desktop/build/`:
- `icon.icns` (macOS)
- `icon.ico` (Windows)
- `icon.png` (Linux, multiple sizes in `build/icons/`)

## 9. Security

| Concern | Mitigation |
|---------|-----------|
| PG network exposure | `listen_addresses = '127.0.0.1'` only |
| PG authentication | `trust` for localhost (only local backend connects) |
| JWT secret | Auto-generated 64-char hex, persisted in `~/.rimedeck/config.json` |
| Data at rest | User home directory, OS file permissions |
| Backend CORS | `http://localhost:{port}` only |
| Auth bypass | `MULTICA_DEV_VERIFICATION_CODE=000000` (server is localhost-only) |

## 10. Implementation Phases

### Phase 0: Fork & Rebrand (1 week)

- [ ] Fork repo, set up upstream remote
- [ ] String-level rebranding (~15 files)
- [ ] New app icons
- [ ] Update electron-builder.yml (appId, productName, protocol, publish target)
- [ ] Update `daemon-manager.ts` paths (`~/.multica` -> `~/.rimedeck`)
- [ ] Remove cloud default URLs from runtime-config.ts
- [ ] Verify build: `pnpm --filter @multica/desktop build && pnpm --filter @multica/desktop package`
- [ ] Verify upstream merge: `git fetch upstream && git merge upstream/main` (should be clean)

### Phase 1: Core Local Backend (2-3 weeks)

- [ ] `PostgresManager` with system PATH detection
- [ ] `BackendManager` for Go server subprocess
- [ ] `MigrationRunner` using CLI binary
- [ ] `setupLocalBackend()` orchestrator in `index.ts`
- [ ] Graceful shutdown chain (daemon -> backend -> PG)
- [ ] Unit tests for config, port utils
- [ ] End-to-end test: app launch -> login -> create workspace -> create issue

### Phase 2: Managed PG Install (1-2 weeks)

- [ ] `pg-binary-resolver.ts` with platform-specific download
- [ ] Checksum verification
- [ ] First-run download progress in splash screen
- [ ] Retry on failed downloads
- [ ] macOS ad-hoc code signing for downloaded binaries

### Phase 3: Bundled PG + Polish (2 weeks)

- [ ] Build-time PG binary bundling script
- [ ] Splash screen with startup progress
- [ ] Database reset / export / import UI
- [ ] Local backend health indicator in status bar
- [ ] Error recovery dialogs
- [ ] PG tuning based on system RAM

## 11. Risks & Mitigations

| Risk | Prob. | Impact | Mitigation |
|------|-------|--------|-----------|
| PG binary incompatibility across OS versions | Med | High | Test on macOS 12+, Windows 10+, Ubuntu 20.04+ |
| Upstream refactors `index.ts` structure | Med | Low | Our insertion is 5 lines + a function call; easy to re-apply |
| Upstream adds mandatory env var to server | Med | Low | Monitor `main.go` for new `os.Exit` on missing env; add to BackendManager |
| pg_bigm extension unavailable | High | Low | Wrap pg_bigm migrations in try/catch; CJK search degrades to LIKE |
| PG startup >10s on slow disk | Low | Low | Show progress UI, parallelize with non-dependent work |
| Data corruption on app force-kill | Low | Med | PG WAL auto-recovery; periodic pg_dump backup |
| Upstream renames `multica` CLI binary | Very Low | Med | Single reference point in `daemon-manager.ts`; trivial to update |
| @multica/* package scope changes upstream | Very Low | High | Would break upstream too; extremely unlikely |

## 12. What We Intentionally Do NOT Change

- **No SQLite migration.** 140 migrations + 50 query files deeply use PG-specific features (JSONB, SKIP LOCKED, recursive CTEs, triggers).
- **No Go backend source modifications.** Server binary consumed as a black box.
- **No @multica/* package renames.** Internal scope, user-invisible, 500+ line diff for zero user value.
- **No MULTICA_* env var renames.** Go binary reads them, renaming means forking server code.
- **No localStorage key renames.** Internal, renaming causes data loss on upgrade.
- **No renderer structural changes.** It already adapts to injected API URLs.

## 13. Open Questions

1. **pg_bigm**: Upstream migrations create `pg_bigm` indexes for CJK search. Extension requires compilation on most platforms.
   - **Recommendation**: Skip for now. Patch MigrationRunner to catch pg_bigm errors. CJK search falls back to LIKE.

2. **Server binary**: Should we build a separate `rimedeck-server` binary, or keep using the `multica` CLI binary (internal, user-invisible)?
   - **Recommendation**: Keep `multica` binary name internally. One less thing to diverge from upstream. The user never sees it.

3. **Auto-update**: PG data dir compatibility across bundled PG versions.
   - PG minor updates (17.x -> 17.y): data-compatible, no action needed.
   - PG major updates (17 -> 18): requires `pg_upgrade`. Defer until needed.

4. **CLI release assets**: Should we publish our own `rimedeck-cli-*` release artifacts, or keep downloading upstream `multica-cli-*`?
   - **Recommendation**: Publish our own. The `cli-bootstrap.ts` GitHub URL already needs to point to our repo. Build the CLI from our merged codebase.

## Appendix A: Upstream Merge Cheat Sheet

```bash
# Setup (once)
git remote add upstream https://github.com/multica-ai/multica.git

# Sync
git fetch upstream
git merge upstream/main

# If conflicts appear, they will be in:
# 1. String literals we renamed -> re-apply our "RimeDeck" strings
# 2. Near our index.ts insertion -> re-insert our setupLocalBackend() call
# 3. runtime-config.ts -> re-remove cloud defaults
#
# Never in:
# - server/**
# - packages/core/**
# - packages/ui/**
# - apps/desktop/src/main/local-backend/** (our dir, upstream doesn't have it)

# After merge, rebuild
make build                                          # Go server binary
pnpm --filter @multica/desktop build                # Electron app
pnpm --filter @multica/desktop package              # Package for distribution
```

## Appendix B: Data Directory Layout

```
~/.rimedeck/
  config.json              # { pgPort, backendPort, jwtSecret, firstRunAt }
  pg/
    data/                  # PostgreSQL data directory
    log/                   # PostgreSQL logs
  uploads/                 # File attachments (LOCAL_UPLOAD_DIR)
  backups/                 # pg_dump snapshots
  daemon/
    config.json            # CLI daemon config (token, server_url)
    daemon.log             # Daemon logs
    .desktop-user-id       # User ID sidecar
  desktop_prefs.json       # Daemon auto-start/stop prefs
```
