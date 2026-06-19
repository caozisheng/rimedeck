"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { workflowStatsOptions } from "@multica/core/workspace/queries";
import { useState } from "react";
import { ChevronDown, ChevronRight, BarChart3 } from "lucide-react";
import { useT } from "../../i18n";

interface WorkflowStatsProps {
  workflowId: string;
}

export function WorkflowStats({ workflowId }: WorkflowStatsProps) {
  const wsId = useWorkspaceId();
  const [expanded, setExpanded] = useState(false);
  const { t } = useT("workflows");

  const { data: stats } = useQuery(workflowStatsOptions(wsId, workflowId));

  if (!stats || stats.total_runs === 0) return null;

  const successRate =
    stats.total_runs > 0
      ? ((stats.completed_runs / stats.total_runs) * 100).toFixed(1)
      : "0";

  const avgDuration = formatDuration(stats.avg_duration_ms);

  let lastRun = "—";
  if (stats.last_run_at) {
    const diff = Date.now() - new Date(stats.last_run_at).getTime();
    const mins = Math.floor(diff / 60_000);
    if (mins < 1) lastRun = t(($) => $.stats.just_now);
    else if (mins < 60) lastRun = t(($) => $.stats.minutes_ago, { count: mins });
    else {
      const hrs = Math.floor(mins / 60);
      if (hrs < 24) lastRun = t(($) => $.stats.hours_ago, { count: hrs });
      else lastRun = t(($) => $.stats.days_ago, { count: Math.floor(hrs / 24) });
    }
  }

  return (
    <div className="border-b">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-4 py-2 text-xs text-muted-foreground hover:bg-muted/50 transition-colors"
        onClick={() => setExpanded((v) => !v)}
      >
        {expanded ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        <BarChart3 className="h-3 w-3" />
        <span>{t(($) => $.stats.title)}</span>
        <span className="ml-auto tabular-nums">{t(($) => $.stats.runs_count, { count: stats.total_runs })}</span>
      </button>

      {expanded && (
        <div className="grid grid-cols-5 gap-4 px-4 pb-3">
          <StatCard label={t(($) => $.stats.total_runs)} value={String(stats.total_runs)} />
          <StatCard label={t(($) => $.stats.success_rate)} value={`${successRate}%`} />
          <StatCard label={t(($) => $.stats.avg_duration)} value={avgDuration} />
          <StatCard label={t(($) => $.stats.total_tokens)} value={formatTokens(stats.total_tokens)} />
          <StatCard label={t(($) => $.stats.last_run)} value={lastRun} />
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] text-muted-foreground">{label}</span>
      <span className="text-sm font-medium tabular-nums">{value}</span>
    </div>
  );
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60_000).toFixed(1)}m`;
}

function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}
