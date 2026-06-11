"use client";

import { useMemo, useState } from "react";
import type { Issue } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  memberListOptions,
  agentListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { useT } from "../../../i18n";

interface WorkloadEntry {
  key: string;
  name: string;
  total: number;
  done: number;
  open: number;
}

export function WorkloadBars({ issues }: { issues: Issue[] }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const [hovered, setHovered] = useState<string | null>(null);

  const nameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const m of members) map.set(`member:${m.user_id}`, m.name);
    for (const a of agents) map.set(`agent:${a.id}`, a.name);
    for (const s of squads) map.set(`squad:${s.id}`, s.name);
    return map;
  }, [members, agents, squads]);

  const data = useMemo(() => {
    const buckets = new Map<string, { total: number; done: number }>();
    for (const issue of issues) {
      const key = issue.assignee_id
        ? `${issue.assignee_type}:${issue.assignee_id}`
        : "__none__";
      const entry = buckets.get(key) ?? { total: 0, done: 0 };
      entry.total++;
      if (issue.status === "done" || issue.status === "cancelled") entry.done++;
      buckets.set(key, entry);
    }

    const entries: WorkloadEntry[] = [];
    for (const [key, val] of buckets) {
      entries.push({
        key,
        name: key === "__none__" ? t(($) => $.swimlane.no_assignee) : (nameMap.get(key) ?? key),
        total: val.total,
        done: val.done,
        open: val.total - val.done,
      });
    }
    entries.sort((a, b) => b.open - a.open);
    return entries;
  }, [issues, nameMap, t]);

  const maxTotal = Math.max(1, ...data.map((d) => d.total));

  return (
    <div className="flex flex-col gap-1.5 overflow-y-auto max-h-[240px]">
      {data.map((d) => {
        const openPct = (d.open / maxTotal) * 100;
        const donePct = (d.done / maxTotal) * 100;
        return (
          <div
            key={d.key}
            className="flex items-center gap-3 transition-opacity duration-150"
            style={{ opacity: hovered && hovered !== d.key ? 0.4 : 1 }}
            onMouseEnter={() => setHovered(d.key)}
            onMouseLeave={() => setHovered(null)}
          >
            <span className="w-24 shrink-0 text-sm text-muted-foreground text-right truncate">
              {d.name}
            </span>
            <div className="flex-1 h-5 flex rounded-sm overflow-hidden bg-muted/30">
              {d.open > 0 && (
                <div
                  className="h-full bg-warning/70 transition-all duration-300"
                  style={{ width: `${Math.max(openPct, 1)}%` }}
                />
              )}
              {d.done > 0 && (
                <div
                  className="h-full bg-info/70 transition-all duration-300"
                  style={{ width: `${Math.max(donePct, 1)}%` }}
                />
              )}
            </div>
            <span className="w-8 shrink-0 text-sm tabular-nums font-medium text-right">
              {d.total}
            </span>
          </div>
        );
      })}
      {data.length > 0 && (
        <div className="flex items-center gap-4 mt-1 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-full bg-warning/70" />
            {t(($) => $.analytics.legend_open)}
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-full bg-info/70" />
            {t(($) => $.analytics.legend_done)}
          </span>
        </div>
      )}
    </div>
  );
}
