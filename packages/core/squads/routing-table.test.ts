import { describe, expect, it } from "vitest";
import {
  hasRoutingTable,
  generateRoutingTable,
  appendMemberToRoutingTable,
  removeMemberFromRoutingTable,
} from "./routing-table";

describe("hasRoutingTable", () => {
  it("returns true when markers present", () => {
    expect(hasRoutingTable("some text\n<!-- ROUTING_TABLE_START -->\n...\n<!-- ROUTING_TABLE_END -->")).toBe(true);
  });
  it("returns false when no markers", () => {
    expect(hasRoutingTable("just plain instructions")).toBe(false);
  });
  it("returns false for empty string", () => {
    expect(hasRoutingTable("")).toBe(false);
  });
});

describe("generateRoutingTable", () => {
  it("sorts members alphabetically", () => {
    const result = generateRoutingTable([
      { name: "Charlie", memberType: "member", role: "审查" },
      { name: "Alice", memberType: "agent", role: "文档撰写" },
      { name: "Bob", memberType: "agent", role: "开发" },
    ]);
    const lines = result.split("\n");
    const dataRows = lines.filter((l) => l.startsWith("| ") && !l.startsWith("| 成员") && !l.startsWith("|--"));
    expect(dataRows[0]).toContain("Alice");
    expect(dataRows[1]).toContain("Bob");
    expect(dataRows[2]).toContain("Charlie");
  });

  it("generates valid markdown with markers", () => {
    const result = generateRoutingTable([
      { name: "Agent1", memberType: "agent", role: "" },
    ]);
    expect(result).toContain("<!-- ROUTING_TABLE_START -->");
    expect(result).toContain("<!-- ROUTING_TABLE_END -->");
    expect(result).toContain("| 成员 | 类型 | 角色 | 擅长领域 |");
    expect(result).toContain("| Agent1 | agent |  |  |");
  });

  it("handles empty member list", () => {
    const result = generateRoutingTable([]);
    expect(result).toContain("<!-- ROUTING_TABLE_START -->");
    expect(result).toContain("### 分配规则");
  });
});

describe("appendMemberToRoutingTable", () => {
  const base = generateRoutingTable([
    { name: "Alice", memberType: "agent", role: "开发" },
  ]);

  it("appends before assignment rules", () => {
    const result = appendMemberToRoutingTable(base, {
      name: "Bob",
      memberType: "agent",
      role: "文档",
    });
    expect(result).toContain("| Bob | agent | 文档 |  |");
    const bobIdx = result.indexOf("Bob");
    const rulesIdx = result.indexOf("### 分配规则");
    expect(bobIdx).toBeLessThan(rulesIdx);
  });

  it("falls back to end marker if rules heading is missing", () => {
    const noRules = base.replace("### 分配规则", "");
    const result = appendMemberToRoutingTable(noRules, {
      name: "Dave",
      memberType: "member",
      role: "",
    });
    expect(result).toContain("| Dave |");
    expect(result).toContain("<!-- ROUTING_TABLE_END -->");
  });
});

describe("removeMemberFromRoutingTable", () => {
  const base = generateRoutingTable([
    { name: "Alice", memberType: "agent", role: "开发" },
    { name: "Bob", memberType: "agent", role: "文档" },
  ]);

  it("removes matching member row", () => {
    const result = removeMemberFromRoutingTable(base, "Alice");
    expect(result).not.toContain("Alice");
    expect(result).toContain("Bob");
  });

  it("leaves instructions unchanged if no match", () => {
    const result = removeMemberFromRoutingTable(base, "NonExistent");
    expect(result).toBe(base);
  });

  it("handles member names with regex special characters", () => {
    const withSpecial = appendMemberToRoutingTable(base, {
      name: "Agent (v2.0)",
      memberType: "agent",
      role: "",
    });
    const result = removeMemberFromRoutingTable(withSpecial, "Agent (v2.0)");
    expect(result).not.toContain("Agent (v2.0)");
    expect(result).toContain("Alice");
  });
});
