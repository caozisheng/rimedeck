export const ROUTING_TEMPLATE_BY_TYPE = `### 按任务类型分配
- 代码类任务（bug 修复、功能开发、重构）→ 分配给 [编码智能体]
- 文档类任务（文档撰写、翻译、README）→ 分配给 [写作智能体]
- 审查类任务（代码审查、安全审计）→ 分配给 [审查智能体]
- 不确定类型 → 使用 cn-skill-router 技能分析后决定`;

export const ROUTING_TEMPLATE_BY_PRIORITY = `### 按优先级处理
- P0 紧急：立即分配给最合适的在线成员
- P1 重要：正常分配，附带截止时间说明
- P2 一般：排入队列，空闲时处理`;

export const ROUTING_TEMPLATE_ESCALATION = `### 升级规则
- 成员回复 BLOCKED → 尝试分配给其他成员或升级给人类
- 超过 3 轮仍未解决 → 升级给人类 reporter`;
