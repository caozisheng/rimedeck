import type { Issue, IssueStatus, TimelineEntry } from "@rimedeck/core/types";

/** A status phase with its associated timeline entries. */
export interface StatusPhase {
  id: string;
  status: IssueStatus;
  actorType: string;
  actorId: string;
  timestamp: string;
  /** ms in this state before next transition */
  duration?: number;
  isRegression?: boolean;
  /** Timeline entries that occurred during this phase (activities + comments) */
  entries: TimelineEntry[];
}

const STATUS_RANK: Record<string, number> = {
  backlog: 0,
  todo: 1,
  in_progress: 2,
  in_review: 3,
  done: 4,
};

/**
 * Build status phases from the issue timeline.
 * Each phase = one status the issue passed through, plus all
 * timeline entries (activities, comments) that occurred in that phase.
 */
export function buildPhases(issue: Issue, timeline: TimelineEntry[]): StatusPhase[] {
  const sorted = [...timeline].sort(
    (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
  );

  // Find initial status from first status_changed.from, or fall back
  const firstChange = sorted.find((e) => e.type === "activity" && e.action === "status_changed");
  const initialStatus = (firstChange?.details?.from as IssueStatus) ?? issue.status;

  const phases: StatusPhase[] = [
    {
      id: "created",
      status: initialStatus,
      actorType: issue.creator_type,
      actorId: issue.creator_id,
      timestamp: issue.created_at,
      entries: [],
    },
  ];

  let currentStatus = initialStatus;

  for (const entry of sorted) {
    if (entry.type === "activity" && entry.action === "status_changed") {
      const to = entry.details?.to as IssueStatus;
      if (!to) continue;
      const fromRank = STATUS_RANK[currentStatus] ?? -1;
      const toRank = STATUS_RANK[to] ?? -1;

      phases.push({
        id: entry.id,
        status: to,
        actorType: entry.actor_type,
        actorId: entry.actor_id,
        timestamp: entry.created_at,
        isRegression: toRank < fromRank && to !== "blocked" && to !== "cancelled",
        entries: [],
      });
      currentStatus = to;
    } else {
      // Attach to the current (last) phase
      const lastPhase = phases[phases.length - 1];
      if (lastPhase) lastPhase.entries.push(entry);
    }
  }

  // Compute durations
  for (let i = 0; i < phases.length - 1; i++) {
    phases[i]!.duration =
      new Date(phases[i + 1]!.timestamp).getTime() - new Date(phases[i]!.timestamp).getTime();
  }

  return phases;
}

export function formatDuration(ms: number): string {
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h`;
  return `${Math.floor(hr / 24)}d`;
}

export const STATUS_LABEL: Record<IssueStatus, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  done: "Done",
  blocked: "Blocked",
  cancelled: "Cancelled",
};
