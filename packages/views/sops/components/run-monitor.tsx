"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import {
  sopRunDetailOptions,
  sopRunKeys,
} from "@rimedeck/core/workspace/queries";
import { api } from "@rimedeck/core/api";
import type { SOPNodeExecution, SOPRunStatus } from "@rimedeck/core/types";
import { Button } from "@rimedeck/ui/components/ui/button";
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "@rimedeck/ui/components/ui/progress";
import { Skeleton } from "@rimedeck/ui/components/ui/skeleton";
import { AlertCircle, Ban, ChevronDown, ChevronRight, Loader2 } from "lucide-react";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Status helpers
// ---------------------------------------------------------------------------

const nodeStatusIcon: Record<string, string> = {
  completed: "✅",
  running: "🔄",
  failed: "❌",
  pending: "⬜",
};


function statusColor(status: SOPRunStatus) {
  switch (status) {
    case "completed":
      return "text-emerald-600";
    case "running":
      return "text-blue-500";
    case "failed":
      return "text-destructive";
    case "cancelled":
      return "text-muted-foreground";
    default:
      return "text-muted-foreground";
  }
}

// ---------------------------------------------------------------------------
// Node execution row — expandable to show inputs/outputs
// ---------------------------------------------------------------------------

function NodeExecutionRow({ ne }: { ne: SOPNodeExecution }) {
  const [expanded, setExpanded] = useState(false);
  const { t } = useT("sops");

  const formatData = (data: Record<string, unknown> | null): string => {
    if (!data) return "(none)";
    // If data has a "data" key with a string value, show that directly
    const inner = data.data;
    if (typeof inner === "string") {
      if (inner.length > 500) return inner.slice(0, 500) + "…";
      return inner;
    }
    const s = JSON.stringify(data, null, 2);
    if (s.length > 500) return s.slice(0, 500) + "…";
    return s;
  };

  return (
    <li className="rounded border bg-muted/20">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-2 py-1.5 text-xs hover:bg-muted/50 transition-colors"
        onClick={() => setExpanded((v) => !v)}
      >
        <span>{nodeStatusIcon[ne.status] ?? "⬜"}</span>
        <span className="font-medium">{ne.node_id}</span>
        <span className="text-muted-foreground">{ne.node_type}</span>
        {ne.error && (
          <span className="ml-auto truncate text-destructive max-w-[200px]">{ne.error}</span>
        )}
        {!ne.error && ne.duration_ms > 0 && (
          <span className="ml-auto tabular-nums text-muted-foreground">{t(($) => $.run.duration_ms, { ms: ne.duration_ms })}</span>
        )}
        <span className="text-muted-foreground/50">
          {expanded ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
        </span>
      </button>
      {expanded && (
        <div className="border-t px-2 py-2 space-y-2">
          {ne.inputs && (
            <div>
              <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-0.5">{t(($) => $.run.input)}</p>
              <pre className="rounded bg-muted p-1.5 text-[11px] overflow-x-auto max-h-40 whitespace-pre-wrap break-all">
                {formatData(ne.inputs)}
              </pre>
            </div>
          )}
          {ne.outputs && (
            <div>
              <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-0.5">{t(($) => $.run.output)}</p>
              <pre className="rounded bg-muted p-1.5 text-[11px] overflow-x-auto max-h-40 whitespace-pre-wrap break-all">
                {formatData(ne.outputs)}
              </pre>
            </div>
          )}
          {ne.error && (
            <div>
              <p className="text-[10px] font-semibold text-destructive uppercase tracking-wider mb-0.5">{t(($) => $.run.error)}</p>
              <pre className="rounded bg-destructive/5 p-1.5 text-[11px] text-destructive whitespace-pre-wrap break-all">
                {ne.error}
              </pre>
            </div>
          )}
        </div>
      )}
    </li>
  );
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface RunMonitorProps {
  runId: string;
  sopId: string;
}

export function RunMonitor({ runId, sopId }: RunMonitorProps) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { t } = useT("sops");
  const runStatusLabel = (s: SOPRunStatus) =>
    (t(($) => $.run[s as keyof typeof $.run]) as string) ?? s;

  const isTerminal = (s: SOPRunStatus) =>
    s === "completed" || s === "failed" || s === "cancelled";

  const { data: run, isLoading } = useQuery({
    ...sopRunDetailOptions(wsId, sopId, runId, {
      refetchInterval: 2000,
    }),
    // Stop polling once terminal.
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      if (status && isTerminal(status)) return false;
      return 2000;
    },
  });

  const handleCancel = async () => {
    await api.cancelSOPRun(sopId, runId);
    qc.invalidateQueries({
      queryKey: sopRunKeys.detail(wsId, sopId, runId),
    });
  };

  if (isLoading) {
    return (
      <div className="space-y-2 p-3">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-2 w-full" />
        <Skeleton className="h-4 w-24" />
      </div>
    );
  }

  if (!run) {
    return (
      <div className="flex items-center gap-2 p-3 text-sm text-muted-foreground">
        <AlertCircle className="size-4" />
        {t(($) => $.run.not_found)}
      </div>
    );
  }

  const pct =
    run.total_nodes > 0
      ? Math.round((run.completed_nodes / run.total_nodes) * 100)
      : 0;

  return (
    <div className="space-y-3 p-3 text-sm">
      {/* Header */}
      <div className="flex items-center justify-between">
        <span className={statusColor(run.status)}>
          {runStatusLabel(run.status)}
        </span>
        {run.status === "running" && (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 gap-1 text-xs"
            onClick={handleCancel}
          >
            <Ban className="size-3" />
            {t(($) => $.run.cancel_button)}
          </Button>
        )}
      </div>

      {/* Progress bar */}
      <Progress value={pct}>
        <ProgressLabel>
          {t(($) => $.run.nodes_progress, { completed: run.completed_nodes, total: run.total_nodes })}
        </ProgressLabel>
        <ProgressValue />
      </Progress>

      {/* Node executions list */}
      {run.node_executions && run.node_executions.length > 0 && (
        <ul className="space-y-1.5">
          {run.node_executions.map((ne) => (
            <NodeExecutionRow key={ne.id} ne={ne} />
          ))}
        </ul>
      )}

      {/* Spinner when running but no node executions yet */}
      {run.status === "running" &&
        (!run.node_executions || run.node_executions.length === 0) && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="size-3 animate-spin" />
            {t(($) => $.run.waiting_first)}
          </div>
        )}

      {/* Error */}
      {run.status === "failed" && run.error && (
        <div className="rounded border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
          {run.error}
        </div>
      )}

      {/* Output preview */}
      {run.status === "completed" && run.output && (
        <div className="rounded border bg-muted/30 p-2">
          <p className="mb-1 text-xs font-medium text-muted-foreground">
            {t(($) => $.run.output)}
          </p>
          <pre className="max-h-32 overflow-auto text-xs">
            {JSON.stringify(run.output, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}
