<img width="256" height="256" alt="rimedeck-icon" src="https://github.com/user-attachments/assets/b6d42b07-33d3-4045-ac63-05b708c03025" />

# RimeDeck

Local-first AI agent workbench — manage issues, orchestrate AI coding agents, and run deterministic SOPs, all in one desktop app. Zero Docker, zero cloud dependency. Forked from [Multica](https://github.com/multica-ai/multica).

<details open>
<summary>📸 Screenshot</summary>
<img width="630" height="400" alt="image" src="https://github.com/user-attachments/assets/116bf358-e8bb-4b0a-a3dd-c553a5a86222" />
</details>

---

## Design Highlights

### Agent = Person, Skill = Knowledge, SOP = Capability

Three-layer architecture that mirrors how a human team works:

| Layer | What it is | How it's delivered |
|-------|-----------|-------------------|
| **Agent** | An AI entity with identity, model, and instructions | Bound to one of 16 runtime CLIs |
| **Skill** | Reusable knowledge (code review checklist, language conventions) | Injected into system prompt — passive knowledge |
| **SOP** | A deterministic DAG pipeline (HTTP → LLM → filter → doc gen) | Injected into runtime config — active capability the agent calls on demand |

### SOP-as-MCP: Agent-Triggered Deterministic Pipelines

SOPs (Standard Operating Procedures) are pre-built DAGs executed server-side by the [RuleGo](https://github.com/rulego/rulego) engine. Non-LLM nodes (HTTP calls, JS filters, document generation) run at **zero token cost**; only LLM nodes consume tokens.

**Dual-path injection** ensures all 16 runtimes can discover and trigger SOPs:

```
Path 1 (primary):  SOP list → CLAUDE.md / AGENTS.md → agent reads natively
Path 2 (auxiliary): SOP MCP server → McpConfig → agent sees trigger_sop tool
```

The agent **decides autonomously** whether to trigger an SOP based on the user's request — no server-side intent matching or hardcoded commands.

### 16 Runtime CLIs, One Unified Interface

| Runtime | CLI | Provider |
|---------|-----|----------|
| Antigravity | `agy` | Google |
| Claude Code | `claude` | Anthropic |
| Codex | `codex` | OpenAI |
| Copilot | `copilot` | GitHub / Microsoft |
| Cursor | `cursor-agent` | Cursor |
| Gemini CLI | `gemini` | Google |
| Hermes | `hermes` | NousResearch |
| Kimi | `kimi` | Moonshot AI |
| Kiro CLI | `kiro-cli` | Amazon |
| OMP | `omp` | Community |
| OpenCode | `opencode` | Community |
| OpenClaw | `openclaw` | Community |
| Pi | `pi` | Community |
| Qoder | `qoder` | Qodo |
| Qwen Code | `qwen-code` | Alibaba |
| CodeBuddy | `codebuddy` | Tencent |

All runtimes share the same `Backend` interface — `Execute(ctx, prompt, ExecOptions)`. Skills, SOPs, MCP config, and system prompts are injected uniformly via `ExecOptions` and per-task runtime config files.

### Local-First, Network-Optional

- **Embedded PostgreSQL** + Go server as Electron child processes — double-click to launch
- **Compute sharing** — add remote machines as headless compute nodes via daemon token
- **Workspace collaboration** — invite members over LAN / Tailscale / VPN with full UI access
- **Backup & restore** — export agents, skills, SOPs, squads as a single JSON file

### Squad-Based Multi-Agent Orchestration

A **Squad** is a team with one leader agent and member agents/users. When an issue is assigned to a squad, the leader claims it, breaks the work down, and delegates sub-tasks via `@mention` — no centralized orchestrator, just agent-to-agent communication on the issue thread.

---

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                       RimeDeck App                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────┐  │
│  │ Electron │  │ Go Server│  │PostgreSQL│  │   Daemon     │  │
│  │   (UI)   │◄►│  (API)   │◄►│  (Data)  │  │(Task Runner) │  │
│  └──────────┘  └──────────┘  └──────────┘  └──────┬───────┘  │
└───────────────────────────────────────────────────┼───────────┘
                                                    │
         ┌──────────────────────────────────────────┤
         ▼                                          ▼
  ┌─────────────┐                           ┌─────────────┐
  │   Agent A   │                           │   Agent B   │
  │ claude CLI  │                           │ codex CLI   │
  │             │                           │             │
  │ Skills: ────┤  injected into            │ Skills: ────┤
  │  Go Review  │  CLAUDE.md / AGENTS.md    │  TS Expert  │
  │  Security   │                           │  Test TDD   │
  │             │                           │             │
  │ SOPs: ──────┤  listed in runtime config │ SOPs: ──────┤
  │  竞品监控   │  + MCP tool (if supported) │  周报生成   │
  └─────────────┘                           └─────────────┘
         │                                          │
         ▼                                          ▼
  ┌─────────────────────────────────────────────────────┐
  │              RuleGo Engine (embedded)                │
  │  restApiCall · jsFilter · agentLLM · docGenerate    │
  │  webScrape · rssFetch · spreadsheet · sendEmail     │
  └─────────────────────────────────────────────────────┘
```

---

## Quick Start

**Prerequisites**: Node.js 22+, pnpm 10+, Go 1.24+, PostgreSQL 17

```bash
pnpm install
make dev          # auto-creates env, starts DB, migrates, launches everything
```

### Desktop App

```bash
pnpm dev:desktop                          # dev mode with HMR
pnpm --filter @rimedeck/desktop package   # build for current platform
```

The desktop build bundles Go CLI + embedded PostgreSQL — runs fully offline.

---

## Project Structure

```
apps/desktop/     Electron desktop app
packages/core/    Headless business logic (zero react-dom)
packages/ui/      Atomic UI components (shadcn)
packages/views/   Shared business pages
server/           Go backend (Chi, sqlc, RuleGo, gorilla/ws)
  internal/
    handler/      HTTP handlers (REST API)
    service/      Business logic (SOP engine, task queue, autopilot)
    daemon/       Task runner + runtime config injection
    workflow/     SOP templates + n8n/Dify importers
  pkg/agent/      16 runtime backends (unified Backend interface)
  migrations/     PostgreSQL migrations (127 applied)
```

## License

See [LICENSE](LICENSE).
