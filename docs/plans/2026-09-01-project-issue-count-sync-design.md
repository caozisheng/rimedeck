# GitLab 导入后项目 Issue 计数同步设计

## Problem

GitLab 项目导入通过 `ImportIssues` 在数据库事务中创建或更新本地 issue。Project API 的 `issue_count` / `done_count` 是按查询实时计算的，因此数据库数据已经正确；但导入路径没有发布项目状态变更事件，已打开客户端的 React Query 项目缓存不会失效。用户切换排序后看到 issue，或重启应用重新请求项目列表后，计数才出现。

## Decision

导入或 reconcile 成功写入 issue 后，发布一次 `project:updated` 事件，让现有前端 Project 查询缓存统一失效并自动重新拉取。事件只携带项目标识和 workspace 路由所需信息；计数继续由 Project API 从 issue 表计算，避免引入第二套计数维护逻辑。

事件发布属于提交后的通知，不参与导入事务：导入数据提交成功但通知失败时，不回滚导入；下一次周期 reconcile 或客户端重新获取仍可恢复一致性。

## Data Flow

1. GitLab worker 获取完整 issue snapshot。
2. `ImportIssues` 在事务中幂等创建/更新 issue 和 GitLab link。
3. 事务提交成功后，worker 发布一次 `project:updated`，包含 `workspace_id`、`project_id`，以及可选的最新 Project 响应/计数字段。
4. Web realtime handler 收到项目事件并失效 `projectKeys.all(wsId)`。
5. 已打开的 Project 列表、详情及依赖同一缓存前缀的页面自动 refetch，显示最新 `issue_count` / `done_count`。

## Scope and Error Handling

- 覆盖初始导入、增量 reconcile 和 full reconcile 复用的 issue 导入路径。
- 同一次导入只发布一次项目事件，不按 issue 逐条发布，避免事件风暴。
- 空 issue snapshot 也应使项目缓存失效，因为远端删除/状态变化可能让计数下降。
- 不改变数据库 schema、Project API、issue 状态映射或排序逻辑。
- 事件缺少 project ID 时不应让客户端刷新整个 workspace；实现应使用 tracker 的 `ProjectID` 作为路由来源。

## Verification

- Go 回归测试证明 issue 导入事务成功后发出一次项目更新通知，并覆盖非空及空 snapshot。
- 前端 realtime updater 测试证明 `project:updated` 使 `projectKeys.all(wsId)` 失效。
- 运行相关 Go/TypeScript 定向测试；必要时执行实际导入/事件链路 smoke test。
