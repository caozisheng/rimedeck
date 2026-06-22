import type { AgentTask, TimelineEntry } from "@rimedeck/core/types";

const COMMENT_TASK_WINDOW_MS = 10 * 60 * 1000;

export function buildCommentTaskLinks(
  timeline: TimelineEntry[],
  tasks: AgentTask[] = [],
): Map<string, string> {
  const pendingTaskByAgent = new Map<string, TimelineEntry>();
  const latestCommentByAgent = new Map<string, TimelineEntry>();
  const links = new Map<string, string>();

  for (const entry of timeline) {
    const actorKey = `${entry.actor_type}:${entry.actor_id}`;
    if (entry.type === "activity" && isTaskActivity(entry.action)) {
      const taskId = taskIdFromDetails(entry.details);
      if (taskId && entry.actor_type === "agent") {
        const recentComment = latestCommentByAgent.get(actorKey);
        if (recentComment && isWithinWindow(recentComment, entry)) {
          links.set(recentComment.id, taskId);
        } else {
          pendingTaskByAgent.set(actorKey, entry);
        }
      }
      continue;
    }

    if (entry.type !== "comment" || entry.actor_type !== "agent") continue;
    latestCommentByAgent.set(actorKey, entry);
    const pendingTask = pendingTaskByAgent.get(actorKey);
    const taskId = taskIdFromDetails(pendingTask?.details);
    if (pendingTask && taskId && isWithinWindow(pendingTask, entry)) {
      links.set(entry.id, taskId);
      pendingTaskByAgent.delete(actorKey);
    }
  }

  linkCommentsFromTasks(links, timeline, tasks);
  return links;
}

function isTaskActivity(action: string | undefined): boolean {
  return action === "task_completed" || action === "task_failed";
}

function taskIdFromDetails(details: Record<string, unknown> | undefined): string | null {
  const value = details?.task_id;
  return typeof value === "string" && value.length > 0 ? value : null;
}

function isWithinWindow(a: TimelineEntry, b: TimelineEntry): boolean {
  const diff = Math.abs(new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
  return Number.isFinite(diff) && diff <= COMMENT_TASK_WINDOW_MS;
}

function linkCommentsFromTasks(
  links: Map<string, string>,
  timeline: TimelineEntry[],
  tasks: AgentTask[],
) {
  const agentComments = timeline.filter(
    (entry) => entry.type === "comment" && entry.actor_type === "agent",
  );
  const terminalTasks = tasks
    .filter((task) =>
      (task.status === "completed" || task.status === "failed") &&
      !!task.completed_at,
    )
    .toSorted(
      (a, b) =>
        new Date(b.completed_at ?? b.created_at).getTime() -
        new Date(a.completed_at ?? a.created_at).getTime(),
    );

  for (const comment of agentComments) {
    if (links.has(comment.id)) continue;
    const match = terminalTasks.find((task) =>
      task.agent_id === comment.actor_id &&
      isWithinTaskWindow(task, comment) &&
      isAfterTaskCompletion(task, comment),
    );
    if (match) links.set(comment.id, match.id);
  }
}

function isWithinTaskWindow(task: AgentTask, comment: TimelineEntry): boolean {
  const taskAt = new Date(task.completed_at ?? task.created_at).getTime();
  const commentAt = new Date(comment.created_at).getTime();
  const diff = Math.abs(commentAt - taskAt);
  return Number.isFinite(diff) && diff <= COMMENT_TASK_WINDOW_MS;
}

function isAfterTaskCompletion(task: AgentTask, comment: TimelineEntry): boolean {
  const completedAt = new Date(task.completed_at ?? task.created_at).getTime();
  const commentAt = new Date(comment.created_at).getTime();
  return Number.isFinite(completedAt) && Number.isFinite(commentAt) && commentAt >= completedAt;
}
