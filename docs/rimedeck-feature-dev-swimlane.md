# 泳道图 Issue 关联可视化方案

## 背景

在 Rimedeck 中，issue 可以分配给 agent，agent 可以创建子 issue 并委派给其他 agent。在泳道图按 assignee 或 project 分组时，父子 issue 分散在不同泳道中，关联关系完全不可见。

## 方案概述

采用 **静态标记 + 交互高亮** 的双层方案：

### Layer B: 静态父级标记（Parent Badge）

在每张卡片的 chip 行显示父 issue 的 identifier（如 `↑ MUL-42`），点击可跳转至父 issue。

- 仅在 `parent_issue_id` 非空时显示
- 在泳道的 parent 分组模式下自动隐藏（泳道标题已包含父 issue 信息）
- 通过 Display Settings 中的 "Parent issue" 开关控制
- 样式与现有 project/label chip 一致：`rounded-full bg-muted/60 px-1.5 py-0.5 text-[11px]`

### Layer A: 交互式关联高亮

悬停或点击某张卡片时，高亮所有关联卡片并降低其他卡片透明度。

- 关联范围：父 issue + 兄弟 issue（同一 parent）+ 子 issue
- 非关联卡片 `opacity-30`，关联卡片加 `ring-2 ring-primary/50`
- 通过全局 zustand store 广播 `focusedIssueId` 和 `relatedIds`
- 拖拽开始时自动清除高亮，避免干扰

## 技术设计

### 新增文件

1. **`packages/core/issues/stores/relationship-focus-store.ts`**
   - 非持久化的 zustand 全局 store
   - 状态：`focusedIssueId: string | null`，`relatedIds: Set<string>`
   - 方法：`setFocus(issueId, relatedIds)`，`clearFocus()`

2. **`packages/views/issues/utils/relationship.ts`**
   - 纯函数 `computeRelatedIds(issue, allIssues): Set<string>`
   - 从已加载的 issue 列表计算关联 ID，无需额外 API 调用
   - 复杂度 O(n)，对于典型的 300-500 条 issue 可忽略

### 修改文件

| 文件 | 改动 |
|------|------|
| `view-store.ts` | `CardProperties` 新增 `parentBadge: boolean` |
| `board-card.tsx` | `BoardCardContent` 渲染父级 badge；`DraggableBoardCard` 添加 hover 处理和 dim/highlight 样式 |
| `board-column.tsx` | 透传 `parentIssueMap` 和 `allIssues` |
| `board-view.tsx` | 构建并透传 `parentIssueMap` 和 `allIssues`；drag start 清除高亮 |
| `swimlane-view.tsx` | 透传 `issueMap` 和 `mergedIssues`；drag start 清除高亮 |
| `issues-header.tsx` | Display Settings 新增 "Parent issue" 开关 |
| `locales/*/issues.json` | 新增 `card_parent_badge` 翻译 key |

### 数据流

```
BoardView / SwimLaneView
  ├─ issueMap (已有) → parentIssueMap (从中查找父 issue identifier)
  ├─ allIssues (已有的 issues/mergedIssues 数组)
  └─ DraggableBoardCard
       ├─ parentIdentifier → BoardCardContent (Layer B)
       ├─ onMouseEnter → computeRelatedIds → setFocus (Layer A)
       ├─ onMouseLeave → clearFocus (Layer A)
       └─ isDimmed / isHighlighted → CSS classes (Layer A)
```

### 关键设计决策

1. **不使用 React Context**：`allIssues` 通过 props 透传，因为组件树中已有数据，显式依赖更清晰
2. **不新增 API 调用**：关联计算完全基于已加载的 issue 列表
3. **父 issue identifier 查找**：优先从 `issueMap` 查找（零成本），仅在父 issue 不在当前视图时显示 "..."
4. **拖拽兼容**：badge 的 click/mousedown/pointerdown 事件 `stopPropagation()`，复用现有 `PickerWrapper` 模式
5. **allIssues 存入 ref**：hover 回调引用 ref 而非 prop，避免成为 memo 依赖

### 边界情况

- **父 issue 不在当前视图**：badge 显示 "..." 占位符
- **泳道 parent 分组模式**：badge 自动隐藏，避免重复信息
- **拖拽中**：drag start 时清除 focus，isDragging 的 opacity-30 与 isDimmed 自然组合
- **无关联的 issue**：没有 parent 也没有 children 的 issue，hover 时不触发高亮

## 实施阶段

1. **Phase 1**：基础设施（store + utility + view-store + i18n）
2. **Phase 2**：Layer B（父级 badge）
3. **Phase 3**：Layer A（交互高亮）
