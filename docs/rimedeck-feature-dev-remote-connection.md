# Rimedeck 去云化方案：添加电脑 & 邀请成员

## Context

Rimedeck 移除了 multica 的 cloud 功能，Desktop app 内嵌 server 实例。需要重新设计：
1. **添加电脑**：支持本机作为服务器/客户端，连接远端机器
2. **邀请成员**：以邀请码为主的无邮件邀请方式
3. **认证方式**：简化为 IP/Tailscale 域名 + 首次随机认证码（替代邮箱验证码）

---

## 一、认证方式改造（基础依赖）

### 现状
- 现有认证：邮箱 + 验证码（Resend API / SMTP / dev stdout）
- 用户注册需要邮箱

### 新方案：网络地址 + 认证码配对

**首次认证流程**：
1. Server 端（Desktop 内嵌）启动时生成 **设备认证码**（6位字母数字，如 `K3M9ZP`）
2. 认证码显示在 Server 端的 Desktop UI 上（状态栏 / 弹窗 / 设置页）
3. Client 端连接 Server 时：
   - 输入 Server 地址（IP 或 Tailscale 域名）
   - 弹出认证码输入框
   - 用户口头或屏幕分享获取认证码并输入
4. Server 验证认证码 → 颁发长期 token（类似现有 daemon token `mdt_`）
5. 后续连接使用 token 自动认证，无需再次输入

**关键改动**：
- 新增 `POST /api/auth/pair` 端点：接收认证码 → 返回长期 token
- Server 启动时生成认证码，存内存（或轻量持久化），可刷新
- Desktop UI 新增 "设备认证码" 显示区域（系统托盘或设置页顶部）
- 去掉邮箱强制要求，用户名可选填（或自动用设备名）
- 保留现有 PAT / daemon token 机制作为认证后的凭证载体

**涉及文件**：
- `server/internal/handler/` — 新增 pair 端点
- `server/internal/middleware/daemon_auth.go` — 支持新 token 类型（或复用 `mdt_`）
- `apps/desktop/src/` — UI 显示认证码
- `server/internal/daemon/config.go` — Client 端存储 server 地址和 token

---

## 二、添加电脑

### 模式 A：本机作为服务器（其他电脑连入）

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

**改动点**：
- `connect-remote-dialog.tsx`：
  - 从 `/api/config` 获取 `daemon_server_url`（内嵌 server 时应返回自身地址）
  - 显示本机 IP 和认证码
  - 命令改为 `multica setup self-host --server-url <url>`
- `server/internal/handler/config.go`：内嵌模式下返回 `daemon_server_url` = 本机地址
- 新增：本机 IP 检测逻辑（前端或通过 server API 返回）

### 模式 B：本机作为客户端（连接到远程服务器）

**用户操作流程**：
1. "运行时"页或设置中 → "连接到远程服务器"
2. 输入远程 Server 地址（如 `http://192.168.1.50:8080` 或 Tailscale 域名）
3. 输入远程 Server 的认证码
4. 本机 daemon 重配 `ServerBaseURL` 并连接
5. 连接成功后，本机出现在远程 Server 的运行时列表

**改动点**：
- 新增前端组件：`connect-to-server-dialog.tsx`
  - 输入 server URL + 认证码
  - 调用 `POST <remote-server>/api/auth/pair` 获取 token
  - 写入 daemon 配置 → 重启 daemon
- Desktop IPC：暴露 daemon 配置修改和重启能力
- 可能需要 `runtimes-page.tsx` 添加新按钮入口

---

## 三、邀请成员（邀请码为主）

### 流程

**Admin 端**：
1. 设置 → 成员 → "邀请成员"
2. 选择角色（member / admin）
3. 点击"生成邀请码"
4. 显示 **6位邀请码**（如 `XP39KM`）和有效期
5. Admin 口头/截图/消息告知被邀请人

**被邀请人端**：
1. 打开 Rimedeck Desktop
2. 首页/登录后 → "加入工作空间" → 输入邀请码
3. 匹配成功 → 自动加入工作空间

### 改动点

**Server 端**：
- DB migration：`workspace_invitation` 表新增 `invite_code VARCHAR(8)` 列 + 唯一索引
- `invitation.sql` 新增查询：`GetInvitationByCode`（按 code + status='pending' 查询）
- `invitation.go`：
  - `CreateInvitation()` 修改：生成随机 6 位码（大写字母 + 数字，去歧义字符如 O/0/I/1）
  - 新增 `POST /api/invitations/redeem` 端点：接收 `{ code: "XP39KM" }` → 查找邀请 → 执行接受逻辑（复用 `AcceptInvitation` 的事务逻辑）
  - 邮箱字段改为可选（`invitee_email` 可为空，code 是主要匹配方式）

**前端 — Admin 端**：
- `members-tab.tsx`：
  - 邀请表单改为：角色选择 + "生成邀请码" 按钮（移除强制邮箱输入）
  - 生成后显示邀请码（大字体 + 复制按钮）
  - 邀请列表中显示邀请码而非邮箱

**前端 — 被邀请人端**：
- 新增 `redeem-invite-dialog.tsx` 或在 onboarding 中添加 "输入邀请码" 步骤
- 调用 `POST /api/invitations/redeem`
- 成功后导航到新加入的工作空间

**i18n**：
- `locales/en/settings.json` 和 `locales/zh-Hans/settings.json` 添加邀请码相关文案

---

## 四、实现优先级

| 阶段 | 内容 | 复杂度 | 依赖 |
|------|------|--------|------|
| **P0** | 认证码配对机制（`POST /api/auth/pair`，Desktop 显示认证码）| 中 | 无 |
| **P0** | 添加电脑 — 模式 A（改造 connect-remote-dialog 显示 self-host 命令 + 认证码）| 低 | P0 认证 |
| **P0** | 邀请成员 — 邀请码（DB migration + redeem API + 前端改造）| 中 | 无 |
| **P1** | 添加电脑 — 模式 B（连接到远程服务器 dialog + daemon 重配）| 高 | P0 认证 |

---

## 五、关键文件清单

### 新增文件
- `server/migrations/0XX_invitation_invite_code.up.sql` — 邀请码列
- `packages/views/runtimes/components/connect-to-server-dialog.tsx` — 模式 B UI
- `packages/views/workspace/redeem-invite.tsx` — 输入邀请码页面

### 修改文件
- `server/internal/handler/invitation.go` — 邀请码生成 + redeem 端点
- `server/pkg/db/queries/invitation.sql` — 新查询
- `server/cmd/server/router.go` — 注册新路由
- `packages/views/settings/components/members-tab.tsx` — 邀请码 UI
- `packages/views/runtimes/components/connect-remote-dialog.tsx` — 显示 self-host 命令
- `server/internal/handler/config.go` — 内嵌模式返回自身地址
- `packages/core/api/client.ts` — 新增 API 方法

---

## 六、验证方式

1. **认证配对**：启动 Desktop → 查看认证码 → 另一台机器用认证码连接 → 验证 token 颁发和后续免码使用
2. **添加电脑 (A)**：打开"添加电脑" → 确认显示正确的 self-host 命令和认证码 → 远端执行 → 确认 runtime 注册成功
3. **邀请码**：Admin 生成邀请码 → 被邀请人输入码 → 确认加入工作空间 → 确认出现在成员列表
4. **添加电脑 (B)**：输入远程 server 地址和认证码 → 确认本机 daemon 重连 → 确认出现在远程 runtime 列表

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
