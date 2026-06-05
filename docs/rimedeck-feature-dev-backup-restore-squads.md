# RimeDeck Feature: Backup & Restore (Agents / Squads / Skills)

## 背景

RimeDeck 的 agents、squads、skills 全部存储在本地 PostgreSQL 数据库中（`~/.rimedeck/pg/data/`），当前没有导入/导出功能。用户在以下场景需要此功能：

- 迁移到新机器
- 团队间共享配置
- 版本控制 agent 配置
- 灾难恢复

## 数据模型概览

### Agent
| 字段 | 类型 | 备注 |
|------|------|------|
| name | TEXT | workspace 内唯一 |
| description | TEXT | |
| instructions | TEXT | 系统提示词 |
| runtime_mode | TEXT | "local" / "cloud" |
| runtime_config | JSONB | |
| custom_env | JSONB | 环境变量（敏感，单独 API） |
| custom_args | JSONB | CLI 参数 |
| mcp_config | JSONB | MCP 服务器配置 |
| model | TEXT | 模型选择 |
| thinking_level | TEXT | 推理等级 |
| visibility | TEXT | "workspace" / "private" |
| max_concurrent_tasks | INT | 默认 6 |
| skills | M2M | agent_skill 关联表 |

### Skill
| 字段 | 类型 | 备注 |
|------|------|------|
| name | TEXT | workspace 内唯一 |
| description | TEXT | |
| content | TEXT | SKILL.md 内容 |
| config | JSONB | |
| files | 1:N | skill_file 子表 (path + content) |

### Squad
| 字段 | 类型 | 备注 |
|------|------|------|
| name | TEXT | workspace 内唯一 |
| description | TEXT | |
| instructions | TEXT | |
| leader_id | UUID | 引用 agent（RESTRICT 删除） |
| members | 1:N | squad_member (member_type: "agent"/"member") |

### 关键关系
- Squad.leader_id → Agent（必须先恢复 Agent）
- agent_skill → Agent + Skill（多对多）
- squad_member → Squad + Agent/Member

## 导出格式设计

单个 JSON 文件，包含所有资源：

```json
{
  "version": 1,
  "exported_at": "2026-06-05T12:00:00Z",
  "app_version": "0.3.16",
  "skills": [
    {
      "name": "code-review",
      "description": "...",
      "content": "...",
      "config": {},
      "files": [
        { "path": "prompts/review.md", "content": "..." }
      ]
    }
  ],
  "agents": [
    {
      "name": "Bug Fixer",
      "description": "...",
      "instructions": "...",
      "runtime_mode": "local",
      "runtime_config": {},
      "custom_args": [],
      "mcp_config": null,
      "model": "claude-sonnet-4-6",
      "thinking_level": "medium",
      "visibility": "workspace",
      "max_concurrent_tasks": 6,
      "skill_names": ["code-review"]
    }
  ],
  "squads": [
    {
      "name": "Dev Team",
      "description": "...",
      "instructions": "...",
      "leader_name": "Bug Fixer",
      "members": [
        { "member_type": "agent", "name": "Bug Fixer", "role": "member" },
        { "member_type": "member", "email": "alice@example.com", "role": "reviewer" }
      ]
    }
  ]
}
```

### 设计要点

1. **用 name 而非 UUID 做引用** — UUID 在不同实例间不通用
2. **不导出 custom_env** — 包含 API key 等敏感信息，安全起见排除
3. **不导出 runtime_id** — runtime 是本机特定的，导入时让用户选择
4. **不导出 avatar_url** — 可能是本地路径或过期 URL
5. **skill 内联 files** — 保持自包含
6. **不导出已归档资源** — archived_at 非空的 agents/squads 不纳入导出
7. **owner_id / creator_id 不导出** — 导入时自动设为当前用户
8. **squad.leader_name 与 members 职责分离** — `leader_name` 标识谁是 leader，`members` 列出所有成员（含 leader 自身），members 中的 `role` 字段独立于 leader 身份（如 "member"、"reviewer" 等业务角色）
9. **"member" 类型成员用 email 标识** — `member_type: "agent"` 用 `name` 匹配，`member_type: "member"` 用 `email` 匹配目标 workspace 中的成员；若匹配不到则跳过该成员并记录警告

## 恢复顺序

按依赖关系恢复：
1. **Skills** — 无外部依赖
2. **Agents** — 依赖 skills（通过 name 匹配），需要用户指定 runtime_id
3. **Squads** — 依赖 agents（通过 leader_name 匹配）
4. **Squad Members** — 依赖 squads + agents

### 冲突处理策略

同名资源已存在时：
- **默认跳过** — 保留现有，记录跳过
- **可选覆盖** — UI 提供 "覆盖已有" 选项

### 引用缺失处理

- Agent 引用的 `skill_names` 中某 skill 既不在导出文件中、也不在目标 workspace 中 → 跳过该绑定，记录警告
- Squad 的 `leader_name` 对应 agent 不存在 → **整个 squad 导入失败**，记录错误
- Squad members 中某 agent/member 不存在 → 跳过该成员，记录警告

## 实现方案

### Phase 1: 后端 API

在 `server/internal/handler/` 新增两个端点：

```
GET  /api/backup/export    → 导出当前 workspace 的 agents/skills/squads
POST /api/backup/import    → 导入 JSON，返回导入结果摘要
```

> **路由说明**：现有资源路由（`/api/agents`、`/api/skills`、`/api/squads`）均挂在 `/api/` 下，workspace 上下文通过 `RequireWorkspaceContext` 中间件隐式获取。workspace CRUD 在 `/api/workspaces/{id}/...`。新路由使用 `/api/backup/` 前缀以避免与 workspace CRUD 路由混淆。

**导出逻辑**（handler 伪码）：
1. 查询所有**未归档** skills（含 files）
2. 查询所有**未归档** agents（含 skill 关联），排除 `custom_env`
3. 查询所有**未归档** squads（含 members）
4. 组装 JSON，skill/agent 引用用 name，member 类型为 "member" 时附带 email
5. 可选参数：`?agents=name1,name2&skills=name1&squads=name1` 支持选择性导出

**导入逻辑**（单事务）：
1. 解析 JSON，校验 version 和文件大小（上限 10MB）
2. 创建/跳过 skills
3. 前端传入 `runtime_id`（或用 workspace 默认 runtime）
4. 创建/跳过 agents，绑定 skills；`owner_id` / `creator_id` 设为当前用户
5. 创建/跳过 squads，绑定 leader 和 members；`creator_id` 设为当前用户
6. 返回结果摘要：
```json
{
  "created": { "skills": 3, "agents": 2, "squads": 1 },
  "skipped": { "skills": 1 },
  "warnings": [
    "Agent 'Bug Fixer': skill 'missing-skill' not found, binding skipped",
    "Squad 'Dev Team': member 'alice@example.com' not found in workspace, skipped"
  ],
  "errors": []
}
```

### Phase 2: 前端 UI

在 Settings 页面的 Workspace 组下新增 **"Backup" tab**（`?tab=backup`），新建 `packages/views/settings/components/backup-tab.tsx`：

- **注册方式**：在 `settings-page.tsx` 的 `WORKSPACE_TAB_KEYS` 中添加 `"backup"`，对应 icon 使用 `HardDriveDownload`（lucide）
- **Export 按钮** → 调用 `GET /api/backup/export`，浏览器下载 JSON 文件
- **Import 按钮** → 文件选择器，上传 JSON，显示预览摘要（将创建 X 个 agents...），确认后执行 `POST /api/backup/import`
- **Runtime 选择** → 导入 agents 时，弹出选择器让用户指定目标 runtime
- **权限控制** → 复用现有 `canManageWorkspace`（owner / admin）门控，非管理员不可见
- **i18n** → 在 `packages/views/locales/*/settings.json` 中添加以下 key（4 个语言：en / zh-Hans / ja / ko）：

```jsonc
// page.tabs 新增
"backup": "Backup"  // zh-Hans: "备份"

// 新增顶层 "backup" 组
"backup": {
  "section_title": "Backup & Restore",
  "section_description": "Export your workspace's agents, skills, and squads as a JSON file, or import from a previous backup.",
  "export_title": "Export",
  "export_description": "Download all agents, skills, and squads as a single JSON file.",
  "export_button": "Export",
  "exporting": "Exporting...",
  "toast_export_failed": "Failed to export workspace data",
  "import_title": "Import",
  "import_description": "Upload a previously exported JSON file to restore agents, skills, and squads.",
  "import_button": "Import",
  "importing": "Importing...",
  "import_choose_file": "Choose file",
  "import_preview_title": "Import preview",
  "import_preview_description": "The following resources will be created:",
  "import_preview_skills": "{{count}} skills",
  "import_preview_agents": "{{count}} agents",
  "import_preview_squads": "{{count}} squads",
  "import_overwrite_label": "Overwrite existing resources with the same name",
  "import_runtime_label": "Target runtime",
  "import_confirm": "Confirm import",
  "import_cancel": "Cancel",
  "toast_import_success": "Import complete: {{created}} created, {{skipped}} skipped",
  "toast_import_failed": "Failed to import workspace data",
  "toast_import_warnings": "Import completed with {{count}} warnings",
  "manage_hint": "Only admins and owners can backup and restore workspace data."
}
```

### Phase 3: CLI 支持（可选）

```bash
multica backup export --workspace <slug> -o backup.json
multica backup import --workspace <slug> backup.json [--overwrite] [--runtime <id>]
```

> **命名约定**：现有 CLI 命令格式为 `multica <resource> <action>`（如 `multica agent list`、`multica skill import`），新命令使用 `multica backup export/import` 保持一致。

## 关键文件

| 文件 | 用途 |
|------|------|
| `server/internal/handler/agent.go` | 参考现有 Agent CRUD |
| `server/internal/handler/skill.go` | 参考现有 Skill CRUD |
| `server/cmd/server/router.go` | 注册新路由 |
| `packages/core/api/client.ts` | 添加客户端方法 |
| `packages/core/types/agent.ts` | Agent/Skill 类型定义 |
| `packages/core/types/squad.ts` | Squad 类型定义 |
| `packages/views/settings/components/settings-page.tsx` | 注册新 tab（WORKSPACE_TAB_KEYS / icons / content） |
| `packages/views/settings/components/backup-tab.tsx` | **新建** — Backup & Restore UI |
| `packages/views/locales/*/settings.json` | 添加 backup 相关 i18n key |
| `server/internal/handler/agent_template.go` | 参考 Agent 从模板创建的逻辑（含默认值处理） |
| `server/migrations/084_squad.up.sql` | Squad 表结构（leader_id ON DELETE RESTRICT） |
| `server/migrations/008_structured_skills.up.sql` | Skill + skill_file + agent_skill 表结构 |

## 验证

- 导出 → 导入到空 workspace，检查 agents/skills/squads 完整恢复
- 导入同名资源，验证跳过逻辑
- 导入同名资源并开启覆盖模式，验证覆盖逻辑
- 导入含 squad 但缺少 leader agent 的 JSON，验证 squad 整体跳过并返回错误
- Agent 引用不存在的 skill，验证绑定跳过并返回警告
- Squad member 引用不存在的 agent/member，验证该成员跳过并返回警告
- JSON 格式错误/版本不匹配时的错误处理
- 超过 10MB 的文件上传被拒绝
- 选择性导出：只导出指定的 agents/skills/squads
- 已归档资源不出现在导出中
- 导入后 owner_id / creator_id 正确设为当前用户
