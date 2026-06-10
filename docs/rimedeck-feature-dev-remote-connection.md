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

### 模式 B：本机 daemon 贡献算力给远程工作区（已实现）

当前实现为**单 server 切换**：daemon 一次只能连一个 server。通过 `ConnectToServerDialog` 输入远端地址 + pairing code 完成配对后，daemon 切换到远端 server，本机 server 不再收到心跳。

**实现方式**：
- `connect-to-server-dialog.tsx`：输入 server 地址 + pairing code → `POST /api/auth/pair` → 获取 `mdt_*` token
- `daemonAPI.setTargetApiUrl(url)` → `daemonAPI.syncToken(token, "")` → `daemonAPI.restart()`
- daemon 重启后使用远端 profile（`desktop-<remote-host>`），在远端 server 注册 runtime
- 前端不切换，仍指向本机 server

**限制**：daemon 切换到远端后，本机 server 的 runtime 不再收到心跳，150s 后被 sweeper 标记 offline。未来可考虑多 server 并行（需多 client 架构）。

**未来设计方向（多 server 并行）**：
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
| ~~P3+~~ ✅ | 添加电脑 — 模式 B（daemon 单 server 切换，共享算力到远端） | 中 | P0 认证 |

---

## 五、关键文件清单

### 新增文件
- `server/internal/handler/server_info.go` — `GET /api/server-info`，Go 侧检测本机网络接口 + pairing code
- `server/internal/handler/device_pair.go` — `POST /api/auth/pair`，设备认证码配对
- `packages/views/common/server-address-bar.tsx` — 共用的服务器地址展示组件
- `packages/views/runtimes/components/connect-to-server-dialog.tsx` — daemon 连远程 server UI（Client 端）
- `packages/views/workspace/join-workspace-dialog.tsx` — 输入邀请码 + 切换前端 API

### 修改文件
- `apps/desktop/src/main/local-backend/backend-manager.ts` — server 监听地址 `127.0.0.1` → `0.0.0.0`
- `apps/desktop/src/main/index.ts` — `runtime-config:switch` / `disconnect` IPC + `loadRemoteConfig()` / `saveRemoteConfig()`
- `apps/desktop/src/main/daemon-manager.ts` — `syncToken()`、`setTargetApiUrl()`、`clearToken()`
- `apps/desktop/src/preload/index.ts` — 暴露 runtime-config 切换 / authToken / history IPC
- `apps/desktop/src/renderer/src/App.tsx` — auto-login guard + user-login effect（pending daemon token）
- `apps/desktop/src/renderer/src/pages/remote-reconnect.tsx` — 远端连接失败时的重连页
- `apps/desktop/src/renderer/src/components/desktop-runtimes-page.tsx` — daemon_id 粘性缓存（仅本地时更新）
- `apps/desktop/src/renderer/src/components/desktop-agents-page.tsx` — 同上
- `server/internal/handler/invitation.go` — 邀请码生成 + redeem 端点 + JWT 签发 + 已有 member 处理
- `server/internal/handler/daemon.go` — daemon token runtime 自动 public + deregister 立即删除
- `server/internal/handler/device_pair.go` — 配对码单次使用 + 限流
- `server/cmd/server/router.go` — 注册新路由
- `packages/core/platform/auth-initializer.tsx` — 401 时清 token，网络错误保留
- `packages/views/workspace/join-workspace-dialog.tsx` — 简化（无 daemon ops）+ 历史列表
- `packages/views/runtimes/components/connect-remote-dialog.tsx` — 轮询 fallback
- `packages/views/runtimes/components/connect-to-server-dialog.tsx` — daemon restart fire-and-forget
- `packages/views/layout/app-sidebar.tsx` — 断开连接（含 localStorage + remote_server 清除）
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

### Flow 1：运行时 → 添加电脑（纯算力共享）

#### 连接

| 步骤 | Server 端 | Client 端 |
|------|-----------|-----------|
| 1. 入口 | 运行时页 → "添加电脑" → `ConnectRemoteDialog` | 运行时页 → "连接到服务器" → `ConnectToServerDialog` |
| 2. 配对 | 显示 pairing code + 服务器地址，监听 `daemon:register` WS 事件 | 输入服务器地址 + pairing code |
| 3. 认证 | `POST /api/auth/pair` 验证 code → 返回 `mdt_*` daemon token | 收到 `{ token, workspace_id }` |
| 4. Daemon 配置 | — | `setTargetApiUrl(url)` → `syncToken(mdt_token)` → `restart()` |
| 5. Daemon 注册 | 收到 `daemon:register` 事件 → 对话框跳转成功页（同时有 3s 轮询 fallback） | Daemon 用 token + server_url 向 server 注册 |
| 6. 完成状态 | 运行时列表显示远端 runtime（在线，visibility=public） | **前端不切换**，停留在本地工作区；daemon 在后台共享算力 |

**关键文件**：
- `server/internal/handler/device_pair.go` — `DevicePair()` 配对端点
- `packages/views/runtimes/components/connect-remote-dialog.tsx` — Server 端对话框
- `packages/views/runtimes/components/connect-to-server-dialog.tsx` — Client 端对话框
- `apps/desktop/src/main/daemon-manager.ts:syncToken()` — `mdt_*` token 透传 + `server_url` 写入

**连接后状态**：

| 维度 | Server 端 | Client 端 |
|------|-----------|-----------|
| 前端 URL | 本地（不变） | 本地（不变） |
| daemon 连接 | 本地 | **远端 server**（共享算力） |
| 数据库 | `agent_runtime` 新增远端 runtime 行（visibility=public） | 不变 |
| 运行时分组 | 远端 runtime 在"远程"分组 | 本机 runtime 保持在"本机"分组（`lastIdentity` 粘性缓存，不被远端 profile 覆盖） |
| 磁盘持久化 | — | daemon profile config 有 `server_url` + `token`；`localStorage` 有记录 |
| 重启后 | 不变 | daemon 从 profile config 读 `server_url` + `token`，自动重连 |

#### 断开

| 端 | 操作 | 效果 |
|----|------|------|
| **Server 端** | 运行时页 → 选中远端 runtime → 删除 | runtime 从列表消失；daemon 心跳收到 `RuntimeGone` 标志 → 丢弃该 runtime |
| **Client 端** | 停止 daemon 或清除 daemon profile config | daemon 调用 `deregister` → 无 agent 绑定的 runtime 立即删除；有 agent 绑定的标记 offline |

**Daemon deregister 行为**（`daemon.go:DaemonDeregister`）：
- daemon 关闭时自动调用（`defer d.deregisterRuntimes()`，5s 超时）
- 对每个 runtime 检查 `CountActiveAgentsByRuntime`
  - `== 0`：先清理已归档 agent，再 `DeleteAgentRuntime`（立即从 DB 删除）
  - `> 0`：`SetAgentRuntimeOffline`（保留行，等 sweeper 在 7 天后删除）

#### 重连

- **网络闪断**：daemon 心跳 15s 重试 + WebSocket 指数退避（1s→30s），自动恢复
- **Client 重启**：daemon 从 profile config 读取 `server_url` + `token`，自动重连
- **Server 重启**：daemon 心跳失败 → `handleRuntimeGone` → `registerRuntimesForWorkspace` → `RecoverOrphans` → 恢复
- **Runtime 被 Server 删除**：daemon 心跳收到 `RuntimeGone` 标志 → `handleRuntimeGone` → 从本地状态移除该 runtime；如果有 token 则重新注册新 runtime 行

---

### Flow 2：设置 → 成员 → 邀请 → 加入工作区

#### 连接

| 步骤 | Server 端 | Client 端 |
|------|-----------|-----------|
| 1. 入口 | 设置 → 成员 → "邀请成员" → 生成邀请码 | 侧边栏工作区菜单 → "加入工作区" → `JoinWorkspaceDialog` |
| 2. 邀请 | 显示 6 位邀请码 + 服务器地址 | 输入服务器地址 + 邀请码 |
| 3. 赎回 | `POST /api/invitations/redeem` → 创建 user + member + 返回 `mdt_*` token + JWT | 收到 `{ member, workspace_id, user_id, token, auth_token }` |
| 4. 前端切换 | — | `switchRuntimeConfig({ apiUrl, wsUrl, authToken })` → 持久化到磁盘 |
| 5. 存储 JWT | — | `localStorage.setItem("multica_token", auth_token)` |
| 6. 存储 daemon token | — | `localStorage.setItem("rimedeck_pending_daemon_token", ...)` 供 reload 后使用 |
| 7. 页面刷新 | — | `window.location.reload()` → 立即 reload，不等 daemon |
| 8. Auth 初始化 | — | `AuthInitializer` 读 JWT → `getMe()` → 用户登录成功 |
| 9. Daemon 同步 | 收到 `daemon:register` 事件 | App.tsx user-login effect 读取 pending daemon token → `syncToken` → `restart` → daemon 注册 |

**关键文件**：
- `server/internal/handler/invitation.go` — `RedeemInvitation()` 赎回端点（含 daemon token 生成 + JWT 签发 + 已有 member 处理）
- `packages/views/workspace/join-workspace-dialog.tsx` — Client 端对话框（简化版：只做 redeem + switchConfig + reload）
- `packages/views/layout/app-sidebar.tsx` — "断开远程连接"菜单项
- `apps/desktop/src/main/index.ts` — `loadRemoteConfig()` / `saveRemoteConfig()` 持久化（含 authToken）
- `apps/desktop/src/renderer/src/App.tsx` — user-login effect 处理 pending daemon token
- `packages/core/platform/auth-initializer.tsx` — 只在 401 时清除 token，网络错误保留

**连接后状态**：

| 维度 | Server 端 | Client 端 |
|------|-----------|-----------|
| 前端 URL | 本地（不变） | **远端 server**（完整工作区 UI） |
| daemon 连接 | 本地 | **远端 server**（共享算力） |
| 数据库 | `user` + `member` + `daemon_token` + `agent_runtime` 新增 | 不变 |
| 磁盘持久化 | — | `~/.rimedeck/remote_connection.json`（前端 URL + authToken）；`~/.rimedeck/remote_servers.json`（连接历史）；`localStorage` 中的 JWT；daemon profile config（`server_url` + `token`） |
| 重启后 | 不变 | 前端从 `remote_connection.json` 恢复远端 URL；auth store 从 `localStorage` 读取远端 JWT 自动认证；daemon 从 profile config 自动重连 |

#### 断开

| 步骤 | 操作 | 效果 |
|------|------|------|
| 1 | Client 侧边栏 → 工作区菜单 → "断开远程连接" | — |
| 2 | `disconnectRuntimeConfig()` | 删除 `~/.rimedeck/remote_connection.json`；内存 `runtimeConfigResult` 恢复本地 |
| 3 | `localStorage.removeItem("multica_token")` | 清除远端 JWT，防止 reload 后用过期凭证请求本地 server |
| 4 | `clearToken()` | 删除 daemon profile config 中的 `token` + `server_url` |
| 5 | `setTargetApiUrl("")` | 内存中清空 `targetApiBaseUrl` |
| 6 | `restart()` | daemon 重启 → 调用 `deregister`（远端 runtime 立即删除）→ 连回本地 backend |
| 7 | `window.location.reload()` | 前端刷新 → `isRemote=false` → auto-login 触发 → 以 `local@rimedeck.local` 登录本机 server |

**断开后状态**：

| 维度 | Server 端 | Client 端 |
|------|-----------|-----------|
| 前端 URL | 不变 | 恢复本地 |
| daemon | 远端 runtime 立即删除（无 agent 绑定时）或标记 offline | 连回本地 |
| 成员记录 | member 行保留（需管理员手动移除） | — |
| daemon token | 数据库中 token 仍然有效（365天） | config 已清除，无法使用 |
| JWT | — | localStorage 已清除 |
| 磁盘 | — | `remote_connection.json` 已删；`multica_token` 已清除；daemon profile config 已清除；**`remote_servers.json` 保留**（供下次快速重连） |

**断开后能否操作远端工作区**：**不能**。六层保护：
1. 前端 URL → 指向本地，API 请求不到远端
2. `remote_connection.json` → 已删，重启不恢复
3. `localStorage("multica_token")` → 已清除，无法携带远端 JWT
4. daemon `token` → 已清除，无法认证
5. daemon `server_url` → 已清除，不知道远端地址
6. daemon 进程 → 已重启，连到本地

#### 重连

**自动重连**（App 重启时，有 `remote_connection.json`）：

App 启动时 `loadRemoteConfig()` 发现远端配置 → 使用远端 URL 初始化 `CoreProvider` → `AuthInitializer` 读 JWT → `getMe()` 尝试认证：

```
App 启动 → loadRemoteConfig() → 有远端配置
  → CoreProvider(remoteApiUrl) → AuthInitializer 读 localStorage JWT
  ┌─ getMe() 成功 → user 设置 → DesktopShell 渲染 → 正常使用
  ├─ 网络错误 → token 保留（不清除）→ RemoteReconnectPage 显示：
  │   ├─ "重试" → 从磁盘恢复 token（如 localStorage 被清） → initialize() 
  │   ├─ "更换地址" → 输入新 IP/域名 → switchRuntimeConfig → reload
  │   └─ "断开连接" → disconnect 流程 → 回到本机
  └─ 401（JWT 过期）→ token 清除 → RemoteReconnectPage 显示：
      ├─ "重新加入" → JoinWorkspaceDialog → 用新邀请码获取新 JWT
      └─ "断开连接" → disconnect 流程 → 回到本机
```

**关键机制**：
- `AuthInitializer` 只在 401 时清除 token，网络错误保留（允许重试）
- JWT 同时存储在 `localStorage` 和 `remote_connection.json.authToken`（磁盘备份）
- `RemoteReconnectPage` 不独立调用 `initialize()`，只在用户点"重试"时触发

**手动重连**（断开后，通过 JoinWorkspaceDialog）：

断开后 `remote_connection.json` 被删除，但 `~/.rimedeck/remote_servers.json` 保留连接历史。用户点"加入工作区"时：

```
JoinWorkspaceDialog 打开 → getRemoteHistory()
  ┌─ 有历史 → 显示"最近连接"列表：
  │   ├─ 选择条目 → fetch /api/me(authToken) → 成功 → switchConfig + reload（无需邀请码）
  │   ├─ 选择条目 → JWT 过期(401) → 预填地址 → 提示输入新邀请码
  │   └─ "连接新服务器" → 显示地址+邀请码表单
  └─ 无历史 → 直接显示地址+邀请码表单
```

**`remote_servers.json` 格式**：
```json
[
  { "apiUrl": "http://192.168.1.100:18080", "authToken": "<jwt>", "lastConnected": "2026-06-10T..." }
]
```

每次 `saveRemoteConfig()` 时自动追加/更新到历史列表。disconnect 不删除历史。

**其他重连场景**：

- **网络闪断**：前端 WebSocket 自动重连（`use-realtime-sync.ts`）；daemon 心跳 + WS 退避自动恢复
- **Server 重启**：daemon `handleRuntimeGone` → 重注册 → `RecoverOrphans`；前端 WebSocket 重连
- **Daemon token 过期**（365天后）：daemon 心跳认证失败，需重新邀请加入

---

### 认证闭环：赎回时签发 JWT

**问题**：每个 Desktop 实例在首次启动时生成独立的随机 JWT secret（`local-backend/config.ts` — `randomBytes(32)`）。机器 A 签发的 JWT 在机器 B 的 server 上**必定验签失败**。如果 `RedeemInvitation` 不签发 JWT，远端用户 reload 后 localStorage 中的本机 JWT 在远端 server 上 401，无法操作工作区。

**方案**：`RedeemInvitation` 在创建 user + member 后，调用 `issueJWT(user)` 签发由远端 server 自己密钥签名的 JWT，通过 `auth_token` 字段返回。已有 member 时不返回 409，而是查找已有记录继续签发凭据（允许用新邀请码刷新过期 JWT）。

前端 dialog 只做 redeem + switchConfig + store JWT + reload（不阻塞 daemon 操作）。Daemon 同步由 App.tsx 的 user-login effect 在 reload 后处理。

**时序**：
```
1. fetch POST <remote>/api/invitations/redeem
   → { token: "mdt_*", auth_token: "<jwt>", member, workspace_id, user_id }
2. switchRuntimeConfig({ apiUrl, wsUrl, authToken })  → 主进程持久化到 remote_connection.json
3. localStorage.setItem("multica_token", auth_token)
4. localStorage.setItem("rimedeck_pending_daemon_token", { token, userId, serverUrl })
5. window.location.reload()  ← 立即 reload，不等 daemon
   → AuthInitializer 读 JWT → getMe() → user 设置 → DesktopShell 渲染
   → App.tsx user-login effect 发现 pending daemon token → syncToken → restart → daemon 注册
```

**用户身份**：远端用户操作工作区时，使用的是赎回时在 Server DB 上新建的用户身份，而非 Server 本机 owner 或远端机器的本地用户。三者完全独立：

| 身份 | 存在于哪个 DB | 用途 |
|------|-------------|------|
| Server 本机 owner | Server DB | 本机操作工作区 |
| 赎回时新建的 user（`<code>@local.rimedeck`） | Server DB | 远端用户操作该工作区 |
| 远端机器本地 user | 远端机器本地 DB | 远端用户操作自己本地的工作区 |

JWT 的 `sub` claim 写的是赎回时新建 user 的 UUID，Auth middleware 据此设置 `X-User-ID`，所有 API 操作（创建 issue、操作 agent 等）均归属该身份。

**设计决策**：
- 不调用 `SetAuthCookies`：Electron 渲染进程从 `localhost` 发请求到远端 IP，`SameSite: Strict` cookie 不会被发送。Desktop 统一用 `localStorage` token 模式
- JWT 签发失败不阻断请求（member 已创建），只 log warning
- `auth_token` 字段名与 `token`（daemon token `mdt_*`）区分

---

### 运行时状态管理

#### 远端 runtime 的 visibility 和 owner_id

daemon token（`mdt_*`）注册的 runtime 没有 `owner_id`（`daemon.go` 中 daemon token 认证路径不设置 ownerID，`COALESCE` 保留已有值或 NULL）。为解决"智能体选择器中远端 runtime 显示为私有"的问题：

- **首次注册时自动设为 `public`**：`daemon.go:RegisterDaemonRuntimes` 在 `row.Inserted == true` 且认证为 daemon token 时，调用 `UpdateAgentRuntimeVisibility` 设为 `"public"`
- owner/admin 可在运行时页手动切换 public/private（`PATCH /api/runtimes/:id`，检查 `canEditRuntime`）
- `owner_id = NULL` 的 runtime：普通 member 无法删除（`canEditRuntime` 需要 owner 匹配或 admin 角色），需 owner/admin 操作

#### 远端 runtime 的生命周期

| 事件 | 行为 |
|------|------|
| daemon 注册 | `UpsertAgentRuntime` → 新行 visibility=public（daemon token 认证时） |
| daemon 心跳 | `last_seen_at` 刷新（15s 周期） |
| daemon 正常关闭 | `deregister` → 无 agent 绑定则立即删除，有绑定则标记 offline |
| daemon 异常退出（没有 deregister） | sweeper 150s 后标记 offline → 7 天后删除（无 agent 绑定时） |
| Server 端手动删除 | runtime 行删除 → daemon 心跳收到 `RuntimeGone` → 从本地状态移除 |

#### 客户端 daemon_id 保持

`DesktopRuntimesPage` 和 `DesktopAgentsPage` 使用 `lastIdentity` 粘性缓存记住本地 daemon_id。缓存 **只在 daemon 指向本地 server 时更新**（检查 `status.serverUrl` 是否为 `127.0.0.1` 或 `localhost`）。当 daemon 切换到远端 server 共享算力时，缓存不被覆盖，本机 runtime 继续匹配 `isCurrent` → 显示在"本机"分组下。

#### 前端 auto-login 保护

`App.tsx` 中的 auto-login（用 `local@rimedeck.local` + `000000` 自动登录）在检测到远端连接时跳过（`isRemote` 检查 `runtimeConfig.apiUrl` 是否为非本地地址）。这防止在远端 server 上误创建 `local@rimedeck.local` 用户。断开后 `isRemote` 恢复为 false，auto-login 正常触发。

---

### 角色权限对比

| 角色 | 说明 | 权限 |
|------|------|------|
| **owner** | 所有者 | 完全访问权限，可管理所有设置、成员、删除工作区 |
| **admin** | 管理员 | 管理成员、设置、智能体；不能删除工作区 |
| **member** | 成员 | 创建和处理 issue、使用智能体和聊天 |

---

### 已知限制

1. **JWT 过期需重新邀请**：JWT 默认 30 天过期。重连页引导用户输入新邀请码（`RedeemInvitation` 对已有 member 签发新凭据）。断开后再加入时，历史列表自动尝试已存 JWT，过期则提示输入新邀请码
2. **成员记录不自动清理**：断开后 Server 端的 member 行保留，需管理员手动移除
3. **单 daemon 单 server**：daemon 一次只能连一个 server（本地或远端），不支持同时服务多个 server。切换到远端后本机 server 的 runtime 会因无心跳而被标记 offline
4. **daemon token 注册的 runtime 无 owner_id**：普通 member 无法通过 `canEditRuntime` 检查删除这些 runtime，需 owner/admin 操作
5. **占位邮箱不可登录**：赎回时创建的 `<code>@local.rimedeck` 用户无法通过邮箱验证码流程登录，只能依赖 JWT 或重新邀请
6. **IP 地址变化需手动处理**：局域网 IP 变化后，自动重连失败，需通过重连页输入新地址或使用 Tailscale 域名（域名不变）
