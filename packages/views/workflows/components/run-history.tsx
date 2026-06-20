"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { workflowRunListOptions } from "@rimedeck/core/workspace/queries";
import type { WorkflowRun, WorkflowRunStatus } from "@rimedeck/core/types";
import { Badge } from "@rimedeck/ui/components/ui/badge";
import { Skeleton } from "@rimedeck/ui/components/ui/skeleton";
import { useTimeAgo, useT } from "../../i18n";
import { RunMonitor } from "./run-monitor";
import {
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  Ban,
} from "lucide-react";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function statusBadgeVariant(
  status: WorkflowRunStatus,
): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "completed":
      return "default";
    case "running":
      return "secondary";
    case "failed":
      return "destructive";
    default:
      return "outline";
  }
}

function StatusIcon({ status }: { status: WorkflowRunStatus }) {
  switch (status) {
    case "completed":
      return <CheckCircle2 className="size-3.5 text-emerald-600" />;
    case "running":
      return <Loader2 className="size-3.5 animate-spin text-blue-500" />;
    case "failed":
      return <XCircle className="size-3.5 text-destructive" />;
    case "cancelled":
      return <Ban className="size-3.5 text-muted-foreground" />;
    default:
      return <Clock className="size-3.5 text-muted-foreground" />;
  }
}

function formatDuration(run: WorkflowRun): string | null {
  if (!run.started_at) return null;
  const start = new Date(run.started_at).getTime();
  const end = run.completed_at
    ? new Date(run.completed_at).getTime()
    : Date.now();
  const ms = end - start;
  if (ms < 1000) return `${ms}ms`;
  const secs = Math.round(ms / 1000);
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  const remSecs = secs % 60;
  return `${mins}m ${remSecs}s`;
}

// ---------------------------------------------------------------------------
// Row
// ---------------------------------------------------------------------------

function RunRow({
  run,
  workflowId,
  expanded,
  onToggle,
}: {
  run: WorkflowRun;
  workflowId: string;
  expanded: boolean;
  onToggle: () => void;
}) {
  const timeAgo = useTimeAgo();
  const { t } = useT("workflows");
  const duration = formatDuration(run);
  const statusText = (t(($) => $.run[run.status as keyof typeof $.run]) as string) ?? run.status;

  return (
    <div className="border-b last:border-b-0">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted/50 transition-colors"
      >
        {expanded ? (
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        <StatusIcon status={run.status} />
        <Badge variant={statusBadgeVariant(run.status)} className="text-[10px] px-1.5 py-0">
          {statusText}
        </Badge>
        {duration && (
          <span className="tabular-nums text-xs text-muted-foreground">
            {duration}
          </span>
        )}
        <span className="ml-auto text-xs text-muted-foreground">
          {run.source !== "manual" && (
            <span className="mr-2 text-muted-foreground/70">{run.source}</span>
          )}
          {timeAgo(run.created_at)}
        </span>
      </button>
      {expanded && <RunMonitor runId={run.id} workflowId={workflowId} />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

interface RunHistoryProps {
  workflowId: string;
}

export function RunHistory({ workflowId }: RunHistoryProps) {
  const wsId = useWorkspaceId();
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const { t } = useT("workflows");

  const { data: runs, isLoading } = useQuery(
    workflowRunListOptions(wsId, workflowId),
  );

  if (isLoading) {
    return (
      <div className="space-y-2 p-3">
        {Array.from({ length: 3 }, (_, i) => (
          <Skeleton key={i} className="h-8 w-full" />
        ))}
      </div>
    );
  }

  if (!runs || runs.length === 0) {
    return (
      <div
        className="px-3 py-6 text-center text-sm text-muted-foreground"
        dangerouslySetInnerHTML={{ __html: t(($) => $.run.no_runs_hint) }}
      />
    );
  }

  return (
    <div className="divide-y">
      {runs.map((run) => (
        <RunRow
          key={run.id}
          run={run}
          workflowId={workflowId}
          expanded={expandedId === run.id}
          onToggle={() =>
            setExpandedId((prev) => (prev === run.id ? null : run.id))
          }
        />
      ))}
    </div>
  );
}
