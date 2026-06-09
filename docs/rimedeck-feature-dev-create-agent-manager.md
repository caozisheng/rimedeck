# 方案设计：创建智能体 — 新增 Agent Manager 模板

> **状态**: 草案  
> **日期**: 2026-06-09  
> **方案**: A — 小队创建两步流程  
> **范围**: 后端模板 + 小队创建流程改造 + 路由表自动管理 + 路由编辑器  

---

## 1. 背景与动机

### 1.1 问题

RimeDeck 的小队（Squad）功能允许一个 Leader Agent 协调多个成员完成任务。
但目前创建小队 Leader 时，用户需要：

1. 手动创建一个普通 Agent
2. 自行编写 Manager 类型的 system prompt
3. 手动导入 `cn-skill-router`、`multica-cli-operator` 等技能
4. 手动配置小队路由规则

这个过程门槛高、容易出错，且缺乏最佳实践引导。

此外存在「先有鸡还是先有蛋」的体验问题：创建小队需要先选一个 Agent 做
Leader（`leader_id` 必填），但新用户可能还没有适合当 Leader 的 Agent。

### 1.2 目标

将小队创建流程改造为**两步**，在第一步中内联提供 Agent Manager 创建能力，
让用户在一个 Modal 内完成 Leader 创建 + 小队创建，同时自动生成默认路由表。

### 1.3 参考

- `multica-agent-workflow-template/` — Manager Agent 模式设计文档
  - `docs/04-manager-agent-pattern.zh.md` — Manager 角色定义
  - `docs/03-how-to-map-skills.zh.md` — 技能分配原则
  - `docs/05-multica-cli-dispatch.zh.md` — CLI 调度流程
  - `skills/multica-workflow-bootstrapper/SKILL.md` — 引导技能
- `server/internal/handler/squad_briefing.go` — 现有小队 Leader 协议

---

## 2. 现有架构分析

### 2.1 Agent 模板系统

```
server/internal/agenttmpl/
├── types.go          # Template struct 定义
├── loader.go         # embed.FS 加载 + 校验
└── templates/*.json  # 25 个静态模板（Engineering / Writing 分类）
```

**API 端点**（已实现，前端 picker 尚未上线）：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/agent-templates` | GET | 返回所有模板摘要（不含 instructions） |
| `/api/agent-templates/:slug` | GET | 返回模板详情（含 instructions） |
| `/api/agents/from-template` | POST | 从模板创建 Agent + 自动导入技能 |

### 2.2 小队系统

```
Squad {
  id, name, description, instructions,
  leader_id (FK → Agent),
  members: SquadMember[]
}

SquadMember {
  member_type: "agent" | "member",
  member_id, role
}
```

**Leader Briefing 注入链**：
```
ClaimTaskByRuntime (daemon.go)
  → buildSquadLeaderBriefing (squad_briefing.go)
    → squadOperatingProtocol（硬编码协议）
    → buildSquadRoster（成员花名册 + @mention 语法）
    → squad.Instructions（用户自定义路由规则）
```

Leader 在认领任务时，系统自动在其 Instructions 后追加以上三段 briefing。
这意味着 Agent Manager 模板的 instructions 会与 squad briefing 叠加生效。

### 2.3 小队创建现状

小队创建是**单步 Modal**（`packages/views/modals/create-squad.tsx`）：

- 表单字段：name, description, avatar, **leader_id**（必填）, additional members
- Leader 选择器（`LeaderPicker`）只展示**已存在的活跃 Agent**
- 没有内联创建 Agent 的入口
- 创建后自动跳转到小队详情页

**核心约束**：`CreateSquadRequest.leader_id` 必填——**必须先有 Agent 才能建小队**。

---

## 3. 方案选型

### 3.1 候选方案对比

| 维度 | A：两步小队创建 | B+C：模板选择器 + 内联创建 |
|------|--------|--------|
| 新增文件 | 5 | 8 |
| 修改文件 | 3 | 6 |
| Dialog 最大嵌套 | **0**（同一 Modal 内分步） | 3 层（create-squad → TemplatePicker → CreateAgent） |
| 新用户建小队体验 | 一个 Modal 内完成 | 3 层 Dialog 跳转 |
| 已有 Agent 用户 | Step 1 直接选已有 Agent，无感 | 与现有流程一致 |
| 通用模板选择器 | 不包含（只为 Manager 服务） | 包含（所有 25+ 模板可选） |
| 智能体页面独立创建 Manager | 不支持 | 支持 |
| 预估工期 | **5-7 天** | 8-12 天 |

### 3.2 决策：采用方案 A

选择 A 的理由：
- **自然**：用户想建小队 → 在建小队的地方完成一切，不需要先去智能体页面
- **简单**：零 Dialog 嵌套，改动面小，同一个 Modal 内分步
- **聚焦**：只解决「小队需要 Manager」这一个问题，不引入通用模板选择器的额外复杂度

通用 TemplatePicker 可作为后续独立 feature 开发（见 §9）。

---

## 4. 方案设计

### 4.1 总体架构

```
┌─ 新建小队 Modal（两步） ────────────────────────────────┐
│                                                         │
│  Step 1：选择或创建 Leader                                │
│  ┌─────────────────────────────────────────────────┐    │
│  │  ○ 选择已有智能体                                │    │
│  │    LeaderPicker（现有组件，不变）                  │    │
│  │    ├── 我的智能体: Atlas, Mira                   │    │
│  │    └── 工作区智能体: ...                          │    │
│  │                                                  │    │
│  │  ○ 创建 Agent Manager                           │    │
│  │    ┌─ 内联 mini 表单 ───────────────────────┐   │    │
│  │    │  名称:    [队长              ]          │   │    │
│  │    │  Runtime: [▾ 选择运行时设备    ]         │   │    │
│  │    │  Model:   [▾ claude-sonnet-4-6 ]        │   │    │
│  │    └────────────────────────────────────────┘   │    │
│  └─────────────────────────────────────────────────┘    │
│                                          [下一步 →]     │
│                                                         │
│  Step 2：填写小队信息                                     │
│  ┌─────────────────────────────────────────────────┐    │
│  │  名称:    [研发小队            ]                  │    │
│  │  描述:    [负责核心功能开发      ]                 │    │
│  │  头像:    [上传]                                  │    │
│  │  成员:    [+ 添加成员]                            │    │
│  └─────────────────────────────────────────────────┘    │
│                                [← 上一步]  [创建小队]    │
└─────────────────────────────────────────────────────────┘
         ↓ 提交
  1. POST /api/agents/from-template（如果选了「创建 Agent Manager」）
  2. POST /api/squads { leader_id: agent.id }
  3. POST /api/squads/:id/members ×N（附加成员）
  4. PATCH /api/squads/:id { instructions: 路由表 }（自动生成）
         ↓
  跳转到小队详情页
```

### 4.2 Step 1 设计细节

Step 1 是一个**单选组**（radio group），两个选项：

**选项 A：选择已有智能体**
- 复用现有 `LeaderPicker` 组件，不做改动
- 用户选中一个 Agent 后，「下一步」按钮激活

**选项 B：创建 Agent Manager**
- 展示一个内联 mini 表单，包含 3 个字段：
  - **名称**（text input）：默认值 `"Agent Manager"`，用户可改
  - **Runtime**（下拉选择）：复用现有 `RuntimePicker` 组件
  - **Model**（下拉选择）：复用现有 `ModelDropdown` 组件，依赖 runtime 选择
- 模板的 description、instructions、skills 全部走后端 `agent-manager` 模板默认值，
  不在 mini 表单中暴露——**降低认知负担，用户只需要做 3 个决策**
- 用户填完 mini 表单后，「下一步」按钮激活

**切换行为**：
- 两个选项互斥，切换时清空对方的状态
- 默认选中哪个？如果用户有可用 Agent → 默认选 A；如果无 Agent → 默认选 B

### 4.3 提交流程

用户点击「创建小队」后，前端按顺序执行：

```typescript
async function handleCreateSquad() {
  let leaderId: string;

  // Step 1: 确保 Leader 存在
  if (mode === "existing") {
    leaderId = selectedAgentId;
  } else {
    // 从模板创建 Agent Manager
    const result = await api.createAgentFromTemplate({
      template_slug: "agent-manager",
      name: managerName.trim(),
      runtime_id: selectedRuntimeId,
      model: selectedModel,
      visibility: "workspace",
    });
    leaderId = result.agent.id;
  }

  // Step 2: 创建小队
  const squad = await api.createSquad({
    name: squadName.trim(),
    description: squadDescription.trim() || undefined,
    leader_id: leaderId,
    avatar_url: avatarUrl ?? undefined,
  });

  // Step 3: 添加附加成员（并行，容错）
  await Promise.allSettled(
    selectedMembers.map((m) =>
      api.addSquadMember(squad.id, m)
    ),
  );

  // Step 4: 自动生成路由表（如果 Leader 来自 Manager 模板）
  if (mode === "create-manager") {
    const members = await api.listSquadMembers(squad.id);
    const routingTable = generateRoutingTable(
      members.filter((m) => m.member_id !== leaderId)
    );
    await api.updateSquad(squad.id, {
      instructions: routingTable,
    });
  }

  // 跳转到小队详情页
  router.push(wsPaths.squadDetail(squad.id));
}
```

### 4.4 路由表自动管理

#### 4.4.1 初始生成（创建小队时）

当用户通过 Step 1「创建 Agent Manager」建小队时，提交成功后自动生成默认路由表
写入 `squad.instructions`。

路由表基于当前小队成员列表，**按字母顺序排列**，Leader 不出现在表中：

```markdown
<!-- ROUTING_TABLE_START -->
## 成员路由表

| 成员 | 类型 | 角色 | 擅长领域 |
|------|------|------|----------|
| Alice (写作智能体) | agent | 文档撰写 | 文档、翻译、README |
| Bob (编码智能体) | agent | 开发 | 代码编写、Bug 修复、重构 |
| Charlie | member | 审查 | 代码审查、安全审计 |

### 分配规则
- 根据任务类型匹配成员的「擅长领域」列
- 领域为空的成员视为通用，可接收任何任务
- 不确定时使用 cn-skill-router 技能分析后决定
<!-- ROUTING_TABLE_END -->
```

**要点**：
- 用 `<!-- ROUTING_TABLE_START -->` / `<!-- ROUTING_TABLE_END -->` HTML 注释标记包裹，
  便于程序化定位，不影响 Markdown 渲染
- 「擅长领域」列默认留空，由用户在小队详情页的 Instructions Tab 手动填写
- Leader（Agent Manager 自身）不出现在路由表中——它是路由者，不是被分配者
- 如果创建时没有附加成员（只有 Leader），仍生成空表头 + 分配规则，后续加成员时追加行

#### 4.4.2 新增成员追加（运行时）

在小队详情页通过 Members Tab 添加新成员后，前端检测 `squad.instructions`
中是否存在路由表标记：

- **存在路由表**：在 `### 分配规则` 标题前追加一行。
  新成员始终追加在末尾（不重新排序，保持用户可能已调整的顺序）
- **不存在路由表**：不做任何操作

```
现有路由表:
| Alice | agent | 文档撰写 | 文档、翻译 |
| Bob   | agent | 开发     | 代码编写   |

新增成员 Dave 后:
| Alice | agent  | 文档撰写 | 文档、翻译 |
| Bob   | agent  | 开发     | 代码编写   |
| Dave  | agent  |          |            |  ← 追加在末尾，擅长领域留空
```

#### 4.4.3 移除成员处理

成员被移除后，**自动删除**路由表中对应的行：
- 人不在小队了，Leader 无法分配任务给他——留着是噪音
- 用户填写的擅长领域信息跟着人走，人走了信息也失效
- 直接删除比「留行 + 警告」更简洁，少一个 stale 检测 + 警告 UI 的实现成本

删除通过成员名匹配表格行（`| Dave |` 开头的行），匹配后整行移除并 PATCH instructions。
如果匹配不到（用户改过名字等），静默跳过。

#### 4.4.4 归档 Agent 的 Leader 保护

当用户在智能体页面归档（删除）一个 Agent 时，如果该 Agent 正在担任某个小队的 Leader，
**前端弹出警告 Dialog，阻止归档操作**：

```
⚠ 该智能体正在担任以下小队的 Leader：

  • 研发小队
  • 文档小队

归档后小队将无法正常运转（所有任务不会被认领）。
请先前往小队设置更换 Leader，再归档此智能体。

                                    [取消]  [前往小队设置]
```

**实现方式**：

- 前端在执行归档前，调用 `GET /api/squads`（已有接口）检查该 Agent 是否出现在
  任何小队的 `leader_id` 中
- 如果是 → 弹出警告 Dialog，列出相关小队名称，提供跳转链接
- 如果不是 → 正常执行归档

**不在后端拦截的理由**：
- 归档 API（`PATCH /api/agents/:id`）是通用接口，加 Leader 检查会耦合小队逻辑
- 前端拦截已足够——用户只通过 UI 操作归档，不会绕过前端直接调 API

**修改文件**：

| 文件 | 变更 |
|------|------|
| `packages/views/agents/components/agent-detail-page.tsx`（或归档按钮所在组件） | 归档前检查 Leader 关系，弹出警告 Dialog |

### 4.5 路由规则模板插入

在小队详情页的 SquadInstructionsTab 中增加「插入模板」下拉按钮：

```
SquadInstructionsTab
├── "插入模板" 下拉按钮 ← 新增
│   ├── 成员路由表（基于当前成员动态生成）
│   ├── 按任务类型分配
│   ├── 按优先级处理
│   ├── 升级规则
│   └── 完整模板（路由表 + 以上全部）
├── ContentEditor（已有）
└── Save 按钮（已有）
```

预置的路由规则片段：

```markdown
### 按任务类型分配
- 代码类任务（bug 修复、功能开发、重构）→ 分配给 [编码智能体]
- 文档类任务（文档撰写、翻译、README）→ 分配给 [写作智能体]
- 审查类任务（代码审查、安全审计）→ 分配给 [审查智能体]
- 不确定类型 → 使用 cn-skill-router 技能分析后决定

### 按优先级处理
- P0 紧急：立即分配给最合适的在线成员
- P1 重要：正常分配，附带截止时间说明
- P2 一般：排入队列，空闲时处理

### 升级规则
- 成员回复 BLOCKED → 尝试分配给其他成员或升级给人类
- 超过 3 轮仍未解决 → 升级给人类 reporter
```

### 4.6 Agent Manager Instructions 设计

模板的 instructions 与现有 `squadOperatingProtocol`（squad_briefing.go）互补不重复。
squad briefing 已覆盖协调角色、@mention 委派、活动记录等，模板 instructions 聚焦于
**分析决策能力**和**技能使用指南**：

```markdown
你是一个智能体管理员（Agent Manager），专注于任务分析、路由决策和结果汇总。

## 核心职责

1. **任务分析**：收到新任务时，分析任务类型、复杂度和所需技能。
2. **路由决策**：根据任务特征和成员能力，选择最合适的执行者。
3. **结果汇总**：当成员完成工作后，审查结果质量并向 reporter 汇报。

## 决策框架

分析任务时，按以下维度评估：

- **任务类型**：代码 / 文档 / 研究 / 审查 / 混合
- **复杂度**：简单（单文件改动）/ 中等（多文件协同）/ 复杂（架构级）
- **紧急度**：根据标签、标题关键词和 reporter 的说明判断
- **所需技能**：使用 cn-skill-router 分析任务需要哪些技能

## 技能使用指南

### cn-skill-router（任务路由）
当你不确定该把任务分配给谁时，使用此技能：
- 输入任务描述，获得推荐的技能和智能体
- 遵循路由建议，但可以根据成员当前负载做调整

### multica-cli-operator（CLI 操作）
当你需要查看工作区状态时使用：
- `multica agent list` — 查看可用智能体
- `multica issue list` — 查看当前任务队列
- 创建子 issue 来分解复杂任务

## 结果汇总格式

成员完成工作后，你的汇报应包含：
- **状态**：DONE / DONE_WITH_CONCERNS / BLOCKED
- **变更摘要**：修改了哪些文件，做了什么
- **验证结果**：测试是否通过，是否有遗留问题
- **后续建议**：是否需要进一步工作

## 禁止行为

- 不要自己写代码或修改文件 — 委派给成员
- 不要复述 issue 正文 — 成员已经能看到
- 不要同时委派给多个成员做同一件事
- 不要跳过 `multica squad activity` 记录
```

**Instructions 互补关系**：

```
Agent 自身 Instructions（来自模板，创建时写入）
├── 任务分析框架
├── 技能使用指南（cn-skill-router / multica-cli-operator）
├── 结果汇总格式
└── 禁止行为

+（运行时追加，来自 squad_briefing.go）
├── Squad Operating Protocol（协调协议）
├── Squad Roster（成员花名册 + @mention 语法）
└── Squad Instructions（用户自定义路由规则 + 路由表）
```

模板 instructions 负责「做什么」和「怎么做」，
squad briefing 负责「和谁做」和「按什么规则分配」。

### 4.7 技能包发布策略

Agent Manager 模板引用的两个技能需要可公开访问。
**推荐将技能内嵌到 rimedeck 仓库**（`bundled://` 协议）：

1. 在 `server/internal/agenttmpl/` 下新增 `bundled_skills/` 目录
2. 复制 `cn-skill-router/SKILL.md` 和 `multica-cli-operator/SKILL.md` 到该目录
3. 扩展 `createAgentFromTemplate` handler：当 `source_url` 以 `bundled://` 开头时，
   从 embed.FS 读取而非 HTTP fetch

理由：不依赖外部 repo 可访问性，技能内容可跟 rimedeck 版本一起演进。

---

## 5. 数据流

### 5.1 创建小队（选择「创建 Agent Manager」）

```
用户打开「新建小队」Modal
  ↓
Step 1：选择 ○ 创建 Agent Manager
  ↓ 填写名称、选择 runtime、选择 model
  ↓ 点击「下一步」

Step 2：填写小队信息
  ↓ 名称、描述、头像、附加成员
  ↓ 点击「创建小队」

前端按顺序执行：
  1. POST /api/agents/from-template
     { template_slug: "agent-manager", name: "队长",
       runtime_id: "xxx", model: "claude-sonnet-4-6" }
     → 后端创建 Agent + 导入 cn-skill-router, multica-cli-operator
     → 返回 { agent, imported_skill_ids, reused_skill_ids }

  2. POST /api/squads
     { name: "研发小队", leader_id: agent.id }
     → 后端自动将 leader 加为成员（role: "leader"）

  3. POST /api/squads/:id/members ×N（附加成员，并行）

  4. 获取小队成员列表，按字母排序生成路由表
     PATCH /api/squads/:id { instructions: "<!-- ROUTING_TABLE_START -->..." }

  ↓
跳转到小队详情页
```

### 5.2 创建小队（选择已有 Agent）

```
Step 1：选择 ○ 已有智能体 → 选中 Atlas
  ↓ 点击「下一步」

Step 2：填写小队信息 → 点击「创建小队」
  ↓
前端执行：
  1. POST /api/squads { name: "...", leader_id: atlas.id }
  2. POST /api/squads/:id/members ×N
  3. 不生成路由表（Leader 不是 Agent Manager 模板创建的）
  ↓
跳转到小队详情页
```

### 5.3 新增成员时路由表追加

```
用户在 Members Tab 添加新成员
  ↓
POST /api/squads/:id/members → 成功
  ↓
前端检测：squad.instructions 是否包含 ROUTING_TABLE_START
  ├─ 有 + 新增 → 在 "### 分配规则" 前追加新行 → PATCH instructions
  ├─ 有 + 移除 → 匹配成员名删除对应行 → PATCH instructions
  └─ 无 → 不操作
```

### 5.4 小队路由编辑

```
用户打开小队详情页 → Instructions Tab
  ↓
点击「插入模板」→ 选择模板片段 → 插入到编辑器
  ↓
用户编辑路由规则（填写擅长领域等）→ Save
  ↓
PATCH /api/squads/:id { instructions: "..." }
  ↓
下次 Leader 认领任务时，路由规则注入 Leader 的 system prompt
```

---

## 6. 实施计划

### Phase 1：后端 — Agent Manager 模板（预计 1-2 天）

| # | 任务 | 文件 |
|---|------|------|
| 1.1 | 创建 `agent-manager.json` 模板 | `server/internal/agenttmpl/templates/agent-manager.json` |
| 1.2 | 复制 cn-skill-router / multica-cli-operator SKILL.md | `server/internal/agenttmpl/bundled_skills/` |
| 1.3 | 扩展 `createAgentFromTemplate` 支持 `bundled://` | `server/internal/handler/agent_template.go` |
| 1.4 | 补充单元测试 | `server/internal/agenttmpl/loader_test.go` |

### Phase 2：前端 — 小队创建两步流程（预计 2-3 天）

| # | 任务 | 文件 |
|---|------|------|
| 2.1 | 改造 create-squad Modal 为 Step 1/2 两步 | `packages/views/modals/create-squad.tsx` |
| 2.2 | Step 1 实现：radio group + LeaderPicker + mini 表单 | 同上 |
| 2.3 | 提交流程：按顺序调用 from-template → createSquad → addMembers | 同上 |
| 2.4 | i18n 翻译（zh / en） | `packages/i18n/locales/*/squads.json` |
| 2.5 | 测试 | `packages/views/modals/create-squad.test.tsx` |

### Phase 3：路由表自动管理 + 编辑器 + Leader 保护（预计 2-3 天）

| # | 任务 | 文件 |
|---|------|------|
| 3.1 | 路由表解析/生成/追加/删除工具函数 | `packages/core/squads/routing-table.ts` |
| 3.2 | 创建小队时自动生成路由表 | `packages/views/modals/create-squad.tsx` |
| 3.3 | 新增成员时自动追加路由表行 | `packages/views/squads/components/squad-detail-page.tsx` |
| 3.4 | 移除成员时自动删除路由表行 | `packages/views/squads/components/squad-detail-page.tsx` |
| 3.5 | 定义路由规则模板文本 | `packages/core/squads/routing-templates.ts` |
| 3.6 | SquadInstructionsTab 增加「插入模板」功能 | `packages/views/squads/components/squad-detail-page.tsx` |
| 3.7 | 归档 Agent 时的 Leader 保护检查 + 警告 Dialog | 归档按钮所在组件 |
| 3.8 | i18n 翻译 | `packages/i18n/locales/*/squads.json` |
| 3.9 | 路由表工具函数单元测试 | `packages/core/squads/routing-table.test.ts` |

### Phase 4：端到端验证（预计 1 天）

| # | 任务 |
|---|------|
| 4.1 | 新建小队 → 选「创建 Agent Manager」→ 填表 → 创建成功 → 技能已导入 |
| 4.2 | 新建小队 → 选「已有智能体」→ 创建成功 → 流程与改造前一致 |
| 4.3 | 创建带成员的小队 → 验证路由表按字母序自动生成 |
| 4.4 | 小队详情 → 新增成员 → 验证路由表末尾追加新行 |
| 4.5 | 小队详情 → 移除成员 → 验证路由表对应行自动删除 |
| 4.6 | 归档正在担任 Leader 的 Agent → 验证弹出警告阻止操作 |
| 4.7 | 编辑路由表 → 创建 issue → 验证 Leader briefing 包含路由信息 |
| 4.8 | 验证 Leader 正确使用 cn-skill-router 和 multica-cli-operator 技能 |

---

## 7. 技术细节

### 7.1 模板分类与图标

| 字段 | 值 | 理由 |
|------|-----|------|
| `category` | `"Building"` | 与基础设施/协调相关 |
| `icon` | `"Crown"` | 与小队 Leader 的 Crown 图标一致 |
| `accent` | `"warning"` | 琥珀色，与 Leader 徽章颜色一致（amber-100/amber-700） |

### 7.2 bundled:// 协议实现

```go
// server/internal/handler/agent_template.go

//go:embed bundled_skills/*/SKILL.md
var bundledSkillFS embed.FS

func resolveBundledSkill(sourceURL string) ([]byte, error) {
    slug := strings.TrimPrefix(sourceURL, "bundled://")
    return fs.ReadFile(bundledSkillFS, "bundled_skills/"+slug+"/SKILL.md")
}
```

在 `fetchTemplateSkillsParallel` 中增加 bundled 分支：

```go
if strings.HasPrefix(skill.SourceURL, "bundled://") {
    data, err := resolveBundledSkill(skill.SourceURL)
    // 解析 SKILL.md frontmatter → 创建 skill 记录
} else {
    // 现有 HTTP fetch 逻辑
}
```

### 7.3 两步 Modal 实现

```typescript
// packages/views/modals/create-squad.tsx

type LeaderMode = "existing" | "create-manager";

function CreateSquadModal() {
  const [step, setStep] = useState<1 | 2>(1);
  const [leaderMode, setLeaderMode] = useState<LeaderMode>("existing");

  // "existing" 模式状态
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);

  // "create-manager" 模式状态
  const [managerName, setManagerName] = useState("Agent Manager");
  const [selectedRuntimeId, setSelectedRuntimeId] = useState<string | null>(null);
  const [selectedModel, setSelectedModel] = useState<string | null>(null);

  // Step 2 状态（与现有表单一致）
  const [squadName, setSquadName] = useState("");
  // ...

  const canProceedToStep2 =
    leaderMode === "existing"
      ? !!selectedAgentId
      : !!managerName.trim() && !!selectedRuntimeId;

  // 默认选中模式：有可用 Agent → existing，无 → create-manager
  useEffect(() => {
    if (agents.length === 0) setLeaderMode("create-manager");
  }, [agents]);

  return (
    <Dialog>
      {step === 1 && (
        <StepOne
          leaderMode={leaderMode}
          onModeChange={setLeaderMode}
          /* ...各字段 props... */
        />
      )}
      {step === 2 && (
        <StepTwo /* ...现有小队表单 props... */ />
      )}
      <DialogFooter>
        {step === 1 && (
          <Button onClick={() => setStep(2)} disabled={!canProceedToStep2}>
            下一步
          </Button>
        )}
        {step === 2 && (
          <>
            <Button variant="ghost" onClick={() => setStep(1)}>上一步</Button>
            <Button onClick={handleCreateSquad}>创建小队</Button>
          </>
        )}
      </DialogFooter>
    </Dialog>
  );
}
```

### 7.4 路由表工具函数

```typescript
// packages/core/squads/routing-table.ts

const ROUTING_TABLE_START = "<!-- ROUTING_TABLE_START -->";
const ROUTING_TABLE_END = "<!-- ROUTING_TABLE_END -->";

interface RoutingTableMember {
  name: string;
  memberType: "agent" | "member";
  role: string;
}

/** 检测 instructions 中是否已有路由表 */
export function hasRoutingTable(instructions: string): boolean {
  return instructions.includes(ROUTING_TABLE_START);
}

/** 从成员列表生成默认路由表（按字母排序，Leader 排除在外） */
export function generateRoutingTable(members: RoutingTableMember[]): string {
  const sorted = [...members].sort((a, b) => a.name.localeCompare(b.name));
  const rows = sorted
    .map((m) => `| ${m.name} | ${m.memberType} | ${m.role} |  |`)
    .join("\n");

  return [
    ROUTING_TABLE_START,
    "## 成员路由表",
    "",
    "| 成员 | 类型 | 角色 | 擅长领域 |",
    "|------|------|------|----------|",
    rows,
    "",
    "### 分配规则",
    "- 根据任务类型匹配成员的「擅长领域」列",
    "- 领域为空的成员视为通用，可接收任何任务",
    "- 不确定时使用 cn-skill-router 技能分析后决定",
    ROUTING_TABLE_END,
  ].join("\n");
}

/** 在已有路由表末尾追加一行新成员（不重新排序） */
export function appendMemberToRoutingTable(
  instructions: string,
  member: RoutingTableMember,
): string {
  const newRow = `| ${member.name} | ${member.memberType} | ${member.role} |  |`;
  // 在 "### 分配规则" 前插入；匹配失败时 fallback 到 ROUTING_TABLE_END 前
  const patched = instructions.replace(
    /(\n\n### 分配规则)/,
    `\n${newRow}$1`,
  );
  if (patched === instructions) {
    return instructions.replace(ROUTING_TABLE_END, `${newRow}\n${ROUTING_TABLE_END}`);
  }
  return patched;
}

/** 从路由表中删除指定成员所在的行 */
export function removeMemberFromRoutingTable(
  instructions: string,
  memberName: string,
): string {
  // 匹配 "| memberName |" 或 "| memberName (" 开头的整行（名字后可能带描述）
  const escaped = memberName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const rowPattern = new RegExp(`^\\| ${escaped}[^|]*\\|.*$\\n?`, "m");
  return instructions.replace(rowPattern, "");
}
```

### 7.5 路由表触发时机

| 事件 | 触发位置 | 行为 |
|------|----------|------|
| 创建小队（Manager 模式） | `create-squad.tsx` 提交流程 Step 4 | 生成路由表 → PATCH instructions |
| 新增成员 | `squad-detail-page.tsx` addMember 回调 | 如有路由表 → 追加行 → PATCH |
| 移除成员 | `squad-detail-page.tsx` removeMember 回调 | 如有路由表 → 匹配名字删行 → PATCH |
| 手动「插入模板 → 成员路由表」 | SquadInstructionsTab | 如已有路由表 → 跳过；否则生成 |

**并发保护**：路由表追加仅在 Members Tab 操作时触发。
如果 Instructions Tab 有未保存编辑（dirty），弹 toast 提示而非静默覆盖。

---

## 8. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 技能 URL 不可访问（私有 repo） | 从模板创建失败 | 使用 bundled:// 协议内嵌技能 |
| 模板 instructions 与 squad briefing 重叠 | Leader 收到冗余指令 | 明确划分：模板管"能力"，briefing 管"协调"（见 §4.6） |
| Agent Manager 创建失败导致小队创建中断 | 用户流程卡住 | 前端捕获错误，停在 Step 1 并提示重试 |
| 路由表标记被用户误删 | 增删成员时无法定位路由表 | `hasRoutingTable()` 返回 false 时静默跳过；用户可通过「插入模板」重新生成 |
| 路由表格式被用户编辑后解析失败 | 追加/删除位置不对 | 追加用 `### 分配规则` 定位，删除用成员名匹配行；匹配失败时静默跳过 |
| 路由表 PATCH 与用户编辑 instructions 冲突 | 覆盖用户编辑 | 仅 Members Tab 操作时触发；Instructions Tab dirty 时弹 toast 而非写入 |
| 删除成员行匹配错误（同名/改名） | 删错行或删不掉 | 按创建时记录的名字匹配；匹配不到时静默跳过，不影响流程 |
| 已有 Agent 用户多了一步（Step 1 → Step 2） | 体验变重 | 默认选中「已有智能体」且 LeaderPicker 已预选首个 Agent，用户只需点「下一步」 |

---

## 9. 被否决的方案

### B+C：通用模板选择器 + 小队内联创建

在智能体页面增加通用 TemplatePicker，在小队创建 LeaderPicker 中增加 3 层 Dialog 嵌套入口。

**否决原因**：
- 8 个新增文件、6 个修改文件，复杂度远超方案 A
- 入口 C 需要 3 层 Dialog 嵌套（create-squad → TemplatePicker → CreateAgent），UX 不自然
- 通用 TemplatePicker 本身有价值，但当前版本只需要 Agent Manager 一个模板，
  为一个模板建通用基础设施成本过高

### 仅新增模板 JSON（最小 MVP）

只新增 `agent-manager.json`，不做前端改造。

**否决原因**：前端无入口，用户不可见。

### 深度集成 — 可视化路由编辑器

将路由规则建模为结构化 JSON，拖拽式可视化编辑。

**延后原因**：工作量大（2-3 周），squad briefing 当前是纯文本注入。

---

## 10. 后续迭代方向

1. **小队 Instructions 图形化编辑器**：替代当前的 Markdown 直接编辑，提供结构化的
   图形界面来管理路由表和分配规则。路由表部分用可编辑表格组件（行增删、单元格编辑、
   拖拽排序），分配规则部分用表单化的条件-动作编辑器（任务类型 → 分配给谁），
   最终序列化为 Markdown 写入 `squad.instructions`。用户不再需要手动编辑标记文本
2. **通用模板选择器**：将 TemplatePicker 作为独立 feature 开发，让智能体列表页的
   「创建智能体」也能选择模板（当前 25 个模板都能用起来）
3. **可视化路由编辑器（进阶）**：将路由规则建模为结构化 JSON 存储（独立字段，
   不再依赖 instructions 文本解析），后端 briefing 机制直接消费结构化数据
4. **Manager 自学习**：Manager 根据历史分派记录自动优化路由规则
5. **负载均衡**：Manager 感知成员当前任务量，自动均衡分配
6. **模板市场**：允许用户发布和分享自定义模板（社区模板）
