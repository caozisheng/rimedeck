# RimeDeck Feature: Export Issue as Markdown

> **Status**: Design  
> **Date**: 2026-06-06  
> **Scope**: Web + Desktop

---

## 1. Background

收件箱中的每条通知最终指向一个 Issue，Issue 的详情页将评论、状态变更、Agent 执行记录等折叠在一条时间线里。用户希望把这些完整记录导出为 Markdown 文件保存到本地，用于归档、分享、或离线阅读。

### 核心诉求

- 一键导出整个 Issue 的完整工作记录（元数据 + 描述 + 时间线）
- 输出格式为人类可读的 Markdown
- 放在 Issue 详情页的 `...` 菜单里，与已有的 Copy Link、Copy Workdir Path 同级

---

## 2. UX Design

### 2.1 入口位置

在 `IssueActionsMenuItems` 的 "Copy Link" 和 "Copy Local Workdir Path" 之间（或之后），新增一个菜单项：

```
├── Pin/Unpin
├── Copy Link
├── Copy Local Workdir Path
├── Export as Markdown          ← NEW
├── ─────────────── (separator)
├── More (submenu)
```

- **图标**: `Download` (lucide-react)
- **文案**: i18n key `$.actions.export_markdown`
  - en: "Export as Markdown"
  - zh: "导出为 Markdown"
  - ja: "Markdownとしてエクスポート"
  - ko: "Markdown으로 내보내기"

### 2.2 交互流程

```
用户点击 "Export as Markdown"
  │
  ├─ Desktop (Electron)
  │   └─ 调用 desktopAPI.saveFile(suggestedName, content)
  │      → 弹出系统原生 Save Dialog
  │      → 保存到用户选择的路径
  │      → toast.success("文件已保存")
  │
  └─ Web
      └─ 构造 Blob → 创建临时 <a download> 触发浏览器下载
         → 默认文件名: `{identifier}-{slugified-title}.md`
         → toast.success("文件已下载")
```

**默认文件名**: `PROJ-42-fix-login-timeout.md`
- `identifier`: Issue 的项目前缀 + 编号（如 `PROJ-42`）
- `slugified-title`: 标题转 kebab-case，截断至 50 字符

---

## 3. Markdown Output Format

```markdown
# PROJ-42: Fix login timeout

| Field | Value |
|-------|-------|
| Status | In Progress |
| Priority | High |
| Assignee | @alice |
| Creator | @bob |
| Project | Backend |
| Start Date | 2026-05-28 |
| Due Date | 2026-06-10 |
| Created | 2026-05-27 14:30 |
| Updated | 2026-06-05 09:15 |

## Labels

`bug`, `auth`

## Description

(Issue description in original Markdown)

---

## Timeline

### 2026-05-27 14:30 — @bob created this issue

### 2026-05-27 14:35 — @alice commented

> We should check the session refresh logic first.

### 2026-05-27 15:00 — Status changed: Backlog → In Progress

### 2026-05-28 10:00 — Agent @coder completed task

<details>
<summary>Agent transcript (12 events, 3m 24s)</summary>

**[1] Thinking**
Analyzing the session refresh flow...

**[2] Tool: read_file**
Input: `src/auth/session.ts`

**[3] Agent**
Found the issue — the refresh token TTL is hardcoded to 5 minutes...

</details>

### 2026-06-01 09:00 — @alice commented

> Fixed in PR #128. Moving to review.

### 2026-06-01 09:01 — Status changed: In Progress → In Review
```

### 3.1 格式规则

| 内容类型 | 渲染方式 |
|---------|---------|
| Issue 元数据 | Markdown table |
| Labels | 逗号分隔的 inline code |
| Description | 原文输出（已是 Markdown） |
| Comment | `> ` blockquote，保留原文格式 |
| Activity (status/priority/assignee change) | 单行文字描述 |
| Coalesced activities | 展开为独立条目 |
| Agent transcript | `<details>` 折叠块，内含事件列表 |
| Attachments | `[filename](url)` 链接（不下载文件本身） |
| Reactions | 追加在 comment 末尾：`Reactions: 👍×3 🎉×1` |
| Resolved thread | 标注 `(Resolved)` |

### 3.2 时间线排序

- 按 `created_at` 升序（最早的在上面），与 Issue 详情页默认排序一致

---

## 4. Technical Design

### 4.1 Architecture

```
issue-actions-menu-items.tsx       — 菜单入口
  └─ useExportIssueMarkdown.ts     — hook: 组装数据 + 生成 Markdown + 触发下载
       ├─ buildIssueMarkdown.ts    — 纯函数: Issue + Timeline → Markdown string
       └─ downloadMarkdownFile.ts  — 平台适配: Electron save dialog / Web blob download
```

### 4.2 Data Dependencies

导出需要以下数据，全部已有现成的 API / query：

| Data | Source | Query Key |
|------|--------|-----------|
| Issue 基础信息 | 已在 `IssueActionsMenuItems` props 中 | — |
| Timeline (comments + activities) | `api.listIssueTimeline(issueId)` | `issueKeys.timeline(id)` |
| Agent tasks (for transcript) | `api.listTasksByIssue(issueId)` | `issueKeys.tasks(id)` |
| Task messages (transcript detail) | `api.listTaskMessages(taskId)` | per-task lazy fetch |
| Members / Agents (actor names) | workspace members + agents cache | 已在全局 cache |

### 4.3 Key Files

#### `packages/views/issues/export/build-issue-markdown.ts`

纯函数，无副作用，可单元测试：

```typescript
interface ExportContext {
  issue: Issue;
  timeline: TimelineEntry[];
  tasks: AgentTask[];
  taskMessages: Map<string, TaskMessagePayload[]>;
  resolveActorName: (type: string, id: string) => string;
  labels: Label[];
}

function buildIssueMarkdown(ctx: ExportContext): string;
```

主要逻辑：
1. 渲染 header (title + metadata table)
2. 渲染 labels section
3. 渲染 description
4. 按时间排序 timeline entries
5. 遍历 entries:
   - `type === "comment"` → blockquote 格式 + reactions
   - `type === "activity"` → 单行描述，调用 `formatActivityAction()` 翻译 action 字段
6. Agent task transcript → `<details>` block，复用 `buildTimeline()` + `redactSecrets()`

#### `packages/views/issues/export/download-markdown-file.ts`

```typescript
function downloadMarkdownFile(filename: string, content: string): Promise<void>;
```

- Desktop: `window.desktopAPI.saveFile(filename, content)` — 需要在 preload 中新增 `saveFile` IPC
- Web: `Blob` + 临时 `<a>` 元素 + `.click()` + `URL.revokeObjectURL()`

#### `packages/views/issues/export/use-export-issue-markdown.ts`

```typescript
function useExportIssueMarkdown(issue: Issue): {
  exportMarkdown: () => Promise<void>;
  isExporting: boolean;
};
```

点击时：
1. 并行 fetch timeline + tasks（优先取 cache）
2. 对每个 task 并行 fetch messages（transcript 内容）
3. 调用 `buildIssueMarkdown()` 生成内容
4. 调用 `downloadMarkdownFile()` 触发下载
5. toast 反馈

### 4.4 Desktop IPC Extension

当前 `desktopAPI` 仅有 `downloadURL(url)` 用于下载远程文件。导出 Markdown 是本地生成的字符串，需要新增：

```typescript
// preload/index.ts
saveFile: (suggestedName: string, content: string, mimeType?: string) 
  => Promise<{ cancelled: boolean; filePath?: string }>

// main process handler
ipcMain.handle("file:save", async (_e, suggestedName, content, mimeType) => {
  const { canceled, filePath } = await dialog.showSaveDialog(mainWindow, {
    defaultPath: suggestedName,
    filters: [{ name: "Markdown", extensions: ["md"] }],
  });
  if (canceled || !filePath) return { cancelled: true };
  await fs.writeFile(filePath, content, "utf-8");
  return { cancelled: false, filePath };
});
```

### 4.5 Performance Considerations

- **Transcript fetch 是最重的部分。** 一个 Issue 可能有多个 Agent task，每个 task 可能有数百条 messages。
- 策略：默认包含 transcript（完整导出）。如果 task 数量 > 5 或总 message 数 > 1000，在 `<details>` summary 中标注 `(truncated to last 200 events)` 并截断。
- Timeline 和 tasks 通常已在 cache 中（用户刚看过 Issue 详情），大部分情况下只有 task messages 需要额外请求。
- 生成 Markdown 字符串是 CPU-bound 但数据量有限（通常 < 1MB），无需 worker。

---

## 5. Implementation Plan

### Phase 1: Core Export (MVP)

1. **`build-issue-markdown.ts`** — 纯函数 + 单元测试
   - 元数据 table
   - Description
   - Comments (blockquote)
   - Activities (单行)
   - 不含 transcript

2. **`download-markdown-file.ts`** — Web-only (Blob download)

3. **`use-export-issue-markdown.ts`** — hook

4. **Menu item** — 在 `issue-actions-menu-items.tsx` 添加入口

5. **i18n** — en / zh / ja / ko

### Phase 2: Agent Transcript

6. 在 `buildIssueMarkdown` 中集成 `buildTimeline()` + `redactSecrets()`
7. 每个 task 的 transcript 渲染为 `<details>` 折叠块

### Phase 3: Desktop Native Save

8. 新增 `file:save` IPC handler
9. `download-markdown-file.ts` 增加 Electron 分支
10. 使用系统原生 Save Dialog

---

## 6. Out of Scope

- **批量导出**: 一次导出多个 Issue（未来可扩展）
- **PDF 导出**: Markdown 已足够，PDF 可通过外部工具转换
- **附件下载**: 仅输出链接，不打包附件文件
- **Mobile**: 移动端暂不支持（缺少文件系统写入能力）
- **自定义模板**: 固定格式，不提供用户自定义
- **Inbox 批量导出**: 从 inbox 列表直接批量导出（需要先导航到 issue 详情）

---

## 7. Testing

| Layer | Coverage |
|-------|----------|
| `buildIssueMarkdown` | Unit tests: 各种 entry type 的格式化、空值处理、特殊字符转义 |
| `download-markdown-file` | 手动测试 Web 下载 + Desktop save dialog |
| `useExportIssueMarkdown` | Integration test: mock API, 验证 fetch 调用和状态管理 |
| E2E | 手动: 打开 Issue → `...` → Export as Markdown → 验证下载文件内容 |
