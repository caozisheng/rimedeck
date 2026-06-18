"use client";

import { useMemo, useState } from "react";
import { ChevronDown, ChevronRight, MessageSquare } from "lucide-react";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { StatusIcon } from "../status-icon";
import { buildPhases, formatDuration, STATUS_LABEL, type StatusPhase } from "./build-sequence";

// ---------------------------------------------------------------------------
// Format helpers
// ---------------------------------------------------------------------------

function shortTime(iso: string): string {
  const d = new Date(iso);
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

function activityLabel(entry: TimelineEntry): string {
  if (entry.type === "comment") return "💬 评论";
  const action = entry.action ?? "";
  const details = (entry.details ?? {}) as Record<string, string>;
  switch (action) {
    case "assignee_changed": return "指派变更";
    case "priority_changed": return `优先级 ${details.from ?? "?"} → ${details.to ?? "?"}`;
    case "description_updated": return "更新描述";
    case "task_completed": return "✅ Agent 完成";
    case "task_failed": return "❌ Agent 失败";
    case "squad_leader_evaluated": return "Squad 评估";
    case "start_date_changed": return "开始日期变更";
    case "due_date_changed": return "截止日期变更";
    default: return action.replace(/_/g, " ");
  }
}

// ---------------------------------------------------------------------------
// Activity entry row (right side, inside expanded phase)
// ---------------------------------------------------------------------------

function EntryRow({ entry, getActorName }: { entry: TimelineEntry; getActorName: (type: string, id: string) => string | null }) {
  const name = getActorName(entry.actor_type, entry.actor_id) ?? entry.actor_id.slice(0, 8);
  const isComment = entry.type === "comment";

  return (
    <div className={`flex items-start gap-2 py-1 px-2 text-[11px] rounded ${isComment ? "bg-muted/30" : ""}`}>
      <span className="text-muted-foreground tabular-nums shrink-0 pt-0.5">{shortTime(entry.created_at)}</span>
      <div className="flex-1 min-w-0">
        <span className="font-medium">{name}</span>
        <span className="text-muted-foreground ml-1.5">{activityLabel(entry)}</span>
        {isComment && entry.content && (
          <p className="text-muted-foreground mt-0.5 truncate">{entry.content.slice(0, 80)}{entry.content.length > 80 ? "…" : ""}</p>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Single phase row (left status node + right activity list)
// ---------------------------------------------------------------------------

function PhaseRow({
  phase,
  isCurrent,
  isLast,
  nextPhase,
}: {
  phase: StatusPhase;
  isCurrent: boolean;
  isLast: boolean;
  nextPhase?: StatusPhase;
}) {
  const [expanded, setExpanded] = useState(isCurrent);
  const { getActorName } = useActorName();
  const actorName = getActorName(phase.actorType, phase.actorId) ?? phase.actorId.slice(0, 8);

  const hasEntries = phase.entries.length > 0;
  const commentCount = phase.entries.filter((e) => e.type === "comment").length;
  const activityCount = phase.entries.filter((e) => e.type === "activity").length;

  const isNextBlocked = nextPhase?.status === "blocked";
  const isRecovery = phase.status === "blocked" && nextPhase != null && nextPhase.status !== "blocked";

  return (
    <div className="flex gap-0">
      {/* ── Left rail: status node + vertical connector with duration ── */}
      <div className="flex flex-col items-center w-10 shrink-0">
        {/* Status dot */}
        <div
          className={`
            flex items-center justify-center size-8 rounded-full border-2 bg-card z-10
            ${isCurrent ? "border-primary ring-2 ring-primary/30" : "border-muted-foreground/30"}
          `}
        >
          <StatusIcon status={phase.status} className="size-4" />
        </div>
        {/* Vertical line + duration label */}
        {!isLast && (
          <div className="flex items-start w-full flex-1">
            {/* Line */}
            <div className="flex flex-col items-center flex-1">
              <div
                className={`
                  w-0 flex-1 min-h-4 border-l-2
                  ${isNextBlocked ? "border-destructive border-dashed" : isRecovery ? "border-green-500" : "border-muted-foreground/20"}
                `}
              />
            </div>
            {/* Duration + arrow alongside the line */}
            {phase.duration != null && phase.duration > 0 && (
              <div className="flex flex-col items-center justify-center -ml-5 mt-1 pointer-events-none select-none">
                <span className="text-[9px] text-muted-foreground/50 tabular-nums bg-card px-0.5">
                  {formatDuration(phase.duration)}
                </span>
                <span className="text-[9px] text-muted-foreground/30">↓</span>
              </div>
            )}
          </div>
        )}
      </div>

      {/* ── Right content: phase info + expandable entries ── */}
      <div className={`flex-1 min-w-0 pb-4 ${isLast ? "" : ""}`}>
        {/* Phase header */}
        <button
          type="button"
          className={`
            flex items-center gap-2 w-full text-left rounded-md px-2 py-1 -mt-1 transition-colors
            ${isCurrent ? "bg-primary/5" : "hover:bg-accent/30"}
          `}
          onClick={() => hasEntries && setExpanded((v) => !v)}
          disabled={!hasEntries}
        >
          <span className="text-xs font-medium">{STATUS_LABEL[phase.status] ?? phase.status}</span>

          {phase.isRegression && (
            <span className="text-[10px] text-yellow-600 font-medium rounded bg-yellow-500/10 px-1">回退</span>
          )}

          <span className="text-[10px] text-muted-foreground">
            {phase.actorType === "agent" ? "🤖" : "👤"} {actorName}
          </span>

          <span className="text-[10px] text-muted-foreground/60 tabular-nums ml-auto shrink-0">
            {shortTime(phase.timestamp)}
            {phase.duration != null && phase.duration > 0 && (
              <> · {formatDuration(phase.duration)}</>
            )}
          </span>

          {/* Entry count badges */}
          {hasEntries && (
            <div className="flex items-center gap-1 shrink-0">
              {commentCount > 0 && (
                <span className="inline-flex items-center gap-0.5 text-[10px] text-muted-foreground">
                  <MessageSquare className="size-2.5" />{commentCount}
                </span>
              )}
              {activityCount > 0 && (
                <span className="text-[10px] text-muted-foreground">{activityCount}条</span>
              )}
              {expanded ? <ChevronDown className="size-3 text-muted-foreground" /> : <ChevronRight className="size-3 text-muted-foreground" />}
            </div>
          )}
        </button>

        {/* Expanded entries */}
        {expanded && hasEntries && (
          <div className="ml-2 mt-1 space-y-0.5 border-l-2 border-muted-foreground/10 pl-2">
            {phase.entries.map((entry) => (
              <EntryRow key={entry.id} entry={entry} getActorName={getActorName} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

interface IssueSequenceProps {
  issue: Issue;
  timeline: TimelineEntry[];
}

export function IssueSequence({ issue, timeline }: IssueSequenceProps) {
  const phases = useMemo(() => buildPhases(issue, timeline), [issue, timeline]);

  if (phases.length === 0) return null;

  return (
    <div className="rounded-lg border bg-card/50 p-3">
      {phases.map((phase, i) => (
        <PhaseRow
          key={phase.id}
          phase={phase}
          isCurrent={i === phases.length - 1}
          isLast={i === phases.length - 1}
          nextPhase={phases[i + 1]}
        />
      ))}
    </div>
  );
}
