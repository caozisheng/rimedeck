# Rimedeck 运行时扩展评估：新增 AI Coding CLI

## Context

Rimedeck（基于 Multica）的运行时系统已支持 13 个 provider：
`claude`, `codex`, `copilot`, `opencode`, `openclaw`, `hermes`, `gemini`, `pi`, `omp`, `cursor`, `kimi`, `kiro`, `antigravity`

本文评估新增以下 2 个 CLI 运行时的可行性、工作量和实施方案，同时评估已接入的 Kimi CLI 的维护状态：

| # | 名称 | 厂商 | 定位 |
|---|------|------|------|
| 1 | **Qoder CLI** | Qodo (formerly Codium) | 通用 coding agent，ACP 0.23 |
| 2 | **Qwen Code** | 阿里巴巴 (通义千问) | Qwen 模型原生 coding agent |
| 3 | **Kimi CLI** | MoonshotAI (月之暗面) | 已接入，评估是否需要更新/增强 |

---

## 一、现有运行时架构概述

### 添加一个新 provider 的完整改动清单

基于对 codebase 的分析，新增一个 runtime provider 需要修改以下位置：

#### 后端（Go）

| 文件 | 改动 | 说明 |
|------|------|------|
| `server/pkg/agent/<provider>.go` | **新建** | Backend 实现：Execute()，事件解析，进程管理 |
| `server/pkg/agent/agent.go` | 修改 | `New()` switch-case 加分支；`launchHeaders` 加条目 |
| `server/pkg/agent/models.go` | 修改 | `ListModels()` 加分支；静态/动态模型发现 |
| `server/internal/daemon/types.go` | 无需修改 | `AgentEntry` 和 `Runtime` 通用 |
| `server/internal/daemon/daemon.go` | 修改 | `defaultArgsForProvider()` 加分支（如需） |
| `server/internal/daemon/config.go` | 修改 | `Config.Agents` map 注释更新 |
| `server/internal/daemon/execenv/runtime_config.go` | 修改 | `runtimeConfigPath()` switch-case 加新 provider |

#### 前端（TypeScript）

| 文件 | 改动 | 说明 |
|------|------|------|
| `packages/core/agents/mcp-support.ts` | 修改 | `MCP_SUPPORTED_PROVIDERS` set 加条目（如支持 MCP） |
| `packages/core/runtimes/cli-version.ts` | 可能修改 | CLI 版本检测逻辑（如支持 quick-create） |

#### 通信协议分类

目前已有 provider 使用三种通信模式：

| 模式 | Provider | 特点 |
|------|----------|------|
| **stream-json (stdout)** | claude, gemini, cursor | CLI 以 JSON 行流式输出到 stdout |
| **ACP (stdin/stdout JSON-RPC)** | hermes, kimi, kiro, copilot | Agent Client Protocol 双向通信 |
| **JSON output (stdout)** | codex, opencode, openclaw, pi, omp | CLI 运行完毕后 stdout 输出结构化 JSON |
| **plain text (stdout)** | antigravity | 纯文本输出 + 日志文件解析 session ID |

新 provider 最理想是支持 ACP 协议（可复用 `hermesClient`），次优是 stream-json。

---

## 二、逐项评估

### 2.1 Qoder CLI

**厂商**：Qodo（原 CodiumAI → 品牌重塑为 Qoder）
**仓库**：`https://github.com/qoder-official/qoder-mcp`
**CLI 二进制名**：`qoder`
**安装方式**：CLI 安装器（详见 docs.qoder.com）
**成熟度**：生产就绪，已有 Zed / JetBrains / VS Code 集成

#### 协议与接口

- **支持 ACP 0.23 协议**：`qoder acp` 子命令 → 可直接复用 `hermesClient`
- 深度 MCP 集成：可同时作为 MCP client 和 server 运行
- 定位已从纯 code review 扩展为**通用 coding agent**

#### 模型支持

- 通过 settings 配置，支持多种 provider
- 模型发现可通过 ACP `session/new` 的 `availableModels` 获取

#### 工作量评估

| 场景 | 工作量 | 备注 |
|------|--------|------|
| ACP 0.23 模式 | **2-3 天** | 复用 hermesClient，仅需 args + tool name mapping |

#### 优先级建议：**P1 — 推荐优先接入**

- ACP 0.23 支持 → 工作量最小（参考 kimi.go 模板）
- MCP 深度集成 → `MCP_SUPPORTED_PROVIDERS` 可直接加入
- 已从 code review 工具进化为通用 agent，与 Rimedeck task 模式匹配
- IDE 集成成熟度高，用户基数可观

---

### 2.2 Kimi Code CLI（已接入，增强评估）

**厂商**：MoonshotAI（月之暗面）
**仓库**：`https://github.com/MoonshotAI/kimi-code`（品牌升级：kimi-cli → kimi-code）
**CLI 二进制名**：`kimi`（原），可能已更名为 `kimi-code`
**安装方式**：脚本安装（无需 Node.js），支持 Python 3.12-3.14 + uv 包管理
**默认模型**：Kimi K2 系列

#### 现有集成状态

Kimi CLI **已完整接入**，代码位于：
- `server/pkg/agent/kimi.go` — Backend 实现（ACP 协议，复用 hermesClient）
- `server/pkg/agent/models.go` — 动态模型发现 `discoverKimiModels()`
- `server/internal/daemon/execenv/runtime_config.go` — 写入 `AGENTS.md`
- `packages/core/agents/mcp-support.ts` — MCP 支持已启用

功能覆盖：
- [x] ACP 通信协议
- [x] 动态模型发现
- [x] MCP server 支持
- [x] Session resume
- [x] 模型切换 (`session/set_model`)
- [x] 工具名映射 (`kimiToolNameFromTitle`)
- [x] Provider error 嗅探
- [x] Custom args 支持

#### 可能的增强项

1. **二进制名更新**：仓库已从 `kimi-cli` 迁移到 `kimi-code`，需确认 CLI 二进制名是否从 `kimi` 变更为 `kimi-code`。如变更，需更新 `kimi.go` 中 `execPath` 默认值。
2. **thinking level 支持**：当前 `ExecOptions.ThinkingLevel` 未被 kimi 后端消费，如果 Kimi K2 支持 reasoning effort 控制，可增加
3. **版本检测**：`checkQuickCreateCliVersion` 可能需要适配 Kimi Code CLI 的版本格式
4. **Sidecar manifest**：如 Kimi Code CLI 支持 sidecar 模式可增加
5. **安装方式变更**：新版不依赖 Node.js，改为脚本安装 + Python/uv，文档需更新

#### 优先级建议：**维护性 — 需确认二进制名兼容性**

- 已完整集成，核心功能无需新增
- **紧急确认**：`kimi-code` 品牌升级后二进制名是否变化，如变化需尽快适配

---

### 2.3 Qwen Code

**厂商**：阿里巴巴（Alibaba Cloud）
**仓库**：`https://github.com/QwenLM/qwen-code`
**CLI 二进制名**：`qwen-code`
**安装方式**：包管理器安装（详见阿里云文档）
**默认模型**：Qwen3-Coder-Plus
**认证方式**：API Key（OAuth 已于 2026 年 4 月停用）
**成熟度**：生产就绪

#### 协议与接口

- 支持多 provider：OpenAI、Anthropic、Gemini API、阿里云 DashScope、OpenRouter
- **协议格式待确认**：暂无 ACP 支持的明确证据
- 暂无 MCP 支持

#### 模型支持

- Qwen3-Coder-Plus（默认），Qwen3-Coder, Qwen-Max, Qwen-Plus 等
- 通过 API Key 访问，多 provider 路由

#### 替代路径

Qwen 模型可以通过 opencode 配置 DashScope API endpoint 使用，但 `qwen-code` 作为独立 CLI 有自己的 agent 逻辑和工具系统，并非简单的 API wrapper。

#### 工作量评估

| 场景 | 工作量 | 备注 |
|------|--------|------|
| 有 JSON 流输出 | **4-5 天** | 新写事件解析器 |
| 有 ACP 支持 | **2-3 天** | 复用 hermesClient |
| 仅文本输出 | **5-7 天** | 需封装 + session 管理 |

#### 优先级建议：**P2 — 推荐接入**

- 阿里官方出品，有长期维护保障
- Qwen3-Coder 系列在中文编程场景表现优秀
- 对国内用户有较高的实用价值（DashScope API 延迟低、价格友好）
- 需要先调研其 stdout 输出格式确定最佳接入方式

---

## 三、优先级汇总与建议

### 总览表

| Provider | CLI 可用性 | 协议 | 工作量 | 用户需求 | 优先级 |
|----------|-----------|------|--------|----------|--------|
| **Qoder CLI** | ✅ 生产就绪 | **ACP 0.23** | **2-3 天** | 中-高 | **P1** |
| **Qwen Code** | ✅ 生产就绪 | 待确认 | 4-5 天 | 中（国内用户） | **P2** |
| **Kimi Code** | ✅ 已接入 | ACP | 0（维护） | 已满足 | **维护** |

### 建议行动

1. **立即（本周）**：
   - 确认 Kimi Code CLI 品牌升级后二进制名是否从 `kimi` 变更为 `kimi-code`，如变更需适配
   
2. **短期（1-2 周内）**：
   - **接入 Qoder CLI** — ACP 0.23 完整支持，工作量最小，参照 `kimi.go` 模板即可

3. **中期（1-2 月内）**：
   - **接入 Qwen Code** — 调研其 stdout 输出格式后实施

### 接入 Qoder CLI 的具体步骤（P1）

由于 Qoder CLI 支持 ACP 0.23，接入路径清晰，参照 `kimi.go`：

```
1. 新建 server/pkg/agent/qoder.go
   - type qoderBackend struct { cfg Config }
   - Execute() 走 ACP 协议：`qoder acp`
   - 复用 hermesClient 传输层
   - qoderToolNameFromTitle() 工具名映射
   - qoderBlockedArgs 安全参数过滤

2. 修改 server/pkg/agent/agent.go
   - New() 加 case "qoder" → &qoderBackend{cfg: cfg}
   - launchHeaders["qoder"] = "qoder acp"

3. 修改 server/pkg/agent/models.go
   - ListModels() 加 case "qoder" → discoverACPModels() 复用
   - ModelSelectionSupported("qoder") → true

4. 修改 server/internal/daemon/execenv/runtime_config.go
   - runtimeConfigPath() 加 case "qoder" → AGENTS.md

5. 修改 server/internal/daemon/config.go
   - Agents map 注释加 "qoder"

6. 修改 packages/core/agents/mcp-support.ts
   - MCP_SUPPORTED_PROVIDERS 加 "qoder"（Qoder 有深度 MCP 支持）

7. 测试
   - 新建 server/pkg/agent/qoder_test.go
```

### 关于 opencode 路径的补充说明

`opencode` 运行时本身就是一个**多模型路由器**，支持任意 OpenAI-compatible API endpoint。这意味着：

- DeepSeek API → `opencode run --model deepseek/deepseek-chat`
- Qwen/DashScope API → `opencode run --model qwen/qwen-max`（需配置 endpoint）
- 任何兼容 OpenAI 格式的 API → 均可通过 opencode 使用

因此，对于**纯模型接入需求**（用户只想用 DeepSeek/Qwen 模型，不关心 CLI 本身），不需要新 provider，只需要：
1. 确保 opencode 的模型发现正确展示这些模型
2. 在 UI 引导中说明如何配置自定义 API endpoint

---

## 四、技术实现参考

如果未来确定要接入新 provider，以下是实现模板：

### ACP 模式（推荐）— 参考 kimi.go

```
新建 server/pkg/agent/<provider>.go:
  - type <provider>Backend struct { cfg Config }
  - func (b *<provider>Backend) Execute(...) 复制 kimi.go 结构
  - 修改 binary name、blocked args、tool name mapping
  
修改 server/pkg/agent/agent.go:
  - New() 加 case "<provider>"
  - launchHeaders 加条目

修改 server/pkg/agent/models.go:
  - ListModels() 加 case (discoverACPModels 复用)

修改 server/internal/daemon/execenv/runtime_config.go:
  - runtimeConfigPath() 加 case (多数 ACP 用 AGENTS.md)

修改 server/internal/daemon/config.go:
  - Agents map 注释

可选：修改 packages/core/agents/mcp-support.ts:
  - 如支持 MCP 加入 MCP_SUPPORTED_PROVIDERS
```

### Stream-JSON 模式 — 参考 claude.go / gemini.go

```
事件解析需要自写，关注：
  - stdout JSON line 格式
  - 工具调用/结果的结构化输出
  - token usage 统计
  - session ID 提取
  - 错误信号和退出码语义
```
