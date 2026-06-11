"use client";

import { useMemo, useState } from "react";
import type { Issue, IssueStatus } from "@multica/core/types";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { useT } from "../../../i18n";

const SIZE = 180;
const STROKE = 28;
const RADIUS = (SIZE - STROKE) / 2;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;
const CENTER = SIZE / 2;

const STATUS_HEX: Record<IssueStatus, string> = {
  backlog: "var(--color-muted-foreground)",
  todo: "var(--color-muted-foreground)",
  in_progress: "var(--color-warning)",
  in_review: "var(--color-success)",
  done: "var(--color-info)",
  blocked: "var(--color-destructive)",
  cancelled: "var(--color-muted-foreground)",
};

export function StatusDonut({ issues }: { issues: Issue[] }) {
  const { t } = useT("issues");
  const [hovered, setHovered] = useState<IssueStatus | null>(null);

  const data = useMemo(() => {
    const counts = new Map<IssueStatus, number>();
    for (const issue of issues) {
      counts.set(issue.status, (counts.get(issue.status) ?? 0) + 1);
    }
    return ALL_STATUSES
      .map((s) => ({ status: s, count: counts.get(s) ?? 0 }))
      .filter((d) => d.count > 0);
  }, [issues]);

  const total = issues.length;

  let offset = 0;
  const arcs = data.map((d) => {
    const fraction = total > 0 ? d.count / total : 0;
    const dash = fraction * CIRCUMFERENCE;
    const gap = CIRCUMFERENCE - dash;
    const arc = { ...d, dashArray: `${dash} ${gap}`, dashOffset: -offset };
    offset += dash;
    return arc;
  });

  return (
    <div className="flex items-center gap-6">
      <svg
        width={SIZE}
        height={SIZE}
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        className="shrink-0"
      >
        {total === 0 ? (
          <circle
            cx={CENTER}
            cy={CENTER}
            r={RADIUS}
            fill="none"
            stroke="var(--color-muted)"
            strokeWidth={STROKE}
          />
        ) : (
          arcs.map((arc) => (
            <circle
              key={arc.status}
              cx={CENTER}
              cy={CENTER}
              r={RADIUS}
              fill="none"
              stroke={STATUS_HEX[arc.status]}
              strokeWidth={STROKE}
              strokeDasharray={arc.dashArray}
              strokeDashoffset={arc.dashOffset}
              strokeLinecap="butt"
              className="transition-opacity duration-150"
              style={{
                transform: "rotate(-90deg)",
                transformOrigin: "center",
                opacity: hovered && hovered !== arc.status ? 0.3 : 1,
              }}
              onMouseEnter={() => setHovered(arc.status)}
              onMouseLeave={() => setHovered(null)}
            />
          ))
        )}
        <text
          x={CENTER}
          y={CENTER}
          textAnchor="middle"
          dominantBaseline="central"
          className="fill-foreground text-2xl font-semibold"
          style={{ fontSize: 28 }}
        >
          {total}
        </text>
      </svg>

      <div className="flex flex-col gap-1.5 min-w-0">
        {data.map((d) => (
          <div
            key={d.status}
            className="flex items-center gap-2 text-sm transition-opacity duration-150"
            style={{ opacity: hovered && hovered !== d.status ? 0.4 : 1 }}
            onMouseEnter={() => setHovered(d.status)}
            onMouseLeave={() => setHovered(null)}
          >
            <span
              className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
              style={{ backgroundColor: STATUS_HEX[d.status] }}
            />
            <span className="text-muted-foreground truncate">
              {t(($) => $.status[d.status])}
            </span>
            <span className="ml-auto tabular-nums font-medium">{d.count}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
