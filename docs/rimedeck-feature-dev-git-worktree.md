# 设计方案：基于 Git Worktree 的多用户并发任务隔离

> **状态**: 草案  
> **日期**: 2026-06-10  
> **范围**: Daemon 任务执行层 + Server 调度层 + 前端状态展示

---

## 1. 问题陈述

### 1.1 执行模型

当用户 B 受邀加入用户 A 的工作区时，**共用用户 A 的 Daemon**。所有任务（无论由谁发起）都由同一个 Daemon 进程调度和执行：

```
┌─────────────────────────────────────────────────┐
│              用户 A 的机器                        │
│                                                  │
│  ┌──────────────────────────────────────────┐    │
│  │            Daemon（单进程）               │    │
│  │                                          │    │
│  │  MaxConcurrentTasks = 20                 │    │
│  │                                          │    │
│  │  ┌────────┐ ┌────────┐ ┌────────┐       │    │
│  │  │ Task-1 │ │ Task-2 │ │ Task-3 │ ...   │    │
│  │  │用户A发 │ │用户B发 │ │用户A发 │       │    │
│  │  └────────┘ └────────┘ └────────┘       │    │
│  │       │          │          │            │    │
│  │   worktree-1  worktree-2  worktree-3    │    │
│  │                                          │    │
│  │  共享 bare clone (repocache)             │    │
│  └──────────────────────────────────────────┘    │
│                                                  │
│  用户B通过 Web UI 远程提交任务                    │
│  但执行全部发生在用户A的机器上                    │
└─────────────────────────────────────────────────┘
```

### 1.2 关键推论

因为是同一 Daemon：
- **bare clone 天然共享**：`repocache.Cache` 是进程级单例，所有任务共用
- **`LocalPathLocker` 天然生效**：进程内互斥锁已覆盖 local_directory 并发
- **fetch 时机同步**：每次 `CreateWorktree` 前都会 `git fetch origin`

### 1.3 那真正的冲突在哪？

单 Daemon 并发执行多任务时，冲突发生在 **git 操作层面**，而非文件系统层面：

| 场景 | 当前行为 | 问题 |
|------|---------|------|
| **Repo checkout 模式**：两个任务同时操作同一仓库 | 各自 worktree 隔离，互不干扰 | PR 提交时可能 base 过时导致合并冲突 |
| **Local_directory 模式**：两个任务指向同一本地目录 | `localPathLocks` 串行化，第二个任务等待 | 串行等待影响吞吐量；用户 B 的任务可能被用户 A 的长任务阻塞 |
| **混合模式**：一个 repo checkout + 一个 local_directory 指向同一仓库 | 无协调 | local_directory 任务不经过 worktree，可能与 repo checkout 任务修改同一文件 |

**最典型的痛点场景**：

```
T1: 用户A 创建 issue → Agent 分配 → worktree-1 (agent/fix-bug/aaa)
T2: 用户B 创建 issue → Agent 分配 → worktree-2 (agent/add-feat/bbb)
T3: 两个 Agent 并行工作，同时修改 server/handler.go
T4: Agent-1 完成 → 推送分支 → 创建 PR → 合并
T5: Agent-2 完成 → 推送分支 → 创建 PR → ❌ 合并冲突
    此时用户B不知道为什么冲突，也不知道用户A的任务改了什么
```

---

## 2. 目标

1. **保留现有隔离优势**: Worktree 的任务级隔离已经很好，不破坏它
2. **跨任务感知**: 让并行任务知道彼此的存在及修改范围
3. **冲突前置预警**: 在两个任务同时修改同一文件前告警，而非等到 PR 合并失败
4. **local_directory 升级**: 为 local_directory 模式提供 worktree 化选项，消除串行瓶颈
5. **向后兼容**: 单用户场景行为不变

---

## 3. 现有基础设施盘点

系统已具备完善的 worktree 基础设施：

### 3.1 已实现（直接可用）

| 组件 | 位置 | 功能 | 多用户是否够用？ |
|------|------|------|----------------|
| `repocache.Cache` | `repocache/cache.go` | Bare clone 缓存 + per-repo 互斥锁 | ✅ 单 Daemon 共享 |
| `Cache.CreateWorktree()` | 同上 :385 | worktree 创建，支持 ref 指定、重入更新 | ✅ 每任务独立分支 |
| `Cache.Sync()` | 同上 :103 | repo 同步（clone/fetch） | ✅ 共享 |
| `LocalPathLocker` | `local_directory.go` | 本地路径互斥锁 | ✅ 同 Daemon 有效 |
| `installCoAuthoredByHook()` | `repocache/cache.go:817` | Co-authored-by git hook | ✅ |
| `removeGitWorktree()` | `execenv/git.go:103` | Worktree + branch 清理 | ✅ |
| 任务并发信号量 | `daemon.go:1875` | `MaxConcurrentTasks` 控制总并发 | ✅ |

### 3.2 缺口

| 缺口 | 影响 |
|------|------|
| 无跨任务 modified_files 感知 | 两个并行任务修改同一文件时无人知晓 |
| 任务状态不携带 git 分支信息 | UI 上看不到各任务工作在哪个分支 |
| local_directory 模式只能串行 | 用户 B 的任务被用户 A 的长任务阻塞 |
| Agent system prompt 不注入并行上下文 | Agent 不知道同一 repo 上还有谁在工作 |
| 任务完成后无 base 过时检测 | PR 合并冲突只能事后发现 |

---

## 4. 方案设计

### 4.1 并行任务感知 — Active Task Registry

**核心**: Server 端维护同一仓库上活跃任务的注册表，让 Daemon 在创建 worktree 时能查询并行任务。

#### 数据模型

```sql
CREATE TABLE active_task_branches (
    task_id        UUID PRIMARY KEY REFERENCES agent_tasks(id),
    workspace_id   UUID NOT NULL REFERENCES workspaces(id),
    repo_url       TEXT NOT NULL,
    branch_name    TEXT NOT NULL,
    base_ref       TEXT NOT NULL,
    creator_id     UUID NOT NULL REFERENCES users(id),  -- 谁发起的任务
    status         TEXT NOT NULL DEFAULT 'active',       -- active|pushed|merged|abandoned
    modified_files TEXT[] DEFAULT '{}',                  -- 任务完成时填充
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_atb_active_repo
    ON active_task_branches(workspace_id, repo_url)
    WHERE status = 'active';
```

#### Daemon 侧流程

```go
// CreateWorktree 成功后，上报分支信息
func (d *Daemon) reportTaskBranch(ctx context.Context, task Task, wt *repocache.WorktreeResult, baseRef string) {
    _ = d.client.RegisterTaskBranch(ctx, task.ID, RegisterTaskBranchRequest{
        RepoURL:    repoURL,
        BranchName: wt.BranchName,
        BaseRef:    baseRef,
    })
}

// 在构建 Agent prompt 之前，查询同一 repo 上的活跃分支
func (d *Daemon) getParallelTaskContext(ctx context.Context, task Task) string {
    branches, err := d.client.ListActiveTaskBranches(ctx, task.WorkspaceID, repoURL)
    if err != nil || len(branches) == 0 {
        return ""
    }

    var sb strings.Builder
    sb.WriteString("## Parallel Tasks on This Repository\n\n")
    sb.WriteString("The following tasks are working on the same repo concurrently:\n")
    for _, b := range branches {
        if b.TaskID == task.ID {
            continue // 排除自己
        }
        sb.WriteString(fmt.Sprintf("- Branch `%s` (by %s, base: %s)\n",
            b.BranchName, b.CreatorName, b.BaseRef))
        if len(b.ModifiedFiles) > 0 {
            sb.WriteString(fmt.Sprintf("  Modified files: %s\n",
                strings.Join(b.ModifiedFiles, ", ")))
        }
    }
    sb.WriteString("\nPlease coordinate to avoid modifying the same files.\n")
    return sb.String()
}
```

#### Prompt 注入

在 `BuildPrompt()` 中追加并行任务上下文：

```go
// server/internal/daemon/prompt.go
func BuildPrompt(task Task, provider string) string {
    // ... 现有逻辑 ...

    // 追加并行任务警告（由 handleTask 提前查询好，存入 task 上下文）
    if task.ParallelTaskContext != "" {
        prompt += "\n\n" + task.ParallelTaskContext
    }
    return prompt
}
```

### 4.2 任务状态增强 — Git 元数据上报

扩展 `AgentTask` 携带 worktree 信息，让前端展示每个任务工作在哪个分支。

#### Server 端

```sql
ALTER TABLE agent_tasks ADD COLUMN git_branch TEXT DEFAULT '';
ALTER TABLE agent_tasks ADD COLUMN git_base_ref TEXT DEFAULT '';
```

#### API

```go
// POST /api/daemon/tasks/{taskId}/git-info
type TaskGitInfoRequest struct {
    Branch  string `json:"branch"`
    BaseRef string `json:"base_ref"`
}
```

#### 前端

```typescript
// packages/core/types/chat.ts
export interface AgentTask {
    // ... 现有字段 ...
    git_branch?: string;
    git_base_ref?: string;
}
```

执行日志中展示：

```
🔀 分支: agent/fix-auth/a1b2c3d4 (base: origin/main @ abc1234)
⚠️ 并行: 用户B的"add-payment"任务正在同一仓库的 agent/add-payment/e5f6 分支工作
```

### 4.3 Local Directory Worktree 化 — 消除串行瓶颈

当前 local_directory 模式下，`localPathLocks` 将同一路径的任务串行化。多用户场景中，用户 B 的任务可能被用户 A 的长时间任务阻塞很久。

#### 方案: local_directory + worktree 混合模式

在用户的本地目录中检测到 git 仓库时，自动创建 worktree 而非直接操作主工作树：

```
用户 A 的本地仓库: /Users/alice/project/
    ├── .git/
    ├── src/
    └── .worktrees/              ← 新增
        ├── task-aaa/            ← 用户A任务的 worktree
        │   ├── src/
        │   └── ...
        └── task-bbb/            ← 用户B任务的 worktree
            ├── src/
            └── ...
```

#### Daemon 侧实现

```go
// server/internal/daemon/local_directory.go

// localDirectoryWorktreeMode 检查 local_directory 是否为 git 仓库，
// 如果是，则在其下创建 worktree 而非直接操作。
func (d *Daemon) resolveLocalDirectoryWorkDir(
    ctx context.Context,
    absPath string,
    task Task,
) (workDir string, cleanup func(), err error) {

    gitRoot, isGit := detectGitRepo(absPath)
    if !isGit {
        // 非 git 目录，退回串行模式
        return absPath, nil, nil
    }

    // 在用户仓库下创建 worktree
    worktreePath := filepath.Join(gitRoot, ".worktrees", shortID(task.ID))
    branchName := fmt.Sprintf("agent/%s/%s",
        sanitizeName(task.Agent.Name), shortID(task.ID))
    baseRef := getRemoteDefaultBranch(gitRoot)

    if err := setupGitWorktree(gitRoot, worktreePath, branchName, baseRef); err != nil {
        return "", nil, fmt.Errorf("local_directory worktree: %w", err)
    }

    cleanup = func() {
        removeGitWorktree(gitRoot, worktreePath, branchName, d.logger)
    }

    return worktreePath, cleanup, nil
}
```

#### 并行化收益

| 指标 | 改前（串行） | 改后（worktree 并行） |
|------|------------|---------------------|
| 两个 10 分钟任务总耗时 | 20 分钟 | ~10 分钟 |
| 用户 B 等待时间 | 0~10 分钟 | 0 |
| 磁盘开销 | 无额外 | 每个 worktree ~= 仓库大小（git 硬链接优化后很小） |

#### 可选：用户开关

在 Project Resource 配置中增加选项，让用户选择 local_directory 的并发策略：

```typescript
interface LocalDirectoryResourceRef {
    daemon_id: string;
    local_path: string;
    concurrency_mode: "serial" | "worktree";  // 新增，默认 serial 保持向后兼容
}
```

### 4.4 冲突检测与通知

#### 4.4.1 实时 Modified Files 追踪

Agent 执行过程中可通过进度事件上报当前修改的文件（部分 Agent CLI 支持）。作为备选，在任务完成后获取：

```go
// 任务完成回调中，收集 worktree 的 modified files
func getWorktreeModifiedFiles(worktreePath string) []string {
    cmd := exec.Command("git", "-C", worktreePath, "diff", "--name-only", "HEAD")
    out, err := cmd.Output()
    if err != nil {
        return nil
    }
    return strings.Split(strings.TrimSpace(string(out)), "\n")
}
```

#### 4.4.2 交叉文件冲突检测

Server 端在任务完成时检查是否与其他活跃任务有文件交叉：

```go
// server/internal/service/conflict_detector.go

func (s *ConflictDetector) OnTaskCompleted(ctx context.Context, task AgentTask) {
    if len(task.ModifiedFiles) == 0 {
        return
    }

    // 查询同一 repo 上仍在活跃的任务分支
    activeBranches, _ := s.queries.ListActiveTaskBranches(ctx,
        task.WorkspaceID, task.RepoURL, "active")

    for _, branch := range activeBranches {
        overlap := intersectFiles(task.ModifiedFiles, branch.ModifiedFiles)
        if len(overlap) > 0 {
            // 在 issue 上发评论告警
            s.postConflictWarning(ctx, ConflictWarning{
                CompletedTask:   task,
                ActiveTask:      branch,
                OverlappingFiles: overlap,
            })
        }
    }
}
```

告警形式（issue comment）：

```markdown
⚠️ **潜在冲突检测**

任务 "fix-auth-bug" (by 用户A) 刚完成并修改了以下文件：
- `server/handler.go`
- `server/middleware.go`

当前任务 "add-payment" (by 用户B) 也在修改 `server/handler.go`。

建议：在创建 PR 前先 rebase 到最新的 main 分支。
```

### 4.5 Push 后的 Base 更新策略

当一个任务完成并合并 PR 后，同一 repo 上其他进行中的任务 base 已过时。

#### 策略：通知 + Agent 自治

不做自动 rebase（风险过高），采用被动通知：

1. **任务 A 完成推送后**: Server 更新 `active_task_branches` 表，标记 A 为 `pushed`
2. **Server 检测 base 变化**: 对同一 repo 上其他 `active` 任务生成内部事件
3. **Daemon 收到事件后**: 对相关任务的 bare clone 执行 `git fetch origin`（已是同一进程，直接调用）
4. **Agent 推送前**: 如果 Agent CLI 支持，在 `git push` 前自动 `fetch + rebase`
5. **推送失败**: Daemon 在 `TaskResult` 中标记 `conflict_on_push`，UI 提示用户介入

```go
// daemon.go — 收到 "repo refs updated" 事件
func (d *Daemon) onRepoRefsUpdated(workspaceID, repoURL string) {
    // 同一进程，直接访问 repoCache
    barePath := d.repoCache.Lookup(workspaceID, repoURL)
    if barePath != "" {
        _ = d.repoCache.Fetch(barePath)
    }
}
```

---

## 5. 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                    Server                                │
│                                                          │
│  ┌──────────────┐  ┌────────────────┐  ┌──────────────┐│
│  │ Task         │  │ Active Branch  │  │ Conflict     ││
│  │ Dispatcher   │  │ Registry       │  │ Detector     ││
│  └──────┬───────┘  └───────┬────────┘  └──────┬───────┘│
│         │                  │                   │        │
│         │    REST API / SSE Events             │        │
└─────────┼──────────────────┼───────────────────┼────────┘
          │                  │                   │
          ▼                  ▼                   ▼
┌─────────────────────────────────────────────────────────┐
│                 Daemon（用户 A 的机器，单进程）            │
│                                                          │
│  ┌─────────────────────────────────────────────────┐    │
│  │              repocache.Cache                     │    │
│  │         (共享 bare clone，per-repo 锁)           │    │
│  └─────────────────────┬───────────────────────────┘    │
│                        │                                 │
│  ┌─────────────────────┼───────────────────────────┐    │
│  │   Repo Checkout 模式 (Worktree 并行)             │    │
│  │                     │                            │    │
│  │  ┌─────────────┐  ┌┴────────────┐               │    │
│  │  │ worktree-1  │  │ worktree-2  │               │    │
│  │  │ task-aaa    │  │ task-bbb    │               │    │
│  │  │ (用户A发起) │  │ (用户B发起) │               │    │
│  │  │ branch:     │  │ branch:     │               │    │
│  │  │ agent/fix/  │  │ agent/add/  │               │    │
│  │  │   aaa       │  │   bbb       │               │    │
│  │  └─────────────┘  └─────────────┘               │    │
│  └──────────────────────────────────────────────────┘   │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │   Local Directory 模式                            │   │
│  │                                                   │   │
│  │   改前：localPathLocks 串行                       │   │
│  │   ┌──────┐ → ┌──────┐ (等待)                     │   │
│  │   │task-1│   │task-2│                             │   │
│  │                                                   │   │
│  │   改后 (worktree 化)：并行                        │   │
│  │   /Users/alice/project/                           │   │
│  │   ├── .worktrees/task-aaa/  ← Agent 1             │   │
│  │   └── .worktrees/task-bbb/  ← Agent 2             │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

---

## 6. 实施路线图

### Phase 1: 可见性（1 周）

最低成本、最高收益——让用户看到并行任务状态。

| 工作项 | 改动范围 | 风险 |
|--------|---------|------|
| agent_tasks 表增加 `git_branch`、`git_base_ref` 列 | DB migration | 低：可选字段 |
| Daemon 在 CreateWorktree 后上报分支信息 | `daemon.go` runTask 流程 | 低：额外 API 调用 |
| 前端展示任务分支信息 | `packages/views/` 任务详情 | 低：只读展示 |
| Active Branch Registry 表 + 查询 API | Server handler + DB | 低：新增只读 API |

### Phase 2: 感知与预警（2 周）

让 Agent 知道并行任务的存在，提前避免冲突。

| 工作项 | 改动范围 | 风险 |
|--------|---------|------|
| handleTask 中查询并行分支，注入 system prompt | `daemon.go`、`prompt.go` | 低：只追加上下文 |
| 任务完成时收集 modified_files 并上报 | Daemon + Server API | 低 |
| 交叉文件检测 + issue comment 告警 | Server service 层 | 中：需定义告警阈值 |
| 前端展示并行任务警告 | 执行日志 UI | 低 |

### Phase 3: Local Directory Worktree 化（2-3 周）

消除 local_directory 串行瓶颈，让多用户任务真正并行。

| 工作项 | 改动范围 | 风险 |
|--------|---------|------|
| `resolveLocalDirectoryWorkDir` 新增 worktree 路径 | `local_directory.go`、`execenv.go` | 中：改变执行路径 |
| Worktree 清理逻辑（任务完成后） | `gc.go`、`execenv/git.go` | 中：需防清理残留 |
| Project Resource 增加 `concurrency_mode` 配置 | DB + API + 前端设置 | 低：用户可选 |
| CleanupSidecars 适配 worktree 目录结构 | `execenv/context.go` | 中 |

### Phase 4: 高级功能（远期）

| 工作项 | 说明 |
|--------|------|
| Push 失败自动 rebase + 重试 | 任务完成后检测 non-fast-forward，自动 rebase |
| PR 依赖链可视化 | 前端展示同一 repo 上的 PR 依赖关系 |
| 热点文件智能调度 | 基于 modified_files 历史，调度时避免两个任务同时修改热点文件 |
| Sparse checkout 优化 | 大型 monorepo 场景下 worktree 只检出相关目录 |

---

## 7. 回答核心问题

### Git Worktree 是否能解决多用户并发冲突？

**Repo Checkout 模式下：已经在用。** 每个任务已经通过 `repocache.CreateWorktree()` 获得独立 worktree 和独立分支。同一 Daemon 共享 bare clone，worktree 之间天然隔离。多用户场景下**文件系统级冲突不存在**。

**真正的痛点不是隔离，而是感知**：

| 问题 | Worktree 解决？ | 实际需要 |
|------|----------------|---------|
| 任务级文件隔离 | ✅ 已解决 | — |
| 两个任务同时改同一文件 | ✅ 文件不冲突 | ❌ 但 PR 合并时冲突，需要前置预警 |
| 用户 B 不知道用户 A 在做什么 | ❌ | Active Branch Registry + UI 展示 |
| Agent 不知道有并行任务 | ❌ | System prompt 注入 |
| Local_directory 串行阻塞 | ❌（不走 worktree） | Local Directory Worktree 化 |

**结论**: Worktree 是已有的隔离基座（repo checkout 模式天然并行），核心缺口是**跨任务的可见性和冲突预警**。对于 local_directory 模式，引入 worktree 化可以消除串行瓶颈，是真正需要新增 worktree 支持的地方。

---

## 8. 数据模型变更汇总

```sql
-- Phase 1: 任务 git 元数据
ALTER TABLE agent_tasks ADD COLUMN git_branch TEXT DEFAULT '';
ALTER TABLE agent_tasks ADD COLUMN git_base_ref TEXT DEFAULT '';

-- Phase 1: 活跃分支注册表
CREATE TABLE active_task_branches (
    task_id        UUID PRIMARY KEY REFERENCES agent_tasks(id) ON DELETE CASCADE,
    workspace_id   UUID NOT NULL REFERENCES workspaces(id),
    repo_url       TEXT NOT NULL,
    branch_name    TEXT NOT NULL,
    base_ref       TEXT NOT NULL,
    creator_id     UUID NOT NULL REFERENCES users(id),
    status         TEXT NOT NULL DEFAULT 'active',
    modified_files TEXT[] DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_atb_active_repo
    ON active_task_branches(workspace_id, repo_url)
    WHERE status = 'active';

-- Phase 3: Local Directory 并发模式（project_resources.resource_ref JSON 扩展）
-- 无需新表，在 resource_ref 中增加 concurrency_mode 字段
```

## 9. API 变更汇总

```
Phase 1:
  POST   /api/daemon/tasks/{taskId}/git-info
         Body: { branch, base_ref }

  GET    /api/workspaces/{wsId}/active-branches?repo_url=...
         → 查询仓库上的活跃任务分支

Phase 2:
  POST   /api/daemon/tasks/{taskId}/modified-files
         Body: { files: string[] }

  (告警通过现有 issue comment API 下发)
```

---

## 10. 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| local_directory worktree 化后 Agent 预期路径变化 | 中 | Agent 运行时 config 路径不对 | 保持 `concurrency_mode` 默认为 `serial`，用户手动开启 |
| modified_files 上报不完整（Agent 中途失败） | 低 | 冲突漏报 | 仅用于告警非阻断；失败任务自动清理分支 |
| worktree 磁盘占用（大仓库多并发任务） | 中 | 用户 A 机器磁盘不足 | Git worktree 硬链接 objects；任务完成后即时清理（现有 GC 已覆盖）|
| Prompt 注入的并行上下文太长 | 低 | 浪费 token | 最多列出 5 个并行任务，超出时摘要 |
| Active Branch Registry 过时（任务异常退出未清理） | 中 | 永远显示「并行任务」 | 关联 agent_tasks 的终态（completed/failed/cancelled）自动清理 |

---

## 附录 A: Worktree 存储空间实测分析

> **日期**: 2026-06-11
> **测试环境**: `~/.rimedeck/workspaces_desktop-127.0.0.1-18080/`（单 workspace，34 个 task）

### A.1 目录结构

```
~/.rimedeck/workspaces_desktop-127.0.0.1-18080/
├── .repos/                                         ← bare git repo 缓存
│   └── 04258f33.../
│       └── github.com+caozisheng+rimecraft.git     ← 所有 worktree 共享
│
└── 04258f33.../                                    ← workspace 根目录
    ├── <8位hex-task-id>/                           ← task 目录（共 34 个）
    │   ├── .gc_meta.json                           ← GC 元数据
    │   ├── logs/                                   ← 执行日志
    │   ├── output/                                 ← 输出产物
    │   ├── codex-home/                             ← (部分) agent 运行时 home
    │   │   ├── .sandbox-bin/                       ← 沙箱二进制（空间大户）
    │   │   ├── .tmp/                               ← 临时文件
    │   │   ├── logs_1.sqlite / state_5.sqlite
    │   │   ├── config.toml, cap_sid
    │   │   └── sessions/, memories/, plugins/, skills/
    │   └── workdir/                                ← (部分) 代码工作区
    │       ├── .git → bare repo worktree link      ← git worktree 链接文件
    │       ├── CLAUDE.md, .claude/, .agent_context
    │       └── rimecraft/                          ← (仅1个有完整 checkout)
    └── ...
```

### A.2 磁盘占用明细

| 组成部分 | 大小 | 占比 | 说明 |
|---------|------|------|------|
| **总计** | **744M** | 100% | 整个 workspaces 目录 |
| bare repo (`.repos/`) | 3.7M | 0.5% | 所有 worktree 共享的 git 对象库 |
| 34 个 task 目录合计 | 741M | 99.5% | |
| ↳ `.sandbox-bin/` (2个 task) | ~374M | **50%+** | agent 沙箱二进制文件，每个 ~187M |
| ↳ `.tmp/` | ~68M | ~9% | 临时文件 |
| ↳ `codex-home/` 其余 (sqlite/sessions) | ~73M×3 | ~30% | 日志数据库、session 记录 |
| ↳ **workdir（git worktree checkout）** | **~6.3M** | **<1%** | 仅 1 个 task 有完整代码 checkout |
| ↳ 其余 workdir (3个) | ~45K | ≈0 | 只含 CLAUDE.md 等元文件 |

### A.3 Worktree 共享机制

```
bare repo (3.7M)                    ← git objects 存储一份
   ├── worktrees/rimecraft/         ← worktree 注册
   │     └── gitdir → task workdir
   │
task workdir/.git (文件,非目录)      ← 指向 bare repo 的 worktree 引用
   内容: "gitdir: .../.repos/.../worktrees/rimecraft"
```

- 所有 worktree 通过 `.git` 文件（非目录）**链接**到同一个 bare repo
- **不会**复制 `.git/objects`，因此每新增一个 worktree 只增加工作树文件大小
- 对于本项目（rimecraft），一次完整 checkout ≈ 6.2M

### A.4 结论

**Git worktree 本身几乎不浪费存储空间。**

1. **共享 objects**: bare repo 3.7M 被所有 task 共享，每个 worktree 仅需工作树文件（源码 checkout），不重复存储 git 历史
2. **实际空间大户是 agent 运行时环境**: `.sandbox-bin/`（187M/个）和 `.tmp/`（68M）占总量 60%+，与 worktree 机制无关
3. **34 个 task 中仅 4 个有 workdir**: 说明 task 完成后 worktree 已被 GC 清理，不会无限膨胀
4. **对比全量 clone**: 如果每个 task 都 `git clone` 一份完整仓库，34 个 task 将产生 34 × (repo size) 的存储开销，远大于当前 worktree 方案

**优化建议**（针对真正的空间消耗）:
- 已完成 task 的 `.sandbox-bin/` 可以更积极地清理
- `.tmp/` 应随 task 生命周期清理
- 多个 task 的 `.sandbox-bin/` 内容相同时可考虑符号链接去重

---

## 附录 B: Task 生命周期中的存储回收机制

> **日期**: 2026-06-11
> **代码版本**: main (c7c6af60)

### B.1 回收发生在三个阶段

#### 阶段 1: 任务执行完毕（即时） — 部分清理

`daemon.go` handleTask 的 defer 中执行：

| 动作 | 代码位置 | 清理内容 |
|------|---------|---------|
| `CleanupRuntimeConfig()` | `execenv/runtime_config.go` | 从 CLAUDE.md / AGENTS.md / GEMINI.md 中删除注入的运行时 brief |
| `CleanupSidecars()` | `execenv/sidecar_manifest.go:260` | 按 `.multica_sidecar_manifest.json` 清单，逐一删除 `.agent_context/`、`.multica/`、`.claude/skills/` 等 Prepare 阶段写入 workdir 的文件 |
| `WriteGCMeta()` | `execenv/execenv.go:436` | 写入 `.gc_meta.json`（kind、issue_id/chat_session_id、completed_at），**不删除任何东西**，仅为后续 GC 留下元数据 |

**此阶段不会删除** task 目录、`codex-home/`、`logs/`、`output/` 或 worktree。

#### 阶段 2: 周期性 GC 循环（延迟回收） — 主要清理

`gc.go:18-48` — `gcLoop` 在 Daemon 启动 30 秒后首次运行，之后每 **1 小时**（`GCInterval`）扫描一次。

**判定逻辑**（按 `.gc_meta.json` 中的 Kind 分派）:

| Kind | 触发 Clean（整目录删除）的条件 | 触发 CleanArtifacts 的条件 |
|------|---------------------------|-------------------------|
| `issue` | issue done/cancelled 且 updated_at > 24h | completed > 12h 但 issue 仍 open |
| `chat` | session 被硬删(404) → 立即; archived 且 > 24h | — |
| `autopilot_run` | terminal 状态且 completed_at > 24h | — |
| `quick_create` | terminal 状态 → **立即**（不等 TTL） | — |
| 无 meta / 404 | mtime > 72h（orphan 兜底） | — |

**三种回收动作**:

| 动作 | 清理范围 |
|------|---------|
| `gcActionClean` | `os.RemoveAll(taskDir)` — 整个 task 目录全删 |
| `gcActionCleanArtifacts` | 遍历 taskDir，删除匹配 `GCArtifactPatterns` 的子目录 |
| `gcActionOrphan` | 等同 Clean |

**local_directory 特殊处理** (`gc.go:184-208`): 永远不会被 Clean 整删，最多降级为 CleanArtifacts，保留 `logs/` 和 `output/` 供用户审计。

**默认 TTL 配置** (`config.go`):

```
GCInterval       = 1h      扫描间隔
GCTTL            = 24h     done/cancelled 后多久整目录删除
GCOrphanTTL      = 72h     孤儿目录多久后删除
GCArtifactTTL    = 12h     completed 后多久删可再生产物
ArtifactPatterns = [node_modules, .next, .turbo, .sandbox-bin]
```

#### 阶段 3: Worktree 引用清理（每次 GC 末尾）

`gc.go:83` + `gc.go:570-611` — 每次 GC 循环结束后，对所有 bare repo 执行 `git worktree prune`，清理已不存在 worktree 的 stale 引用。

### B.2 时间线总结

```
Task 开始
  │
  ├─ Prepare: 创建 envRoot/workdir, codex-home, sidecars
  │
  ▼
Task 执行中（worktree + codex-home 完整存在）
  │
  ▼
Task 完成 ──────────────────────────── 即时清理
  │  ├─ CleanupSidecars     → 删 .agent_context, .multica, provider skills
  │  ├─ CleanupRuntimeConfig → 删 CLAUDE.md 中注入的 brief
  │  └─ WriteGCMeta          → 写 .gc_meta.json（打时间戳）
  │
  │  此时 task 目录仍保留: logs/, output/, codex-home/, workdir/
  │
  ▼
+12h ──────────────────────────────── Artifact 清理（如 issue 仍 open）
  │  └─ 删 node_modules, .next, .turbo, .sandbox-bin
  │
  ▼
+24h（issue done/cancelled 后）────── 整目录删除
  │  └─ os.RemoveAll(taskDir) — 一切皆删
  │
  ▼
同一 GC 周期末尾 ─────────────────── git worktree prune
     └─ 清理 bare repo 中的 stale worktree 引用
```

### B.3 优化: 将 .sandbox-bin 纳入 GCArtifactPatterns

**问题**: `.sandbox-bin/` 内含 Codex CLI 沙箱二进制（`codex.exe` 187M + `codex-command-runner.exe` 755K），不是 agent 产出而是 Codex CLI 首次启动时自动释放的运行时文件。默认 `GCArtifactPatterns` 仅覆盖 `node_modules`、`.next`、`.turbo`，不包含 `.sandbox-bin`，导致即使 `ArtifactTTL`（12h）过期也无法回收。

**修复**: 在 `server/internal/daemon/config.go:68` 将 `.sandbox-bin` 加入默认模式列表：

```go
var DefaultGCArtifactPatterns = []string{"node_modules", ".next", ".turbo", ".sandbox-bin"}
```

**冷启动安全性**: `.sandbox-bin/` 被清理后，如果同一 task 目录被复用（如 issue 新评论触发 follow-up task 走 Reuse 路径），Codex CLI 再次启动时发现 `.sandbox-bin/codex.exe` 不存在，会自动重新释放二进制。这是 Codex CLI 的正常冷启动路径，代价仅为首次启动多花几秒解压时间。

**收益**: 每个 task 回收 ~187M，多 task 场景下（如 34 个 task）可节省数 GB 磁盘空间。
