# Analytics View 设计文档

## 背景

现有 4 种视图（Board / List / Gantt / Swimlane）都聚焦于「单个任务的管理和操作」，缺少聚合维度的可视化。Analytics View 提供项目整体进展的统计概览。

## 视图布局（4 个图表卡片）

```
┌─────────────────────────────────────────────────────────────────┐
│  [Board] [List] [Gantt] [Swimlane] [Analytics✨]    Filters... │
├───────────────────────────────┬─────────────────────────────────┤
│                               │                                 │
│   ① 状态分布 (Donut Chart)    │   ② 优先级分布 (Bar Chart)       │
│                               │                                 │
│     ┌───╮     Backlog  12     │    ■■■■■■■■  urgent    3       │
│    │     │    Todo     8      │    ■■■■■■    high      5       │
│    │  34 │    In Prog  6      │    ■■■■■■■■■ medium    8       │
│    │     │    Done     5      │    ■■■        low      2       │
│     └───╯     Cancelled 3    │    ■■■■■■■■■■■■■■■■ none  16   │
│                               │                                 │
├───────────────────────────────┼─────────────────────────────────┤
│                               │                                 │
│   ③ 负责人工作量               │   ④ 每日创建趋势                 │
│      (Horizontal Bar)         │      (Area / Line Chart)        │
│                               │                                 │
│   Alice  ■■■■■■■■  8         │        ╱──                      │
│   Bob    ■■■■■  5            │      ╱╱   done ━━━              │
│   Agent1 ■■■■■■■■■■■ 11     │    ╱╱     in_progress ───       │
│   (none) ■■■■■■■■■■ 10      │   ╱       todo ···              │
│                               │   ──────────────────→           │
│                               │   -14d          today           │
│                               │                                 │
└───────────────────────────────┴─────────────────────────────────┘
```

## 4 个图表详解

| # | 图表 | 类型 | 数据源 | 交互 |
|---|------|------|--------|------|
| ① | **状态分布** | Donut Chart | `issue.status` 计数 | hover 高亮 + tooltip |
| ② | **优先级分布** | Horizontal Bar | `issue.priority` 计数 | hover tooltip |
| ③ | **负责人工作量** | Horizontal Bar | `assignee` 分组计数，按未完成数降序 | hover tooltip |
| ④ | **创建趋势** | Area Chart | 按 `created_at` 聚合，最近 14/30/90 天 | 时间范围切换 |

## 关键设计决策

1. **纯前端聚合** — 复用 `issueListOptions(wsId)` 已有查询结果，无需新 API
2. **纯 SVG 手绘** — 与 Gantt 视图风格一致，不引入新图表库依赖
3. **响应 Filter** — 和其他视图一样响应 header 的 scope / status / priority / assignee 过滤器
4. **ViewMode 扩展**：`"board" | "list" | "gantt" | "swimlane" | "analytics"`

## 文件结构

```
packages/views/issues/components/
├── analytics-view.tsx          # 主视图容器 + 4 卡片布局
├── analytics/
│   ├── status-donut.tsx        # ① 状态环形图
│   ├── priority-bars.tsx       # ② 优先级柱状图
│   ├── workload-bars.tsx       # ③ 负责人工作量
│   └── trend-chart.tsx         # ④ 创建趋势
```

## Props 接口

```tsx
export function AnalyticsView({
  issues,
}: {
  issues: Issue[];
})
```
