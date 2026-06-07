# Rimedeck 去云化方案：添加电脑 & 邀请成员

## Context

Rimedeck 移除了 multica 的 cloud 功能，Desktop app 内嵌 server 实例。需要重新设计：
1. **添加电脑**：给工作区添加远程算力节点（daemon/runtime）
2. **邀请成员**：给工作区添加协作者，以邀请码为主的无邮件方式
3. **认证方式**：简化为 IP/Tailscale 域名 + 首次随机认证码（替代邮箱验证码）

---

## 〇、架构分析：两种远程协作方式的本质区别

### Multica 路由层的权限分界（`server/cmd/server/router.go`）

Multica 中「添加电脑」和「邀请成员」是**正交的、独立的**两个流程，对应完全不同的认证体系和 API 范围：

#### 添加电脑（Runtime） — 纯算力，无 UI 访问

路由：`/api/daemon/*`，认证：`middleware.DaemonAuth`（daemon token `mdt_` 或 PAT）

```
/api/daemon/register                          — 注册 runtime
/api/daemon/deregister                        — 注销 runtime
/api/daemon/heartbeat                         — 心跳
/api/daemon/ws                                — WebSocket 通信
/api/daemon/runtimes/{runtimeId}/tasks/claim  — 领取任务
/api/daemon/tasks/{taskId}/*                  — 任务生命周期（start/complete/fail/usage）
```

- 只创建 `agent_runtime` 行，**不创建 `member` 行**
- Daemon token 是 workspace-scoped，**不能**访问 `/api/workspaces/*/issues`、`/api/agents` 等
- 远端机器是无头（headless）计算节点，不提供工作区 UI

#### 邀请成员（Member） — 完整 UI 访问，无算力

路由：`/api/*`（Protected routes），认证：`middleware.Auth`（JWT/session）

```
/api/workspaces/{id}/*     — 工作区管理
/api/issues/*              — 完整 issue CRUD
/api/agents/*              — 完整 agent CRUD
/api/runtimes/*            — 查看/管理 runtime
/api/chat/*                — 对话
/api/inbox/*               — 收件箱
/api/dashboard/*           — 数据看板
...全部工作区功能
```

- 创建 `member` 行（用户加入工作区）
- 通过 JWT/session 认证（用户有自己的账号）
- **可以**操作工作区的所有内容
- **不**自动创建 runtime — 成员需要另外在本机跑 daemon 才贡献算力

#### 对比总结

| | 添加电脑（Runtime） | 邀请成员（Member） |
|---|---|---|
| 创建的数据行 | `agent_runtime` | `member` + `user` |
| 认证方式 | daemon token (`mdt_`) | JWT / session |
| API 范围 | 仅 `/api/daemon/*` | 全部 `/api/*` |
| 身份 | 机器身份 | 人的身份 |
| 看到工作区 UI | 否（headless） | 是（完整 UI） |
| 贡献算力 | 是（执行 agent 任务） | 否（需另外添加电脑） |
| 典型场景 | 加一台 GPU 服务器跑任务 | 邀请同事一起管理 issue |

#### 完整远程协作 = 两步

一个远程协作者需要同时完成两个流程：
1. **邀请成员**：被邀请为 member → 获得工作区 UI 访问权（issue、agent、settings 等）
2. **添加电脑**：在自己机器上跑 daemon → 给工作区贡献算力（可选）

### Multica 原版架构（Cloud 模式）

```
                  Multica Cloud Server（api.multica.ai）
                  ┌──────────────────────────────────────┐
                  │  PostgreSQL（所有数据）                 │
                  │  Workspace / Issue / Agent / Member    │
                  │  Runtime / Task Queue                 │
                  └──────────┬──────────────┬─────────────┘
                             │              │
                 ┌───────────┘              └───────────┐
                 │                                      │
    机器 A（Desktop）                       机器 B（Desktop）
    ┌──────────────────┐                 ┌──────────────────┐
    │ Electron 前端     │←── Auth API ─→ │ Electron 前端     │
    │ (JWT/session)     │                │ (JWT/session)     │
    │ member: 看工作区   │                │ member: 看工作区   │
    ├──────────────────┤                 ├──────────────────┤
    │ Daemon (runtime)  │←── Daemon API→ │ Daemon (runtime)  │
    │ (mdt_ token)      │                │ (mdt_ token)      │
    │ runtime: 跑任务   │                │ runtime: 跑任务    │
    └──────────────────┘                 └──────────────────┘
```

每台 Desktop 上同时运行两个角色：
- **前端（member 身份）**：JWT 认证 → 操作工作区全部内容
- **Daemon（runtime 身份）**：daemon token 认证 → 领取/执行任务

### Rimedeck 当前架构（去云、本地独立）

Desktop 内嵌 server + PostgreSQL（启动链：`local-backend/index.ts` → PG → migration → API server），每台机器是独立的全栈节点，互不连通。

```
    机器 A（Desktop）                       机器 B（Desktop）
    ┌──────────────────┐                 ┌──────────────────┐
    │ Electron 前端     │                 │ Electron 前端     │
    │ (连 127.0.0.1)    │                 │ (连 127.0.0.1)    │
    ├──────────────────┤                 ├──────────────────┤
    │ 内嵌 Server       │                 │ 内嵌 Server       │
    │ + PostgreSQL      │                 │ + PostgreSQL      │
    ├──────────────────┤                 ├──────────────────┤
    │ Daemon (runtime)  │                 │ Daemon (runtime)  │
    └──────────────────┘                 └──────────────────┘
           完全独立，互不连通
```

关键限制：`backend-manager.ts:32` 硬编码 `http://127.0.0.1:{port}`，server 仅本机可访问。

### 目标架构（连接后）

两种连接方式独立工作，可以组合使用：

```
    机器 A（Server 角色）
    ┌──────────────────────────────────────────────────────┐
    │ 内嵌 Server + PostgreSQL                              │
    │ 工作区数据全在这里                                      │
    └────┬─────────────┬─────────────────┬────────────────┘
         │             │                 │
    ① 本机前端    ② 远程 Runtime     ③ 远程 Member + Runtime
    (JWT)         (daemon token)      (JWT + daemon token)
         │             │                 │
    本机 Desktop   机器 C（纯算力）    机器 B（完整协作）
    ┌──────────┐  ┌──────────┐     ┌──────────────────┐
    │ 前端 + UI │  │ 只跑 daemon│     │ 前端连 A 的 API   │ ← 邀请成员
    │ + Daemon  │  │ 无 UI     │     │ (JWT, 操作工作区)  │
    └──────────┘  └──────────┘     ├──────────────────┤
                   ↑                │ Daemon 连 A       │ ← 添加电脑
                   添加电脑          │ (mdt_, 跑任务)    │
                   (纯算力)         └──────────────────┘
```

- **② 只添加电脑**：机器 C 只跑 daemon，贡献算力，无人操作
- **③ 邀请成员 + 添加电脑**：机器 B 的用户既能操作工作区 UI，也贡献算力
- **③ 只邀请成员**：机器 B 的用户能操作工作区 UI，但不跑 daemon（不贡献算力）

---

## 一、服务器地址展示（两个流程的共用基础）

「添加电脑」和「邀请成员」都需要让对方知道本机 server 的 IP 和端口。需要在 UI 上统一展示。

### 当前状况

- 端口：在 `~/.rimedeck/config.json` 中持久化（`backendPort`，默认 `18080`，端口冲突时动态分配）
- IP：`backend-manager.ts` 硬编码 `127.0.0.1`，无局域网 IP 检测
- `MULTICA_PUBLIC_URL` 环境变量也设为 `http://127.0.0.1:{port}`

### 方案

#### 1. Server 端：新增 `GET /api/server-info` 端点

返回本机可用的网络地址列表：

```json
{
  "port": 18080,
  "addresses": [
    { "ip": "192.168.1.100", "interface": "en0", "type": "lan" },
    { "ip": "100.64.0.3",    "interface": "utun3", "type": "tailscale",
      "domain": "my-macbook.tailnet.ts.net" }
  ],
  "hostname": "my-macbook.local"
}
```

**地址检测逻辑（Go 侧）**：

1. **枚举网络接口**（`net.Interfaces()` + `Addrs()`）
   - 过滤 `127.0.0.1`、`::1`、link-local（`169.254.*`、`fe80::*`）
   - 标记类型：普通接口 → `lan`，tun/utun 接口 → `vpn`
2. **识别 Tailscale 地址**
   - IP 在 CGNAT 范围 `100.64.0.0/10` 内 → 标记 `type: "tailscale"`
   - 尝试执行 `tailscale status --json`（best-effort，失败不报错）
   - 成功时从 JSON 输出中提取 `Self.DNSName`（如 `my-macbook.tailnet.ts.net.`）写入 `domain` 字段
   - CLI 不存在或未登录时，`domain` 为空，前端只展示 IP
3. **公开端点**（无需认证），信息本身不敏感且对方需要知道

#### 2. 前端：共用 `<ServerAddressBar />` 组件

一个可复用的地址展示组件，同时用于「添加电脑」对话框和「邀请成员」面板：

```
┌──────────────────────────────────────────────────────────┐
│ 📍 本机服务器地址                                          │
│                                                          │
│  局域网    192.168.1.100:18080                    [复制]   │
│  Tailscale my-macbook.tailnet.ts.net:18080        [复制]   │
│                                                          │
│  同一局域网内使用「局域网」地址                               │
│  跨网络（不同 Wi-Fi / 远程）使用「Tailscale」地址            │
└──────────────────────────────────────────────────────────┘
```

- 调用 `GET /api/server-info` 获取地址列表
- 每个地址带独立「复制」按钮
  - LAN 地址拷贝 `http://<ip>:<port>`
  - Tailscale 地址优先拷贝 `http://<domain>:<port>`（有域名时），否则拷贝 IP
- Tailscale 行仅在检测到 Tailscale 接口时显示，未安装 Tailscale 的用户不会看到
- 多个 LAN IP 时全部列出（用户可能有有线 + 无线）
- 仅一个地址时简化为单行展示

#### 3. 展示位置

| 位置 | 场景 |
|------|------|
| **运行时 → 添加电脑** 对话框 | 在 setup 命令上方显示本机地址 + 认证码 |
| **设置 → 成员 → 邀请成员** 面板 | 在邀请码旁边显示本机地址，方便一并告知 |
| **设置页顶部**（可选） | 常驻展示，随时可查 |

#### 涉及文件

- `server/internal/handler/server_info.go` — 新增端点，Go 侧检测本机网络接口
- `server/cmd/server/router.go` — 注册公开路由
- `packages/views/common/server-address-bar.tsx` — 共用前端组件
- `packages/core/api/client.ts` — 新增 `getServerInfo()` 方法

---

## 二、认证方式改造（基础依赖）

### 现状
- 现有认证：邮箱 + 验证码（Resend API / SMTP / dev stdout）
- 用户注册需要邮箱

### 新方案：网络地址 + 认证码配对

认证码配对用于两个场景：
- **添加电脑**：远端 daemon 首次连接时，用认证码获取 daemon token（`mdt_`）
- **邀请成员**：远端用户首次连接时，用邀请码 + 认证码注册账号并获取 JWT session

**设备认证码流程**（用于 daemon 连接）：
1. Server 端（Desktop 内嵌）启动时生成 **设备认证码**（6位字母数字，如 `K3M9ZP`）
2. 认证码显示在 Server 端的 Desktop UI 上（状态栏 / 弹窗 / 设置页）
3. 远端 daemon 连接 Server 时输入认证码
4. Server 验证认证码 → 颁发 daemon token（`mdt_`）
5. 后续连接使用 token 自动认证

**关键改动**：
- 新增 `POST /api/auth/pair` 端点：接收认证码 → 返回 daemon token
- Server 启动时生成认证码，存内存（或轻量持久化），可刷新
- Desktop UI 新增 "设备认证码" 显示区域（系统托盘或设置页顶部）
- 保留现有 PAT / daemon token 机制作为认证后的凭证载体

**涉及文件**：
- `server/internal/handler/` — 新增 pair 端点
- `server/internal/middleware/daemon_auth.go` — 支持新 token 类型（或复用 `mdt_`）
- `apps/desktop/src/` — UI 显示认证码
- `server/internal/daemon/config.go` — Client 端存储 server 地址和 token

---

## 二、添加电脑（纯算力 Runtime）

> **本质**：给工作区添加一个 headless 算力节点（daemon/runtime），只能执行 agent 任务，不提供工作区 UI 访问。

### 模式 A：本机作为服务器（其他电脑连入提供算力）

**用户操作流程**：
1. 打开"运行时"页 → 点击"添加电脑"
2. 对话框显示：
   - 本机服务器地址（自动检测局域网 IP + 显示 Tailscale 域名如有）
   - 当前设备认证码（从 Server 状态获取）
   - 远端机器的安装/连接指令：
     ```
     multica setup self-host --server-url http://192.168.1.100:8080
     # 首次连接时输入认证码: K3M9ZP
     ```
3. 实时监听 `daemon:register` 事件，远端注册后自动跳转成功页
4. 远端机器出现在运行时列表的"远程"分组中

**改动点**：
- `connect-remote-dialog.tsx`：
  - 从 `/api/config` 获取 `daemon_server_url`（内嵌 server 时应返回自身地址）
  - 显示本机 IP 和认证码
  - 命令改为 `multica setup self-host --server-url <url>`
- `server/internal/handler/config.go`：内嵌模式下返回 `daemon_server_url` = 本机地址
- 新增：本机 IP 检测逻辑（前端或通过 server API 返回）

### 模式 B：本机 daemon 贡献算力给远程工作区（P3+，暂不实施）

> **延后原因**：daemon 同时服务本地和远程 server 需要多 client 架构（当前是单 `ServerBaseURL` + 单 `Client`），且跨 server 的 agent 绑定关系复杂（一个 runtime 同时出现在两个 server 的 agent 列表中，任务调度、并发控制、heartbeat 都需要独立管理）。放到 P3+ 处理。
>
> 当前"连接到服务器"按钮已在 UI 中就位（`connect-to-server-dialog.tsx`），但仅用于 daemon token 配对，尚未实现 daemon 重配 + 多 server 并行。

**未来设计方向**：
```
Daemon
├── localClient  → 127.0.0.1:18080（本机内嵌 server）
│   └── workspace-A → runtimes, tasks...
└── remoteClients[]
    └── client → 192.168.1.50:18080（远程 server）
        └── workspace-B → runtimes, tasks...
```
- `Daemon` 新增 `remoteClients map[string]*Client`
- 每个 remote client 独立的 token、heartbeat、task polling
- `MaxConcurrentTasks` 本地 + 远程共享
- 持久化到 `~/.rimedeck/remote-servers.json`

---

## 三、邀请成员（完整协作 Member）

> **本质**：给工作区添加一个协作者（member），获得完整工作区 UI 访问权限（issue、agent、settings 等）。成员不自动获得算力——需要另外通过"添加电脑"流程贡献 runtime。

### 流程

**Admin 端**（Server 机器上操作）：
1. 设置 → 成员 → "邀请成员"
2. 选择角色（member / admin）
3. 点击"生成邀请码"
4. 显示 **6位邀请码**（如 `XP39KM`）和有效期
5. Admin 口头/截图/消息告知被邀请人

**被邀请人端**（Client 机器上操作）：
1. 打开 Rimedeck Desktop
2. 输入 Server 地址（如 `http://192.168.1.100:8080`）
3. 输入邀请码 → 自动注册账号 + 加入工作区
4. Desktop 前端 API 切换到远程 server → 看到完整的工作区 UI
5. （可选）本机 daemon 也连接远程 server → 贡献算力

**前端 API 切换**（被邀请人 Desktop 的关键改动）：

当前 Desktop 通过 `runtime-config:get` 同步 IPC 返回固定的本机 API 地址：

```typescript
// 当前：固定指向本机内嵌 server
runtimeConfigResult = {
  ok: true,
  config: {
    schemaVersion: 1,
    apiUrl: localBackend.apiUrl,      // http://127.0.0.1:port
    wsUrl: localBackend.wsUrl,        // ws://127.0.0.1:port/ws
    appUrl: localBackend.apiUrl,
  },
};
```

接受邀请后需要切换到远程 server：
1. 新增 IPC `runtime-config:switch`，允许 renderer 将 API 指向远程 server
2. 持久化选择（写入 `~/.rimedeck/remote-server.json`），下次启动自动连接
3. 提供「断开 / 切回本机」操作，恢复到本地 server
4. Client 端内嵌 server 可保持运行（本地数据不丢），也可暂停以节省资源

### 改动点

**Server 端**：
- DB migration：`workspace_invitation` 表新增 `invite_code VARCHAR(8)` 列 + 唯一索引
- `invitation.sql` 新增查询：`GetInvitationByCode`（按 code + status='pending' 查询）
- `invitation.go`：
  - `CreateInvitation()` 修改：生成随机 6 位码（大写字母 + 数字，去歧义字符如 O/0/I/1）
  - 新增 `POST /api/invitations/redeem` 端点：接收 `{ code: "XP39KM" }` → 查找邀请 → 创建用户 + 接受邀请（复用 `AcceptInvitation` 的事务逻辑）
  - 邮箱字段改为可选（`invitee_email` 可为空，code 是主要匹配方式）

**前端 — Admin 端**：
- `members-tab.tsx`：
  - 邀请表单改为：角色选择 + "生成邀请码" 按钮（移除强制邮箱输入）
  - 生成后显示邀请码（大字体 + 复制按钮）
  - 邀请列表中显示邀请码而非邮箱

**前端 — 被邀请人端**：
- 新增 `join-workspace-dialog.tsx`：输入 server 地址 + 邀请码
  - 调用 `POST <remote-server>/api/invitations/redeem` 注册 + 加入工作区
  - 触发 `runtime-config:switch` IPC 切换前端 API 到远程 server
  - 可选：同时配置本机 daemon 连接远程 server（贡献算力）
- Desktop main process：
  - 新增 `runtime-config:switch` IPC：更新 `runtimeConfigResult`，通知 renderer 重连
  - 新增 `runtime-config:disconnect` IPC：恢复到本机 server
  - 持久化远程连接到 `~/.rimedeck/remote-server.json`

**i18n**：
- `locales/en/settings.json` 和 `locales/zh-Hans/settings.json` 添加邀请码相关文案

---

## 四、实现优先级

| 阶段 | 内容 | 复杂度 | 依赖 |
|------|------|--------|------|
| **P0** | Server 监听从 `127.0.0.1` 改为 `0.0.0.0` + `GET /api/server-info` + `<ServerAddressBar />` 组件 | 低 | 无 |
| **P0** | 设备认证码配对（`POST /api/auth/pair`，Desktop 显示认证码） | 中 | 无 |
| **P0** | 添加电脑 — 模式 A（改造 connect-remote-dialog 显示 self-host 命令 + 认证码） | 低 | P0 认证 |
| **P1** | 邀请成员 — 邀请码（DB migration + redeem API + 前端改造） | 中 | 无 |
| **P1** | 前端 API 切换（`runtime-config:switch` IPC + 持久化 + 断开恢复） | 中 | P1 邀请 |
| **P3+** | 添加电脑 — 模式 B（daemon 多 server 支持 + 同时服务本地和远程） | 高 | P0 认证 |

---

## 五、关键文件清单

### 新增文件
- `server/internal/handler/server_info.go` — `GET /api/server-info`，Go 侧检测本机网络接口
- `packages/views/common/server-address-bar.tsx` — 共用的服务器地址展示组件
- `server/migrations/0XX_invitation_invite_code.up.sql` — 邀请码列
- `packages/views/runtimes/components/connect-to-server-dialog.tsx` — daemon 连远程 server UI
- `packages/views/workspace/join-workspace-dialog.tsx` — 输入邀请码 + 切换前端 API

### 修改文件
- `apps/desktop/src/main/local-backend/backend-manager.ts` — server 监听地址 `127.0.0.1` → `0.0.0.0`
- `apps/desktop/src/main/index.ts` — 新增 `runtime-config:switch` / `disconnect` IPC
- `apps/desktop/src/preload/index.ts` — 暴露 runtime-config 切换方法给 renderer
- `server/internal/handler/invitation.go` — 邀请码生成 + redeem 端点
- `server/pkg/db/queries/invitation.sql` — 新查询
- `server/cmd/server/router.go` — 注册新路由
- `packages/views/settings/components/members-tab.tsx` — 邀请码 UI
- `packages/views/runtimes/components/connect-remote-dialog.tsx` — 显示 self-host 命令
- `packages/views/runtimes/components/runtimes-page.tsx` — 添加按钮入口
- `server/internal/handler/config.go` — 内嵌模式返回自身地址
- `packages/core/api/client.ts` — 新增 API 方法

---

## 六、验证方式

1. **认证配对**：启动 Desktop → 查看认证码 → 另一台机器用认证码连接 daemon → 验证 daemon token 颁发和后续免码使用
2. **添加电脑 (A)**：打开"添加电脑" → 确认显示正确的 self-host 命令和认证码 → 远端执行 → 确认 runtime 出现在列表中 → 验证远端只能跑任务、不能看工作区 UI
3. **邀请成员**：Admin 生成邀请码 → 被邀请人输入 server 地址 + 邀请码 → 确认前端切换到远程 server → 确认能看到完整工作区 UI → 确认出现在成员列表
4. **完整协作**：邀请成员后 → 被邀请人同时添加电脑 → 确认既能操作 UI 又贡献算力
5. **断开恢复**：被邀请人点"断开连接" → 确认前端恢复到本机 server → 本地数据完好

---

## 七、Tailscale 集成评估

### 决策：不内嵌 Tailscale，引导用户自行安装（方案 B）

### 评估背景

方案中多处涉及跨机器通信（添加电脑、连接远程服务器），需要评估是否在 Rimedeck 中内嵌 Tailscale 以简化网络层。

### 现状

- 代码库中零 Tailscale 代码，`go.mod` 无 `tailscale.com` 依赖
- Desktop 内嵌 server 绑定 `127.0.0.1:{port}`（`apps/desktop/src/main/local-backend/backend-manager.ts:32`），仅本机可访问
- Daemon 默认连接 `ws://localhost:8080/ws`
- 网络层是纯 HTTP + WebSocket，无 P2P/VPN/NAT 穿透能力

### 内嵌 Tailscale 的潜在好处

| 维度 | 效果 |
|------|------|
| NAT 穿透 | 跨网络（家/公司/咖啡厅）直连，无需公网 IP 或端口映射 |
| 加密 | WireGuard 端到端加密，不依赖 HTTPS 证书 |
| 稳定地址 | `machine.tailnet.ts.net` 域名不随 IP 变，重启/漫游不断连 |
| 认证 | Tailscale 自带设备认证，可替代「认证码配对」机制 |
| 零配置 | 用户不需要手动查 IP、开端口、配防火墙 |

### 不内嵌的理由

| 维度 | 问题 |
|------|------|
| **与去云理念矛盾** | Tailscale NAT 穿透依赖其协调服务器（`controlplane.tailscale.com`），本质上是把「Multica Cloud」依赖换成「Tailscale Cloud」依赖 |
| **自建协调服务成本高** | 可用 Headscale（开源替代），但用户需额外部署一个 Headscale 实例，增加复杂度 |
| **局域网场景不需要** | 主要场景是局域网内多台电脑协作，HTTP 直连 + IP 地址即可满足 |
| **依赖体积大** | `tsnet` 库引入 ~15-20MB 额外二进制（WireGuard + DERP + 控制面客户端） |
| **平台适配复杂** | Windows/macOS/Linux 的 tun 设备权限不同，Electron + Go sidecar 架构下集成 tsnet 需处理提权 |
| **许可证灰色地带** | `tsnet` 是 BSD-3 可用，但 Tailscale 客户端用 `tailscale.com/go/...` 私有模块，间接依赖可能有问题 |

### 采用方案：不内嵌，兼容外部 VPN

Rimedeck 不感知 Tailscale 的存在，但在 UI 和网络层做好兼容：

1. **Server 监听地址改为 `0.0.0.0`**：当前 `backend-manager.ts` 硬编码 `127.0.0.1`，必须改为 `0.0.0.0` 才能被其他机器（包括 Tailscale 网络）访问
2. **UI 接受任意地址输入**："添加电脑"和"连接到远程服务器"中的地址输入框支持 IP、域名、Tailscale 域名（如 `my-pc.tailnet.ts.net`）
3. **文档说明**：在帮助文档中说明支持 Tailscale/ZeroTier 等 VPN 分配的地址，引导跨公网用户自行安装
4. **认证仍用认证码配对**：不依赖 Tailscale 的设备认证，保持方案独立性

### 后续可能的演进（P3+）

如果未来有强烈的跨公网需求且用户不愿自装 VPN，可考虑：
- 内嵌轻量 relay（如 libp2p hole-punch），仅做 NAT 穿透，不引入完整 VPN 栈
- 提供可选的 Tailscale 插件模式（用户自行启用），而非默认内嵌
