const ROUTING_TABLE_START = "<!-- ROUTING_TABLE_START -->";
const ROUTING_TABLE_END = "<!-- ROUTING_TABLE_END -->";

export interface RoutingTableMember {
  name: string;
  memberType: "agent" | "member";
  role: string;
}

export function hasRoutingTable(instructions: string): boolean {
  return instructions.includes(ROUTING_TABLE_START);
}

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

export function appendMemberToRoutingTable(
  instructions: string,
  member: RoutingTableMember,
): string {
  const newRow = `| ${member.name} | ${member.memberType} | ${member.role} |  |`;
  const patched = instructions.replace(
    /(\n\n### 分配规则)/,
    `\n${newRow}$1`,
  );
  if (patched !== instructions) return patched;
  return instructions.replace(ROUTING_TABLE_END, `${newRow}\n${ROUTING_TABLE_END}`);
}

export function removeMemberFromRoutingTable(
  instructions: string,
  memberName: string,
): string {
  const escaped = memberName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const rowPattern = new RegExp(`^\\| ${escaped}[^|]*\\|.*$\\n?`, "m");
  return instructions.replace(rowPattern, "");
}
