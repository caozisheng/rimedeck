import { describe, expect, it } from "vitest";
import type { AgentTask, TimelineEntry } from "@rimedeck/core/types";
import { buildCommentTaskLinks } from "./task-comment-link";

function activity(id: string, actorId: string, taskId: string, createdAt = "2026-06-20T00:00:00Z"): TimelineEntry {
  return {
    type: "activity",
    id,
    actor_type: "agent",
    actor_id: actorId,
    action: "task_completed",
    details: { task_id: taskId },
    created_at: createdAt,
  };
}

function comment(id: string, actorId: string, createdAt = "2026-06-20T00:00:01Z"): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "agent",
    actor_id: actorId,
    content: "Done",
    created_at: createdAt,
    comment_type: "comment",
  };
}

function task(id: string, actorId: string, completedAt = "2026-06-20T00:00:00Z"): AgentTask {
  return {
    id,
    agent_id: actorId,
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-19T23:59:00Z",
    completed_at: completedAt,
    result: null,
    error: null,
    created_at: "2026-06-19T23:58:00Z",
  };
}

describe("buildCommentTaskLinks", () => {
  it("links an agent comment to the preceding task activity from the same agent", () => {
    const links = buildCommentTaskLinks([
      activity("act-1", "agent-1", "task-1"),
      comment("comment-1", "agent-1"),
    ]);

    expect(links.get("comment-1")).toBe("task-1");
  });

  it("does not link comments from a different agent", () => {
    const links = buildCommentTaskLinks([
      activity("act-1", "agent-1", "task-1"),
      comment("comment-1", "agent-2"),
    ]);

    expect(links.has("comment-1")).toBe(false);
  });

  it("does not link much later comments to an old task activity", () => {
    const links = buildCommentTaskLinks([
      activity("act-1", "agent-1", "task-1", "2026-06-20T00:00:00Z"),
      comment("comment-1", "agent-1", "2026-06-20T00:30:00Z"),
    ]);

    expect(links.has("comment-1")).toBe(false);
  });

  it("links an agent comment to a completed task when the activity has no task_id", () => {
    const links = buildCommentTaskLinks(
      [
        activity("act-1", "agent-1", ""),
        comment("comment-1", "agent-1", "2026-06-20T00:00:02Z"),
      ],
      [task("task-1", "agent-1", "2026-06-20T00:00:01Z")],
    );

    expect(links.get("comment-1")).toBe("task-1");
  });

  it("does not link an agent comment that predates task completion", () => {
    const links = buildCommentTaskLinks(
      [
        activity("act-1", "agent-1", ""),
        comment("comment-1", "agent-1", "2026-06-20T00:00:00Z"),
      ],
      [task("task-1", "agent-1", "2026-06-20T00:00:01Z")],
    );

    expect(links.has("comment-1")).toBe(false);
  });
});
