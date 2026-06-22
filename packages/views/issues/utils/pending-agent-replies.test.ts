import { describe, expect, it } from "vitest";
import type { AgentTask, TimelineEntry } from "@rimedeck/core/types";
import { buildPendingAgentReplyPlacement } from "./pending-agent-replies";

function comment(
  id: string,
  parentId: string | null,
  createdAt = "2026-06-22T00:00:00Z",
  actorType: "member" | "agent" = "member",
  actorId = "member-1",
): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: actorType,
    actor_id: actorId,
    content: id,
    parent_id: parentId,
    created_at: createdAt,
    updated_at: createdAt,
    comment_type: "comment",
  };
}

function task(id: string, triggerCommentId?: string, createdAt = "2026-06-22T00:00:01Z"): AgentTask {
  return {
    id,
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: createdAt,
    completed_at: null,
    result: null,
    error: null,
    created_at: createdAt,
    trigger_comment_id: triggerCommentId,
  };
}

describe("buildPendingAgentReplyPlacement", () => {
  it("places comment-triggered active tasks under the root comment thread", () => {
    const timeline = [
      comment("root-1", null),
      comment("reply-1", "root-1"),
    ];

    const placement = buildPendingAgentReplyPlacement(
      timeline,
      [task("task-1", "reply-1")],
      new Map(),
    );

    expect(placement.unplacedTasks).toEqual([]);
    expect(placement.pendingByRoot.get("root-1")).toMatchObject([
      {
        triggerCommentId: "reply-1",
        rootCommentId: "root-1",
        task: { id: "task-1" },
      },
    ]);
  });

  it("keeps issue-level active tasks in the standalone bucket", () => {
    const placement = buildPendingAgentReplyPlacement(
      [comment("root-1", null)],
      [task("task-1")],
      new Map(),
    );

    expect(placement.pendingByRoot.size).toBe(0);
    expect(placement.unplacedTasks.map((item) => item.id)).toEqual(["task-1"]);
  });

  it("omits tasks that already have a linked agent comment", () => {
    const placement = buildPendingAgentReplyPlacement(
      [comment("root-1", null)],
      [task("task-1", "root-1")],
      new Map([["agent-comment-1", "task-1"]]),
    );

    expect(placement.pendingByRoot.size).toBe(0);
    expect(placement.unplacedTasks).toEqual([]);
  });

  it("omits tasks when the likely agent reply already exists before task status catches up", () => {
    const placement = buildPendingAgentReplyPlacement(
      [
        comment("root-1", null, "2026-06-22T00:00:00Z"),
        comment("agent-reply-1", "root-1", "2026-06-22T00:00:04Z", "agent", "agent-1"),
      ],
      [task("task-1", "root-1", "2026-06-22T00:00:01Z")],
      new Map(),
    );

    expect(placement.pendingByRoot.size).toBe(0);
    expect(placement.unplacedTasks).toEqual([]);
  });
});
