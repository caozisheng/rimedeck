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
| 认证方式 | daemon token (`mdt_`) — pairing code 配对 | JWT — invite code 赎回 |
| API 范围 | 仅 `/api/daemon/*` | 全部 `/api/*` |
| 身份 | 机器身份（无用户） | 人的身份 |
| 看到工作区 UI | 否（headless） | 是（完整 UI） |
| 贡献算力 | 是（执行 agent 任务） | 否（需另外添加电脑） |
| 典型场景 | 加一台 GPU 服务器跑任务 | 邀请同事一起管理 issue |
| 凭据独立性 | daemon token 独立于 JWT | JWT 独立于 daemon token |

**两种凭据完全独立**：pairing code 颁发的 daemon token 只用于 daemon 认证（心跳、任务领取），与用户的 JWT（工作区 UI 访问）无关。一台机器可以只共享算力（无用户身份），也可以只加入工作区（不共享算力），或者两者兼有。

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

关键限制：~~`backend-manager.ts:32` 硬编码 `http://127.0.0.1:{port}`，server 仅本机可访问。~~ （已改为 `0.0.0.0`，支持局域网访问）

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

### 当前状况（已实现）

- 端口：在 `~/.rimedeck/config.json` 中持久化（`backendPort`，默认 `18080`，端口冲突时动态分配）
- IP：server 监听 `0.0.0.0`，支持局域网访问
- `GET /api/server-info` 端点已实现，返回本机网络地址 + pairing code
- `<ServerAddressBar />` 组件已实现，展示在 ConnectRemoteDialog 中

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

### 模式 B：本机 daemon 贡献算力给远程工作区（已实现 — 热添加）

通过 daemon 的 `/remote/add` health 端点热添加远端 server 连接，**不重启 daemon 进程**。本机 runtime 心跳不中断。

**实现方式**：
- `ConnectToServerDialog`：输入 server 地址 + pairing code → `POST /api/auth/pair` → 获取 `mdt_*` token
- `daemonAPI.addRemoteServer(url, token)` → daemon health 端口 `POST /remote/add` → 创建独立 Client + 注册 runtime + 启动心跳
- 停止共享：`daemonAPI.removeRemoteServer(url)` → daemon `POST /remote/remove` → deregister + 停止心跳

**Go daemon 架构**（`server/internal/daemon/remote.go`）：
```
Daemon
├── primaryClient → 127.0.0.1:18080（本机，不变）
│   └── workspaces → runtimes, heartbeats, tasks...
└── remoteServers map[string]*remoteServer
    └── remoteServer{client, runtimeIDs, cancel}
        ├── client → 192.168.1.50:18080（远端）
        └── per-runtime heartbeat goroutines（独立 context）
```

**多 server 并行**已实现：daemon 可同时连接本机 + 多个远端 server。

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
| ~~P3+~~ ✅ | 添加电脑 — 模式 B（daemon 热添加远端 server，多 server 并行） | 中 | P0 认证 |

---

## 五、关键文件清单

### 新增文件
- `server/internal/handler/server_info.go` — `GET /api/server-info`，Go 侧检测本机网络接口 + pairing code
- `server/internal/handler/device_pair.go` — `POST /api/auth/pair`，设备认证码配对（单次使用 + 限流 + 事件广播）
- `server/internal/daemon/remote.go` — daemon 热添加远端 server：`AddRemoteServer` / `RemoveRemoteServer` / `runRemoteHeartbeat`
- `packages/views/common/server-address-bar.tsx` — 共用的服务器地址展示组件
- `packages/views/runtimes/components/connect-to-server-dialog.tsx` — daemon 连远程 server UI
- `packages/views/workspace/join-workspace-dialog.tsx` — 邀请码 + 历史列表 + 快速重连
- `apps/desktop/src/renderer/src/pages/remote-reconnect.tsx` — 远端连接失败时的重连页

### 修改文件
- `apps/desktop/src/main/local-backend/backend-manager.ts` — server 监听 `0.0.0.0` + Windows 防火墙规则
- `apps/desktop/src/main/index.ts` — `runtime-config:switch/disconnect` IPC + `remote_connection.json` + `remote_servers.json` 历史
- `apps/desktop/src/main/daemon-manager.ts` — `addRemoteServer` / `removeRemoteServer`（通过 daemon health 端口）
- `apps/desktop/src/preload/index.ts` — 暴露所有 IPC（runtime-config、daemon remote、history）
- `apps/desktop/src/renderer/src/App.tsx` — auto-login guard（`isRemote`）
- `server/internal/daemon/daemon.go` — `remoteServers` 字段 + `deregisterAllRemotes`
- `server/internal/daemon/health.go` — `POST /remote/add` + `POST /remote/remove` 端点
- `server/internal/handler/invitation.go` — 邀请码 + redeem + JWT 签发 + 已有 member 处理
- `server/internal/handler/daemon.go` — daemon token runtime 自动 public + deregister 立即删除
- `packages/core/platform/auth-initializer.tsx` — 401 时清 token，网络错误保留
- `packages/views/runtimes/components/connect-remote-dialog.tsx` — WS 事件 + 轮询检测
- `packages/views/layout/app-sidebar.tsx` — 工作区列表（本地 + 远端）+ 退出 + 断开
- `packages/core/api/client.ts` — `redeemInvitation` 含 `auth_token`

---

## 六、验证方式

1. **认证配对**：启动 Desktop → 查看认证码 → 另一台机器用认证码连接 daemon → 验证 daemon token 颁发 → 认证码已更新（单次使用）
2. **添加电脑**：打开"添加电脑" → 远端"连接到服务器" → daemon 重启后自动注册 → 主机运行时列表显示远端 runtime（public）
3. **邀请成员**：Admin 生成邀请码 → 被邀请人输入地址+邀请码 → 页面立即 reload → 看到远端工作区 → daemon 自动同步
4. **断开恢复**：点"断开连接" → 回到本机 → 再次"加入工作区" → 显示历史列表 → 选择条目 → JWT 有效则直接连上
5. **JWT 过期重连**：断开后等 30 天（或手动清 token 模拟） → "加入工作区" → 历史条目 JWT 失效 → 提示输入新邀请码 → 连上
6. **地址变更**：远端重启后 IP 变了 → RemoteReconnectPage → 输入新地址 → 用已存 JWT 连上
7. **网络闪断**：断网 → WebSocket + daemon 心跳自动重连 → 恢复后无需操作

---

## 七、Tailscale 集成评估

### 决策：不内嵌 Tailscale，引导用户自行安装（方案 B）

### 评估背景

方案中多处涉及跨机器通信（添加电脑、连接远程服务器），需要评估是否在 Rimedeck 中内嵌 Tailscale 以简化网络层。

### 现状（已实现远程连接）

- 代码库中零 Tailscale 代码，`go.mod` 无 `tailscale.com` 依赖
- Desktop 内嵌 server 已改为绑定 `0.0.0.0:{port}`，支持局域网访问
- `GET /api/server-info` 自动检测 Tailscale 地址并展示
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

1. **Server 监听地址改为 `0.0.0.0`**：~~当前 `backend-manager.ts` 硬编码 `127.0.0.1`~~ 已完成，支持局域网和 Tailscale 网络访问
2. **UI 接受任意地址输入**："添加电脑"和"连接到远程服务器"中的地址输入框支持 IP、域名、Tailscale 域名（如 `my-pc.tailnet.ts.net`）
3. **文档说明**：在帮助文档中说明支持 Tailscale/ZeroTier 等 VPN 分配的地址，引导跨公网用户自行安装
4. **认证仍用认证码配对**：不依赖 Tailscale 的设备认证，保持方案独立性

### 后续可能的演进（P3+）

如果未来有强烈的跨公网需求且用户不愿自装 VPN，可考虑：
- 内嵌轻量 relay（如 libp2p hole-punch），仅做 NAT 穿透，不引入完整 VPN 栈
- 提供可选的 Tailscale 插件模式（用户自行启用），而非默认内嵌

---

## 八、实现状态总结（v0.3.20+）

### 整体架构

```
Desktop App
├── 前端（Electron renderer）
│   ├── 连接本机 server（auto-login, JWT）
│   └── 可切换到远端 server（switchRuntimeConfig + 远端 JWT）
│
├── Daemon（Go 进程，始终运行在本机 profile，不重启）
│   ├── 本机 server → 心跳 + 任务领取（primaryClient）
│   └── remoteServers[] → 每个远端独立 Client + 心跳（通过 /remote/add 热添加）
│
└── 统一操作入口
    ├── "添加电脑" → pairing code → addRemoteServer(url, mdt_token) → 秒级生效
    ├── "加入工作区" → invite code → switchConfig + addRemoteServer + reload
    ├── "停止共享" → removeRemoteServer(url) → 秒级生效
    └── "断开连接" → removeRemoteServer(url) + disconnectRuntimeConfig + reload
```

**核心设计原则**：
- **daemon 不重启**：所有远端操作通过 daemon health HTTP 端口（`/remote/add`、`/remote/remove`）热更新
- **本机连接不中断**：远端连接是独立的 `Client` + 独立的 heartbeat goroutine，本机 runtime 不受影响
- **凭据独立**：pairing code → `mdt_*` daemon token（算力）和 invite code → JWT（UI 访问）完全独立

### Flow 1：运行时 → 添加电脑（纯算力共享）

#### 连接

| 步骤 | Server 端（主机） | Client 端（远端机） |
|------|-----------|-----------|
| 1. 入口 | 运行时页 → "添加电脑" → `ConnectRemoteDialog` | 运行时页 → "连接到服务器" → `ConnectToServerDialog` |
| 2. 配对 | 显示 pairing code + 服务器地址 | 输入服务器地址 + pairing code |
| 3. 认证 | `POST /api/auth/pair` 验证 code → 返回 `mdt_*` token → 广播 `daemon:register` 事件 | 收到 `{ token, workspace_id }` |
| 4. 热添加 | — | `daemonAPI.addRemoteServer(url, token)` → daemon `/remote/add` → 秒级注册 |
| 5. 完成 | 收到 `daemon:register` 事件 → 对话框自动跳转成功 | 运行时列表显示远端 runtime（online, visibility=public） |

**关键文件**：
- `server/internal/handler/device_pair.go` — 配对端点（单次使用码 + `daemon:register` 事件广播）
- `server/internal/daemon/remote.go` — `AddRemoteServer()` / `RemoveRemoteServer()` / `runRemoteHeartbeat()`
- `server/internal/daemon/health.go` — `POST /remote/add` + `POST /remote/remove`
- `packages/views/runtimes/components/connect-remote-dialog.tsx` — 主机端对话框（WS 事件 + 轮询检测）
- `packages/views/runtimes/components/connect-to-server-dialog.tsx` — 远端对话框（调用 `addRemoteServer`）

**连接后状态**：

| 维度 | Server 端 | Client 端 |
|------|-----------|-----------|
| 前端 URL | 本地（不变） | 本地（不变） |
| daemon | 本地（不变） | 本地 + 远端（`remoteServers[]` 热添加） |
| 数据库 | `agent_runtime` 新增远端 runtime 行（visibility=public） | 不变 |
| 磁盘持久化 | — | `localStorage` 有 `rimedeck_remote_server` 记录 |

#### 断开

| 端 | 操作 | 效果 |
|----|------|------|
| **Server 端** | 运行时页 → 选中远端 runtime → 删除 | runtime 行删除 → daemon 心跳收到 `RuntimeGone` |
| **Client 端** | 运行时页 → "停止共享" 横幅 → 点击 | `removeRemoteServer(url)` → deregister + 停止心跳（秒级，不重启 daemon） |

---

### Flow 2：设置 → 成员 → 邀请 → 加入工作区

#### 连接

| 步骤 | Server 端（主机） | Client 端（远端机） |
|------|-----------|-----------|
| 1. 入口 | 设置 → 成员 → "邀请成员" → 生成邀请码 | 侧边栏 → "加入工作区" → `JoinWorkspaceDialog`（有历史则显示列表） |
| 2. 邀请 | 显示 6 位邀请码 + 服务器地址 | 输入服务器地址 + 邀请码（或从历史列表选择，免邀请码） |
| 3. 赎回 | `POST /api/invitations/redeem` → 创建 user + member → 返回 JWT + `mdt_*` token | 收到 `{ auth_token, token, workspace_id }` |
| 4. 前端切换 | — | `switchRuntimeConfig({ apiUrl, wsUrl, authToken, workspaceId })` |
| 5. 热添加 | — | `daemonAPI.addRemoteServer(url, mdt_token)` → 秒级注册 |
| 6. 刷新 | 成员列表更新 | `window.location.reload()` → AuthInitializer 用远端 JWT 认证 → 进入远端工作区 |

**关键文件**：
- `server/internal/handler/invitation.go` — `RedeemInvitation()`（JWT 签发 + 已有 member 处理）
- `packages/views/workspace/join-workspace-dialog.tsx` — 历史列表 + 邀请码表单
- `packages/core/platform/auth-initializer.tsx` — 401 时清 token，网络错误保留
- `apps/desktop/src/renderer/src/pages/remote-reconnect.tsx` — 远端连接失败时的重连页

**连接后状态**：

| 维度 | Server 端 | Client 端 |
|------|-----------|-----------|
| 前端 URL | 本地（不变） | **远端 server**（完整工作区 UI） |
| daemon | 本地（不变） | 本地 + 远端（`remoteServers[]` 热添加） |
| 数据库 | `user` + `member` + `daemon_token` + `agent_runtime` | 不变 |
| 磁盘 | — | `remote_connection.json`（URL + authToken）+ `remote_servers.json`（历史） |

#### 断开

| 步骤 | 操作 | 效果 |
|------|------|------|
| 1 | 侧边栏 → "断开远程连接"（或工作区列表中远端条目的 X 按钮） | — |
| 2 | `removeRemoteServer(url)` | daemon deregister 远端 runtime + 停止心跳 |
| 3 | `disconnectRuntimeConfig()` | 删除 `remote_connection.json`；恢复本地 |
| 4 | `localStorage.removeItem("multica_token")` | 清除远端 JWT |
| 5 | `window.location.reload()` | auto-login → 本机工作区 |

**`remote_servers.json` 保留**（供下次快速重连）。

#### 重连

**自动重连**（App 重启时，有 `remote_connection.json`）：

```
App 启动 → loadRemoteConfig() → 有远端配置
  → AuthInitializer 用存储的 JWT → getMe()
  ┌─ 成功 → 进入远端工作区
  ├─ 网络错误 → token 保留 → RemoteReconnectPage（重试 / 换地址 / 断开）
  └─ 401 → token 清除 → RemoteReconnectPage（重新加入 / 断开）
```

**手动重连**（断开后再加入）：

```
"加入工作区" → 有历史 → 选择条目 → 用存储的 JWT 尝试连接
  ┌─ JWT 有效 → 直接连上（无需邀请码）
  └─ JWT 过期 → 预填地址 → 输入新邀请码
```

---

### 认证闭环

**每个 Desktop 实例有独立的随机 JWT secret**。`RedeemInvitation` 签发由远端 server 自己密钥签名的 JWT，前端存储后在 reload 时使用。已有 member 时不返回 409，而是签发新凭据（允许重新获取过期 JWT）。

**时序**：
```
1. fetch POST <remote>/api/invitations/redeem → { auth_token, token, workspace_id }
2. switchRuntimeConfig({ apiUrl, wsUrl, authToken, workspaceId }) → 持久化
3. localStorage.setItem("multica_token", auth_token)
4. daemonAPI.addRemoteServer(url, token) ← 热添加
5. window.location.reload() → AuthInitializer → getMe() → 进入工作区
```

**用户身份**：三者独立

| 身份 | DB | 用途 |
|------|-----|------|
| Server 本机 owner | Server DB | 本机操作 |
| 赎回时新建的 user（`<code>@local.rimedeck`） | Server DB | 远端用户操作该工作区 |
| 远端机器本地 user | 远端本地 DB | 远端用户操作自己的工作区 |

---

### 运行时状态管理

**远端 runtime visibility**：daemon token 注册的 runtime 自动设为 `public`（`daemon.go` 在 `row.Inserted && isDaemonToken` 时调用 `UpdateAgentRuntimeVisibility`）。

**远端 runtime 生命周期**：

| 事件 | 行为 |
|------|------|
| `/remote/add` | 创建 Client → `registerRuntimesWithClient()` → 启动心跳 goroutine |
| 心跳 | `runRemoteHeartbeat()` 独立于主心跳，15s 周期 |
| `/remote/remove` | deregister → cancel heartbeat context → 从 `remoteServers` map 删除 |
| daemon 关闭 | `deregisterAllRemotes()` 清理所有远端连接 |
| server 端删除 runtime | 心跳收到 `RuntimeGone` → 停止该 runtime 的心跳 |

**self-healing 检查**：`isSelfHealingRuntime(runtime, currentUserId)` — 只有当前用户自己的 online local runtime 才被认为会自动重建（隐藏删除按钮）。远端 runtime（`owner_id` 不匹配）可正常删除。

---

### 工作区列表

Sidebar 工作区下拉菜单显示：
- **本地工作区**（当前 server 上的）
- **远程工作区**（来自 `remote_servers.json` 历史，非当前连接的条目）— 每个有退出按钮
- "加入工作区" / "断开远程连接" 操作

退出远程工作区：调用 `POST /api/workspaces/:id/leave` → 本地删除历史条目。

---

### 角色权限对比

| 角色 | 说明 | 权限 |
|------|------|------|
| **owner** | 所有者 | 完全访问权限，可管理所有设置、成员、删除工作区 |
| **admin** | 管理员 | 管理成员、设置、智能体；不能删除工作区 |
| **member** | 成员 | 创建和处理 issue、使用智能体和聊天 |

---

### 已知限制

1. **JWT 过期需重新邀请**：JWT 默认 30 天过期。历史列表自动尝试已存 JWT，过期则提示输入新邀请码（`RedeemInvitation` 对已有 member 签发新凭据）
2. **成员记录不自动清理**：断开后 Server 端 member 行保留，可通过工作区列表的退出按钮主动退出（调用 `POST /api/workspaces/:id/leave`）
3. **daemon token 注册的 runtime 无 owner_id**：普通 member 无法删除，需 owner/admin 操作
4. **占位邮箱不可登录**：`<code>@local.rimedeck` 用户只能依赖 JWT 或重新邀请
5. **IP 地址变化需手动处理**：重连页支持输入新地址，推荐使用 Tailscale 域名（不变）
