"use client";

import { useRef, useMemo, useState, useCallback, useSyncExternalStore } from "react";
import type { Issue, IssueStatus } from "@multica/core/types";

type TimeRange = "14d" | "30d" | "90d";

const RANGE_DAYS: Record<TimeRange, number> = { "14d": 14, "30d": 30, "90d": 90 };

const STATUS_GROUPS = {
  done: ["done", "cancelled"] as IssueStatus[],
  active: ["in_progress", "in_review", "blocked"] as IssueStatus[],
  open: ["backlog", "todo"] as IssueStatus[],
};

const GROUP_COLORS = {
  done: "var(--color-info)",
  active: "var(--color-warning)",
  open: "var(--color-muted-foreground)",
};

const CHART_H = 160;
const PADDING_TOP = 10;
const PADDING_BOTTOM = 24;
const PADDING_RIGHT = 32;
const DRAWABLE_H = CHART_H - PADDING_TOP - PADDING_BOTTOM;

function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function useContainerWidth(ref: React.RefObject<HTMLDivElement | null>) {
  const subscribe = useCallback(
    (cb: () => void) => {
      const el = ref.current;
      if (!el) return () => {};
      const ro = new ResizeObserver(cb);
      ro.observe(el);
      return () => ro.disconnect();
    },
    [ref],
  );
  const getSnapshot = useCallback(
    () => ref.current?.clientWidth ?? 0,
    [ref],
  );
  return useSyncExternalStore(subscribe, getSnapshot, () => 0);
}

export function TrendChart({ issues }: { issues: Issue[] }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const containerW = useContainerWidth(containerRef);
  const [range, setRange] = useState<TimeRange>("30d");

  const days = RANGE_DAYS[range];

  const buckets = useMemo(() => {
    const now = startOfDay(new Date());
    const result: { date: Date; done: number; active: number; open: number }[] = [];

    for (let i = days - 1; i >= 0; i--) {
      const d = new Date(now);
      d.setDate(d.getDate() - i);
      result.push({ date: d, done: 0, active: 0, open: 0 });
    }

    for (const issue of issues) {
      const created = startOfDay(new Date(issue.created_at));
      for (const bucket of result) {
        if (created <= bucket.date) {
          if (STATUS_GROUPS.done.includes(issue.status)) bucket.done++;
          else if (STATUS_GROUPS.active.includes(issue.status)) bucket.active++;
          else bucket.open++;
        }
      }
    }

    return result;
  }, [issues, days]);

  const maxVal = Math.max(1, ...buckets.map((b) => b.done + b.active + b.open));
  const drawableW = Math.max(1, containerW - PADDING_RIGHT);

  const toX = (i: number) =>
    (i / Math.max(1, buckets.length - 1)) * drawableW;

  const toY = (val: number) =>
    PADDING_TOP + DRAWABLE_H - (val / maxVal) * DRAWABLE_H;

  const buildPath = (
    accessor: (b: (typeof buckets)[0]) => number,
    baseline: (b: (typeof buckets)[0]) => number,
  ) => {
    if (buckets.length === 0 || containerW === 0) return "";
    let upper = "";
    let lower = "";
    for (let i = 0; i < buckets.length; i++) {
      const x = toX(i);
      const yTop = toY(accessor(buckets[i]!));
      const yBot = toY(baseline(buckets[i]!));
      upper += `${i === 0 ? "M" : "L"}${x},${yTop}`;
      lower = `L${x},${yBot}` + lower;
    }
    return upper + lower + "Z";
  };

  const openPath = buildPath(
    (b) => b.done + b.active + b.open,
    (b) => b.done + b.active,
  );
  const activePath = buildPath(
    (b) => b.done + b.active,
    (b) => b.done,
  );
  const donePath = buildPath(
    (b) => b.done,
    () => 0,
  );

  const gridLines = useMemo(() => {
    const lines: number[] = [];
    const step = Math.max(1, Math.ceil(maxVal / 4));
    for (let v = step; v <= maxVal; v += step) lines.push(v);
    return lines;
  }, [maxVal]);

  const xLabels = useMemo(() => {
    if (containerW === 0) return [];
    const labels: { x: number; label: string }[] = [];
    const locale = typeof navigator !== "undefined" ? navigator.language : "en";
    const interval = range === "14d" ? 2 : range === "30d" ? 7 : 14;
    for (let i = 0; i < buckets.length; i += interval) {
      labels.push({
        x: toX(i),
        label: buckets[i]!.date.toLocaleDateString(locale, { month: "short", day: "numeric" }),
      });
    }
    return labels;
  }, [buckets, range, containerW]);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-1 self-end">
        {(["14d", "30d", "90d"] as TimeRange[]).map((r) => (
          <button
            key={r}
            onClick={() => setRange(r)}
            className={`px-2 py-0.5 text-xs rounded-md transition-colors ${
              range === r
                ? "bg-accent text-accent-foreground font-medium"
                : "text-muted-foreground hover:bg-accent/50"
            }`}
          >
            {r}
          </button>
        ))}
      </div>

      <div ref={containerRef}>
        {containerW > 0 && (
          <svg width={containerW} height={CHART_H}>
            {gridLines.map((v) => (
              <g key={v}>
                <line
                  x1={0}
                  y1={toY(v)}
                  x2={drawableW}
                  y2={toY(v)}
                  stroke="var(--color-border)"
                  strokeDasharray="4 4"
                />
                <text
                  x={drawableW + 4}
                  y={toY(v) + 3.5}
                  textAnchor="start"
                  className="fill-muted-foreground"
                  style={{ fontSize: 10 }}
                >
                  {v}
                </text>
              </g>
            ))}

            <path d={openPath} fill={GROUP_COLORS.open} opacity={0.25} />
            <path d={activePath} fill={GROUP_COLORS.active} opacity={0.35} />
            <path d={donePath} fill={GROUP_COLORS.done} opacity={0.45} />

            {xLabels.map((l) => (
              <text
                key={l.x}
                x={l.x}
                y={CHART_H - 4}
                textAnchor="middle"
                className="fill-muted-foreground"
                style={{ fontSize: 10 }}
              >
                {l.label}
              </text>
            ))}
          </svg>
        )}
      </div>

      <div className="flex items-center gap-4 text-xs text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: GROUP_COLORS.open }} />
          Open
        </span>
        <span className="flex items-center gap-1.5">
          <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: GROUP_COLORS.active }} />
          Active
        </span>
        <span className="flex items-center gap-1.5">
          <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: GROUP_COLORS.done }} />
          Done
        </span>
      </div>
    </div>
  );
}
