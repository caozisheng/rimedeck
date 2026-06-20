"use client";

import { useMemo, useState } from "react";
import type { Issue, IssuePriority } from "@rimedeck/core/types";
import { PRIORITY_ORDER } from "@rimedeck/core/issues/config";
import { useT } from "../../../i18n";

const PRIORITY_HEX: Record<IssuePriority, string> = {
  urgent: "var(--color-destructive)",
  high: "var(--color-warning)",
  medium: "var(--color-warning)",
  low: "var(--color-info)",
  none: "var(--color-muted-foreground)",
};

export function PriorityBars({ issues }: { issues: Issue[] }) {
  const { t } = useT("issues");
  const [hovered, setHovered] = useState<IssuePriority | null>(null);

  const data = useMemo(() => {
    const counts = new Map<IssuePriority, number>();
    for (const issue of issues) {
      counts.set(issue.priority, (counts.get(issue.priority) ?? 0) + 1);
    }
    return PRIORITY_ORDER.map((p) => ({ priority: p, count: counts.get(p) ?? 0 }));
  }, [issues]);

  const maxCount = Math.max(1, ...data.map((d) => d.count));

  return (
    <div className="flex flex-col gap-1">
      {data.map((d) => {
        const widthPct = (d.count / maxCount) * 100;
        return (
          <div
            key={d.priority}
            className="flex items-center gap-3 transition-opacity duration-150"
            style={{ opacity: hovered && hovered !== d.priority ? 0.4 : 1 }}
            onMouseEnter={() => setHovered(d.priority)}
            onMouseLeave={() => setHovered(null)}
          >
            <span className="w-20 shrink-0 text-sm text-muted-foreground text-right truncate">
              {t(($) => $.priority[d.priority])}
            </span>
            <div className="flex-1 h-6 relative">
              <div
                className="absolute inset-y-0 left-0 rounded-sm transition-all duration-300"
                style={{
                  width: d.count > 0 ? `${Math.max(widthPct, 2)}%` : "0%",
                  backgroundColor: PRIORITY_HEX[d.priority],
                  opacity: 0.8,
                }}
              />
            </div>
            <span className="w-8 shrink-0 text-sm tabular-nums font-medium text-right">
              {d.count}
            </span>
          </div>
        );
      })}
    </div>
  );
}
