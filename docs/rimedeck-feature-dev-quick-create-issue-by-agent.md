# Quick-Create Issue: 前置创建 Issue 设计方案

> Status: Draft
> Date: 2026-06-05
> Author: 软件架构师 Agent

## 1. 问题描述

### 1.1 现状

用户通过 Agent 创建 issue（quick-create modal）时，issue 不会立即出现在 kanban 上。需要等待前一个 task 完成、当前 agent task 被 daemon 处理后，agent 才会运行 `multica issue create`，此时 issue 才真正存在。

### 1.2 根因

当前 quick-create 的流程是：

```
用户 prompt → server 创建 TASK（无 issue） → daemon 串行执行（localPathLocks）→ agent 运行 multica issue create → issue 才存在
```

关键限制：
- `QuickCreateIssue` handler 只调用 `EnqueueQuickCreateTask`，返回 `{ task_id }`，**不创建 issue**
- daemon 的 `localPathLocks` 机制串行化同一 `local_directory` 的 task，后续 task 进入 `waiting_local_directory` 阻塞等待
- issue 对象仅在 agent 运行 `multica issue create --output json` 时才被创建
- agent 通过 `MULTICA_QUICK_CREATE_TASK_ID` 环境变量给 issue 打上 `origin_type=quick_create` 标记

### 1.3 用户体验问题

| 创建方式 | issue 何时存在 | kanban 何时可见 | 用户心智模型 |
|---------|-------------|--------------|-----------|
| 手动创建 | 立刻 | 立刻 | "我创建了 issue" |
| Agent 创建 | agent 处理后 | agent 处理后 | "我创建了 issue"（实际只创建了 task） |

两条路径的用户预期一致（"我创建了 issue"），但行为完全不同。连续快速创建多个 issue 时，第 2 个 issue 可能要等数分钟才出现在 kanban 上。

---

## 2. 目标

- Agent quick-create 后，issue **立即**出现在 kanban（与手动创建一致）
- 多个连续 quick-create 的 issue 全部立即可见，status = `todo`
- Agent 依然负责丰富 issue 内容（优化标题、填写描述、解析优先级等）
- 保持现有 inbox 通知、subscriber 订阅、origin 追踪等机制不被破坏

---

## 3. 当前架构详解

### 3.1 请求入口

```
POST /api/issues/quick-create
```

**Handler:** `server/internal/handler/issue.go` → `QuickCreateIssue()`

请求体 (`QuickCreateIssueRequest`):
```json
{
  "agent_id": "uuid",      // 或 squad_id，二选一
  "squad_id": "uuid",
  "prompt": "用户输入",
  "project_id": "uuid",     // 可选
  "parent_issue_id": "uuid" // 可选
}
```

响应: `202 Accepted` + `{ "task_id": "uuid" }`

### 3.2 Task Context

`QuickCreateContext` 存储在 task 的 `context` JSONB 列：

```json
{
  "type": "quick_create",
  "prompt": "用户输入",
  "requester_id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "squad_id": "uuid",
  "parent_issue_id": "uuid"
}
```

### 3.3 Daemon Claim 阶段

`server/internal/handler/daemon.go` claim 端点：
1. 解析 `QuickCreateContext` 从 task.Context
2. 解析 project title + resources
3. 解析 parent issue identifier
4. 注入 squad leader briefing（如果是 squad）
5. 设置 `resp.QuickCreatePrompt`

### 3.4 Agent Prompt 构建

`server/internal/daemon/prompt.go` → `buildQuickCreatePrompt()`:
- 指示 agent 执行 **恰好一次** `multica issue create --output json`
- 包含字段级规则（title、description、priority、assignee、project、parent）
- Assignee 默认为选择的 agent/squad
- 输出格式: `Created <identifier>: <title>`

### 3.5 Issue Origin 追踪

```
daemon 设置 env: MULTICA_QUICK_CREATE_TASK_ID=<task_id>
→ CLI `multica issue create` 读取 env
→ 创建 issue 时写入 origin_type=quick_create, origin_id=<task_id>
```

### 3.6 Task 完成处理

`server/internal/service/task.go` → `notifyQuickCreateCompleted()`:
1. `GetIssueByOrigin(workspace_id, "quick_create", task_id)` 查找 agent 创建的 issue
2. `LinkTaskToIssue(task_id, issue_id)` 关联 task 与 issue
3. `AddIssueSubscriber(issue_id, requester_id, "creator")` 订阅创建者
4. 创建 inbox item（type = `quick_create_done`）

### 3.7 串行锁

`server/internal/daemon/daemon.go`:
```go
localPathLocks serialises agent tasks whose project resource is a
local_directory pinned to this daemon. Two tasks targeting the same
on-disk path run sequentially.
```

这意味着同一 daemon 上同一目录的多个 quick-create task 必须串行执行。

---

## 4. 方案设计：前置创建 Issue

### 4.1 核心思路

将 quick-create 拆为两步：
1. **Server 立即创建 issue**（占位，状态 `todo`）→ kanban 可见
2. **Enqueue task 关联到已有 issue** → agent 丰富内容

```
当前: prompt → task(无issue) → agent 创建 issue
改后: prompt → server 创建 issue(todo) + task(关联issue) → agent 更新 issue
```

### 4.2 Server 端改动

#### 4.2.1 `QuickCreateIssue` handler 改造

**文件:** `server/internal/handler/issue.go`

```go
func (h *Handler) QuickCreateIssue(w http.ResponseWriter, r *http.Request) {
    // ... 现有的验证逻辑保持不变 ...

    // === 新增: 前置创建 issue ===
    issue, err := h.createQuickCreateIssue(ctx, CreateQuickCreateIssueParams{
        WorkspaceID:   wsUUID,
        CreatorType:   "member",
        CreatorID:     requesterUUID,
        Title:         deriveQuickCreateTitle(req.Prompt),
        Status:        "todo",
        AssigneeType:  assigneeType,  // agent 或 squad
        AssigneeID:    assigneeUUID,  // agent_id 或 squad_id
        ProjectID:     projectUUID,
        ParentIssueID: parentIssueUUID,
    })
    // 广播 issue:created → kanban 立即可见

    // === 改造: task 关联到已有 issue ===
    task, err := h.TaskService.EnqueueQuickCreateTask(ctx, EnqueueParams{
        // ...现有参数...
        IssueID: issue.ID,  // 新增: 关联 issue
    })

    // 响应改为返回 issue 信息
    writeJSON(w, http.StatusAccepted, QuickCreateIssueResponse{
        TaskID:  uuidToString(task.ID),
        IssueID: issue.ID,           // 新增
        Identifier: issue.Identifier, // 新增
    })
}
```

#### 4.2.2 Title 推导

```go
func deriveQuickCreateTitle(prompt string) string {
    // 取 prompt 前 80 个字符，截断到完整词边界
    // 去掉路由指令 ("让 @X 处理", "assign to @X", etc.)
    // 如果 prompt 很短（< 80 chars），直接用作 title
    // 否则截断 + "..."
}
```

> 注意: 这是临时 title，agent 后续会用更好的 title 替换。

#### 4.2.3 `QuickCreateContext` 扩展

```json
{
  "type": "quick_create",
  "prompt": "...",
  "issue_id": "uuid",  // 新增: 前置创建的 issue ID
  ...
}
```

### 4.3 Task 关联改造

#### 4.3.1 `EnqueueQuickCreateTask` 改造

**文件:** `server/internal/service/task.go`

```go
func (s *TaskService) EnqueueQuickCreateTask(ctx, ..., issueID pgtype.UUID) {
    // task 创建时直接关联 issue_id（而非完成时才 link）
    task, err := s.Queries.CreateQuickCreateTask(ctx, Params{
        AgentID:   agentID,
        RuntimeID: agent.RuntimeID,
        Priority:  priorityToInt("high"),
        Context:   contextJSON,
        IssueID:   issueID,  // 新增: 直接关联
    })
}
```

#### 4.3.2 SQL 改造

`CreateQuickCreateTask` SQL 需支持可选的 `issue_id` 参数：

```sql
INSERT INTO agent_task_queue (agent_id, runtime_id, priority, context, issue_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
```

### 4.4 Agent Prompt 改造

#### 4.4.1 `buildQuickCreatePrompt` 改造

**文件:** `server/internal/daemon/prompt.go`

从"创建 issue"变为"更新已有 issue"：

```
当前指令:
  "Your job is to create a well-formed issue with multica issue create"

改后指令:
  "An issue has already been created: {identifier} (id: {issue_id}).
   Your job is to refine it with multica issue update.
   Update the title to be concise and semantically rich.
   Set the description following the two-section structure.
   Set priority if the prompt implies urgency."
```

#### 4.4.2 命令变更

```
当前: multica issue create --title "..." --description "..." ...
改后: multica issue update <issue_id> --title "..." --description "..." ...
```

#### 4.4.3 环境变量

`MULTICA_QUICK_CREATE_TASK_ID` 保留但语义调整：
- 不再用于 `origin_type=quick_create` 标记（issue 已在 server 端创建）
- 可用于 agent 端识别"这是 quick-create 上下文"

### 4.5 完成通知改造

#### 4.5.1 `notifyQuickCreateCompleted` 改造

**文件:** `server/internal/service/task.go`

```go
func (s *TaskService) notifyQuickCreateCompleted(ctx, task, qc) {
    // 不再需要 GetIssueByOrigin —— issue_id 已在 task 上
    issueID := task.IssueID

    // 不再需要 LinkTaskToIssue —— 创建时已关联

    // AddIssueSubscriber 保持不变
    s.Queries.AddIssueSubscriber(issueID, requesterUUID, "creator")

    // Inbox 通知保持不变
    // ...
}
```

#### 4.5.2 失败处理

如果 agent task 失败（agent 未能 update issue）：
- issue 仍然存在于 kanban（title 是临时的占位文本）
- inbox 通知用户 "Agent failed to refine issue, please edit manually"
- 用户可以手动编辑 issue 补充信息

这比当前的失败体验**更好**：当前 agent 失败 = issue 不存在；改后 agent 失败 = issue 存在但信息不完整。

### 4.6 前端改动

#### 4.6.1 `quick-create-issue.tsx`

**文件:** `packages/views/modals/quick-create-issue.tsx`

```typescript
const submit = async () => {
    const res = await api.quickCreateIssue({ ... });
    // 响应现在包含 issue_id 和 identifier
    // 可以直接用于 toast 显示 "Created MUL-123"
    // 不再需要等待 inbox 通知
};
```

#### 4.6.2 缓存更新

**文件:** `packages/core/issues/mutations.ts`

现有的 `useCreateIssue` 已有完善的缓存更新逻辑。方案选择：

**选项 A: 复用 `useCreateIssue` mutation**
- 让 `quickCreateIssue` API 返回完整的 issue 对象
- 前端用 `useCreateIssue` 的 `onSuccess` 逻辑更新缓存
- 优点: 复用现有逻辑；缺点: 需要改造 API 返回值

**选项 B: 在 `quickCreateIssue` 的 success handler 中手动更新缓存**
- 用返回的 issue 对象调用 `addIssueToBuckets` 更新 list cache
- 优点: 改动隔离；缺点: 可能遗漏某些缓存路径

**推荐选项 A**，因为缓存更新逻辑已经经过验证。

---

## 5. 数据流对比

### 5.1 当前流程

```
┌─────────┐    POST /api/issues/quick-create    ┌──────────┐
│  前端    │ ──────────────────────────────────→  │  Server  │
│ (modal) │  ←── 202 { task_id }                 │          │
└─────────┘                                      └────┬─────┘
                                                      │ EnqueueQuickCreateTask
                                                      ▼
                                                 ┌──────────┐
                                                 │  Task DB  │ status=queued
                                                 └────┬─────┘
                                                      │ (等待 daemon 空闲)
                                                      │ (可能阻塞在 localPathLocks)
                                                      ▼
                                                 ┌──────────┐
                                                 │  Daemon   │ claim + run agent
                                                 └────┬─────┘
                                                      │ agent: multica issue create
                                                      ▼
                                                 ┌──────────┐
                                                 │ Issue DB  │ ← issue 此时才存在
                                                 └────┬─────┘
                                                      │ WS: issue:created
                                                      ▼
                                                 ┌──────────┐
                                                 │  Kanban   │ ← 此时才可见
                                                 └──────────┘
```

### 5.2 改造后流程

```
┌─────────┐    POST /api/issues/quick-create    ┌──────────┐
│  前端    │ ──────────────────────────────────→  │  Server  │
│ (modal) │  ←── 202 { task_id, issue }          │          │
└────┬────┘                                      └────┬─────┘
     │                                                │
     │ 缓存更新 (addIssueToBuckets)                    │ ① CreateIssue(todo)
     ▼                                                │ ② WS: issue:created
┌──────────┐                                          │ ③ EnqueueQuickCreateTask(issue_id)
│  Kanban   │ ← 立即可见                               ▼
└──────────┘                                     ┌──────────┐
                                                 │  Task DB  │ status=queued, issue_id=xxx
                                                 └────┬─────┘
                                                      │ (后台异步)
                                                      ▼
                                                 ┌──────────┐
                                                 │  Daemon   │ claim + run agent
                                                 └────┬─────┘
                                                      │ agent: multica issue update
                                                      ▼
                                                 ┌──────────┐
                                                 │ Issue DB  │ title/desc 被丰富
                                                 └────┬─────┘
                                                      │ WS: issue:updated
                                                      ▼
                                                 ┌──────────┐
                                                 │  Kanban   │ ← title 实时更新
                                                 └──────────┘
```

---

## 6. 改动清单

| # | 层级 | 文件 | 改动内容 | 复杂度 |
|---|------|------|---------|-------|
| 1 | Server | `handler/issue.go` | `QuickCreateIssue` 前置创建 issue，响应增加 issue 字段 | 中 |
| 2 | Server | `service/task.go` | `EnqueueQuickCreateTask` 接受 `issueID` 参数 | 低 |
| 3 | Server | `service/task.go` | `notifyQuickCreateCompleted` 改用 task.IssueID 替代 origin 查找 | 低 |
| 4 | Server | SQL / `generated/agent.sql.go` | `CreateQuickCreateTask` 支持 `issue_id` 列 | 低 |
| 5 | Daemon | `daemon/prompt.go` | `buildQuickCreatePrompt` 从 create 改为 update 模式 | 中 |
| 6 | Daemon | `handler/daemon.go` | claim 端点传递 issue_id 到 task response | 低 |
| 7 | CLI | `cmd/multica/cmd_issue.go` | `MULTICA_QUICK_CREATE_TASK_ID` 语义调整（可选） | 低 |
| 8 | Frontend | `core/api/client.ts` | `quickCreateIssue` 响应类型增加 issue 字段 | 低 |
| 9 | Frontend | `views/modals/quick-create-issue.tsx` | submit 后用返回的 issue 更新缓存 | 中 |
| 10 | Frontend | `core/issues/mutations.ts` | 可选: 提取 `addIssueToBuckets` 逻辑复用 | 低 |

---

## 7. 风险与缓解

### 7.1 Agent 失败留下"空壳 issue"

**风险:** agent task 失败后，issue 仍在 kanban 上，title 可能是截断的 prompt 文本。

**缓解:**
- 这比当前行为更好（当前: agent 失败 = issue 完全不存在，用户不知道发生了什么）
- Inbox 通知提示 "Agent failed to refine, please edit manually"
- 用户可以手动编辑或删除

### 7.2 Agent 试图 create 而不是 update

**风险:** 如果 agent prompt 没有正确更新，agent 可能仍然执行 `multica issue create` 而非 `multica issue update`，导致出现重复 issue。

**缓解:**
- `buildQuickCreatePrompt` 明确指示 "Issue already exists: {identifier}, use multica issue update"
- 可选: server 端在 issue 上加 `origin_type=quick_create` 标记，`multica issue create` 如果检测到相同 task_id 的 issue 已存在则报错
- `notifyQuickCreateCompleted` 仍可保留 origin 查找作为兜底

### 7.3 Title 推导质量

**风险:** 服务端的临时 title 可能不够好（纯截断 vs agent 的语义理解）。

**缓解:**
- 用户预期是 "agent 正在处理"，临时 title 只需要可识别即可
- Agent 后续会 update 为更精炼的 title
- 可在 kanban card 上显示 "Agent refining..." 状态指示

### 7.4 并发安全

**风险:** 前置创建 issue 和 enqueue task 之间如果 server crash，会留下孤立 issue（无 task）。

**缓解:**
- 两步操作在同一个 HTTP handler 中，可以用数据库事务包装
- 或接受小概率孤立 issue（用户可手动处理）

### 7.5 向后兼容

**风险:** 旧版 daemon/CLI 仍然期望 quick-create 流程中没有预创建的 issue。

**缓解:**
- `buildQuickCreatePrompt` 在 daemon 侧构建，随 daemon 升级一起生效
- CLI 的 `multica issue update` 已有完整支持
- 老 daemon 不会 claim 新版 server 的 task（版本检查已在 claim 端点）

---

## 8. 附带修复

在排查过程中发现一个独立的缓存 bug：

**`useCreateIssue.onSettled` 缺少 `issueKeys.myAll(wsId)` invalidation**

- 手动创建 issue 后，My Issues 和 Project Detail 页面的 kanban 不会立即更新
- WS handler `onIssueCreated` 正确处理了此路径，但 mutation 侧遗漏
- 已修复（`packages/core/issues/mutations.ts`），tests 全部通过

---

## 9. 实施建议

### Phase 1: 前置创建 issue（核心改动）
- Server handler 改造 + SQL 调整
- Daemon prompt 改造 (create → update)
- 前端响应处理 + 缓存更新
- 预计工时: 2-3 天

### Phase 2: 体验打磨
- Kanban card 上显示 "Agent refining..." 状态指示
- Agent 失败时的 inbox 体验优化
- Title 推导算法优化
- 预计工时: 1-2 天

### Phase 3: 观察与清理
- 监控 agent update 成功率
- 清理 origin_type=quick_create 相关的旧逻辑（如确认不再需要）
- 预计工时: 0.5 天
