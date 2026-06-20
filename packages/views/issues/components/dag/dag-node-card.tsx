"use client";

import { memo } from "react";
import { ChevronRight, ChevronDown } from "lucide-react";
import type { Issue, IssueStatus } from "@rimedeck/core/types";
import { useActorName } from "@rimedeck/core/workspace/hooks";
import { StatusIcon } from "../status-icon";
import { PriorityIcon } from "../priority-icon";
import type { DagNode } from "./use-dagre-layout";
import { useT } from "../../../i18n";

interface DagNodeCardProps {
  node: DagNode;
  issue: Issue;
  highlighted: boolean;
  dimmed: boolean;
  expandable: boolean;
  expanded: boolean;
  onToggleExpand?: (issueId: string) => void;
  onClick?: (issueId: string) => void;
  onMouseEnter?: (issueId: string) => void;
  onMouseLeave?: () => void;
}

const STATUS_BORDER_COLORS: Record<IssueStatus, string> = {
  backlog: "border-muted-foreground/30",
  todo: "border-muted-foreground/50",
  in_progress: "border-blue-500",
  in_review: "border-yellow-500",
  done: "border-green-500",
  blocked: "border-destructive",
  cancelled: "border-muted-foreground/20",
};

function AssigneeName({ type, id }: { type: string; id: string }) {
  const { getActorName } = useActorName();
  const name = getActorName(type, id);
  const icon = type === "agent" ? "🤖" : type === "squad" ? "👥" : "👤";
  return (
    <span className="truncate text-[10px]">
      {icon} {name ?? id.slice(0, 8)}
    </span>
  );
}

export const DagNodeCard = memo(function DagNodeCard({
  node,
  issue,
  highlighted,
  dimmed,
  expandable,
  expanded,
  onToggleExpand,
  onClick,
  onMouseEnter,
  onMouseLeave,
}: DagNodeCardProps) {
  const { t } = useT("issues");
  const borderColor = STATUS_BORDER_COLORS[issue.status] ?? "border-border";

  return (
    <foreignObject x={node.x} y={node.y} width={node.width} height={node.height}>
      <div
        className={`
          dag-node-card
          h-full rounded-lg border-2 bg-card p-2.5 text-xs cursor-pointer
          transition-all duration-150 select-none
          ${borderColor}
          ${highlighted ? "ring-2 ring-primary shadow-md" : ""}
          ${dimmed ? "opacity-20" : ""}
          hover:shadow-md
        `}
        onClick={(e) => { e.stopPropagation(); onClick?.(issue.id); }}
        onPointerDown={(e) => e.stopPropagation()}
        onMouseEnter={() => onMouseEnter?.(issue.id)}
        onMouseLeave={onMouseLeave}
      >
        <div className="flex h-full">
          {/* Main content */}
          <div className="flex-1 min-w-0">
            {/* Row 1: status icon + identifier + title */}
            <div className="flex items-center gap-1.5 mb-1">
              <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
              <span className="text-muted-foreground font-mono shrink-0">{issue.identifier}</span>
              <span className="font-medium truncate">{issue.title}</span>
            </div>

            {/* Row 2: priority + assignee */}
            <div className="flex items-center gap-2 text-muted-foreground">
              <PriorityIcon priority={issue.priority} className="size-3" />
              {issue.assignee_type && issue.assignee_id && (
                <AssigneeName type={issue.assignee_type} id={issue.assignee_id} />
              )}
            </div>

            {/* Blocked indicator */}
            {issue.status === "blocked" && (
              <div className="mt-1 text-[10px] text-destructive font-medium">
                {t(($) => $.dag_view.blocked)}
              </div>
            )}
          </div>

          {/* Expand/collapse toggle */}
          {expandable && (
            <button
              className="flex items-center justify-center w-6 shrink-0 -mr-1 rounded hover:bg-accent transition-colors"
              onClick={(e) => {
                e.stopPropagation();
                onToggleExpand?.(issue.id);
              }}
              onPointerDown={(e) => e.stopPropagation()}
            >
              {expanded ? (
                <ChevronDown className="size-4 text-muted-foreground" />
              ) : (
                <ChevronRight className="size-4 text-muted-foreground" />
              )}
            </button>
          )}
        </div>
      </div>
    </foreignObject>
  );
});
