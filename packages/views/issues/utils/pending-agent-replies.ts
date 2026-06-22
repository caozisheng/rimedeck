import type { AgentTask, TimelineEntry } from "@rimedeck/core/types";

export interface PendingAgentReply {
  task: AgentTask;
  triggerCommentId: string;
  rootCommentId: string;
}

export interface PendingAgentReplyPlacement {
  pendingByRoot: Map<string, PendingAgentReply[]>;
  unplacedTasks: AgentTask[];
}

export function buildPendingAgentReplyPlacement(
  timeline: TimelineEntry[],
  activeTasks: readonly AgentTask[],
  commentTaskLinks: ReadonlyMap<string, string>,
): PendingAgentReplyPlacement {
  const linkedTaskIds = new Set(commentTaskLinks.values());
  const commentsById = new Map(
    timeline
      .filter((entry) => entry.type === "comment")
      .map((entry) => [entry.id, entry]),
  );
  const comments = [...commentsById.values()];
  const pendingByRoot = new Map<string, PendingAgentReply[]>();
  const unplacedTasks: AgentTask[] = [];

  for (const task of activeTasks) {
    if (linkedTaskIds.has(task.id)) continue;
    const triggerCommentId = task.trigger_comment_id;
    if (!triggerCommentId) {
      unplacedTasks.push(task);
      continue;
    }

    const triggerComment = commentsById.get(triggerCommentId);
    if (!triggerComment) {
      unplacedTasks.push(task);
      continue;
    }

    const root = findThreadRoot(triggerComment, commentsById);
    if (!root) {
      unplacedTasks.push(task);
      continue;
    }
    if (hasLikelyAgentReply(comments, commentsById, task, root.id)) {
      continue;
    }

    const list = pendingByRoot.get(root.id) ?? [];
    list.push({
      task,
      triggerCommentId,
      rootCommentId: root.id,
    });
    pendingByRoot.set(root.id, list);
  }

  for (const list of pendingByRoot.values()) {
    list.sort(comparePendingReplies);
  }
  unplacedTasks.sort(compareTasks);

  return { pendingByRoot, unplacedTasks };
}

function hasLikelyAgentReply(
  comments: readonly TimelineEntry[],
  commentsById: ReadonlyMap<string, TimelineEntry>,
  task: AgentTask,
  rootCommentId: string,
): boolean {
  const taskCreatedAt = new Date(task.created_at).getTime();
  return comments.some((entry) => {
    if (entry.actor_type !== "agent" || entry.actor_id !== task.agent_id) {
      return false;
    }
    const entryCreatedAt = new Date(entry.created_at).getTime();
    if (!Number.isFinite(entryCreatedAt) || entryCreatedAt < taskCreatedAt) {
      return false;
    }
    const root = findThreadRoot(entry, commentsById);
    return root?.id === rootCommentId;
  });
}

function findThreadRoot(
  entry: TimelineEntry,
  commentsById: ReadonlyMap<string, TimelineEntry>,
): TimelineEntry | null {
  let current = entry;
  while (current.parent_id) {
    const parent = commentsById.get(current.parent_id);
    if (!parent) return null;
    current = parent;
  }
  return current;
}

function comparePendingReplies(a: PendingAgentReply, b: PendingAgentReply): number {
  return compareTasks(a.task, b.task);
}

function compareTasks(a: AgentTask, b: AgentTask): number {
  return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
}
