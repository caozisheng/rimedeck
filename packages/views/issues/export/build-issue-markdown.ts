import type { Issue, TimelineEntry, AgentTask } from "@rimedeck/core/types";
import type { TimelineItem } from "../../common/task-transcript/build-timeline";
import { getToolDisplayName } from "../../common/task-transcript/tool-labels";

export interface ExportContext {
  issue: Issue;
  timeline: TimelineEntry[];
  taskTranscripts: Map<string, { task: AgentTask; items: TimelineItem[] }>;
  getActorName: (type: string, id: string) => string;
  formatActivityEntry: (entry: TimelineEntry) => string;
}

export function buildIssueMarkdown(ctx: ExportContext): string {
  const { issue, timeline, taskTranscripts, getActorName, formatActivityEntry } = ctx;
  const lines: string[] = [];

  lines.push(`# ${issue.identifier}: ${issue.title}`);
  lines.push("");

  // Metadata table
  lines.push("| Field | Value |");
  lines.push("|-------|-------|");
  lines.push(`| Status | ${issue.status} |`);
  lines.push(`| Priority | ${issue.priority} |`);
  if (issue.assignee_type && issue.assignee_id) {
    lines.push(`| Assignee | ${getActorName(issue.assignee_type, issue.assignee_id)} |`);
  }
  lines.push(`| Creator | ${getActorName(issue.creator_type, issue.creator_id)} |`);
  if (issue.start_date) {
    lines.push(`| Start Date | ${issue.start_date} |`);
  }
  if (issue.due_date) {
    lines.push(`| Due Date | ${issue.due_date} |`);
  }
  lines.push(`| Created | ${formatDateTime(issue.created_at)} |`);
  lines.push(`| Updated | ${formatDateTime(issue.updated_at)} |`);
  lines.push("");

  // Labels
  if (issue.labels && issue.labels.length > 0) {
    lines.push("## Labels");
    lines.push("");
    lines.push(issue.labels.map((l) => `\`${l.name}\``).join(", "));
    lines.push("");
  }

  // Description
  if (issue.description) {
    lines.push("## Description");
    lines.push("");
    lines.push(issue.description);
    lines.push("");
  }

  // Timeline
  if (timeline.length === 0) return lines.join("\n");

  lines.push("---");
  lines.push("");
  lines.push("## Timeline");
  lines.push("");

  const sorted = [...timeline].sort(
    (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
  );

  // Build a set of task IDs referenced by task_completed/task_failed activities
  // so we can attach transcripts to the right timeline entry.
  const usedTranscriptIds = new Set<string>();

  for (const entry of sorted) {
    const dateStr = formatDateTime(entry.created_at);
    const actor = getActorName(entry.actor_type, entry.actor_id);

    if (entry.type === "comment") {
      if (entry.comment_type === "system") {
        lines.push(`### ${dateStr} — System`);
        lines.push("");
        if (entry.content) lines.push(entry.content);
      } else {
        const resolvedTag = entry.resolved_at ? " *(Resolved)*" : "";
        lines.push(`### ${dateStr} — ${actor} commented${resolvedTag}`);
        lines.push("");
        if (entry.content) {
          for (const line of entry.content.split("\n")) {
            lines.push(`> ${line}`);
          }
        }
      }

      // Reactions
      if (entry.reactions && entry.reactions.length > 0) {
        const counts = new Map<string, number>();
        for (const r of entry.reactions) {
          counts.set(r.emoji, (counts.get(r.emoji) ?? 0) + 1);
        }
        const parts: string[] = [];
        for (const [emoji, count] of counts) {
          parts.push(`${emoji}×${count}`);
        }
        lines.push("");
        lines.push(`Reactions: ${parts.join(" ")}`);
      }

      lines.push("");
    } else {
      // Activity
      const desc = formatActivityEntry(entry);
      lines.push(`### ${dateStr} — ${actor} ${desc}`);
      lines.push("");

      // Attach agent transcript for task_completed/task_failed
      if (
        (entry.action === "task_completed" || entry.action === "task_failed") &&
        taskTranscripts.size > 0
      ) {
        const transcript = findMatchingTranscript(
          entry,
          taskTranscripts,
          usedTranscriptIds,
        );
        if (transcript) {
          usedTranscriptIds.add(transcript.task.id);
          lines.push(renderTranscript(transcript.task, transcript.items));
          lines.push("");
        }
      }
    }
  }

  return lines.join("\n");
}

function findMatchingTranscript(
  entry: TimelineEntry,
  transcripts: Map<string, { task: AgentTask; items: TimelineItem[] }>,
  usedIds: Set<string>,
): { task: AgentTask; items: TimelineItem[] } | undefined {
  // Match by closest completed_at time to the activity created_at
  const entryTime = new Date(entry.created_at).getTime();
  let best: { task: AgentTask; items: TimelineItem[] } | undefined;
  let bestDiff = Infinity;

  for (const [id, t] of transcripts) {
    if (usedIds.has(id)) continue;
    const completedAt = t.task.completed_at;
    if (!completedAt) continue;
    const diff = Math.abs(new Date(completedAt).getTime() - entryTime);
    if (diff < bestDiff) {
      bestDiff = diff;
      best = t;
    }
  }

  // Only match if within 60 seconds
  if (best && bestDiff < 60_000) return best;
  return undefined;
}

function renderTranscript(task: AgentTask, items: TimelineItem[]): string {
  const duration = task.started_at && task.completed_at
    ? formatDuration(task.started_at, task.completed_at)
    : null;

  const durationStr = duration ? `, ${duration}` : "";
  const lines: string[] = [];

  lines.push(`<details>`);
  lines.push(`<summary>Agent transcript (${items.length} events${durationStr})</summary>`);
  lines.push("");

  for (const item of items) {
    const label = getEventLabel(item);
    switch (item.type) {
      case "text":
        lines.push(`**[${item.seq}] ${label}**`);
        if (item.content) {
          lines.push(item.content);
        }
        break;
      case "thinking":
        lines.push(`**[${item.seq}] ${label}**`);
        if (item.content) {
          lines.push(item.content);
        }
        break;
      case "tool_use":
        lines.push(`**[${item.seq}] Tool: ${getToolDisplayName(item.tool) ?? "unknown"}**`);
        if (item.input) {
          lines.push("```json");
          lines.push(JSON.stringify(item.input, null, 2));
          lines.push("```");
        }
        break;
      case "tool_result":
        lines.push(`**[${item.seq}] ${label} result**`);
        if (item.output) {
          const truncated = item.output.length > 2000
            ? item.output.slice(0, 2000) + "\n... (truncated)"
            : item.output;
          lines.push("```");
          lines.push(truncated);
          lines.push("```");
        }
        break;
      case "error":
        lines.push(`**[${item.seq}] Error**`);
        if (item.content) {
          lines.push(`\`\`\`\n${item.content}\n\`\`\``);
        }
        break;
    }
    lines.push("");
  }

  lines.push("</details>");
  return lines.join("\n");
}

function getEventLabel(item: TimelineItem): string {
  switch (item.type) {
    case "text":
      return "Agent";
    case "thinking":
      return "Thinking";
    case "tool_use":
      return getToolDisplayName(item.tool) ?? "Tool";
    case "tool_result":
      return getToolDisplayName(item.tool) ?? "Result";
    case "error":
      return "Error";
    default:
      return "Event";
  }
}

function formatDateTime(dateStr: string): string {
  const d = new Date(dateStr);
  const year = d.getFullYear();
  const month = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  const hours = String(d.getHours()).padStart(2, "0");
  const minutes = String(d.getMinutes()).padStart(2, "0");
  return `${year}-${month}-${day} ${hours}:${minutes}`;
}

function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime();
  const totalSeconds = Math.floor(ms / 1000);
  const m = Math.floor(totalSeconds / 60);
  const s = totalSeconds % 60;
  if (m === 0) return `${s}s`;
  return `${m}m ${s}s`;
}
