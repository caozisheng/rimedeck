"use client";

import { useMemo, useState, useCallback } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import type { Issue, IssueStatus } from "@multica/core/types";
import { StatusIcon } from "./status-icon";
import { useT } from "../../i18n";

const STATUS_DOT_COLOR: Record<IssueStatus, string> = {
  backlog: "bg-muted-foreground",
  todo: "bg-muted-foreground",
  in_progress: "bg-warning",
  in_review: "bg-success",
  done: "bg-info",
  blocked: "bg-destructive",
  cancelled: "bg-muted-foreground/50",
};

const WEEKDAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"] as const;

function issueDate(issue: Issue): Date | null {
  const raw = issue.due_date ?? issue.start_date;
  if (!raw) return null;
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return null;
  return d;
}

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

function dateKey(d: Date): string {
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

interface DayCell {
  date: Date;
  inMonth: boolean;
  isToday: boolean;
  isWeekend: boolean;
}

function buildGrid(year: number, month: number, today: Date): DayCell[] {
  const firstOfMonth = new Date(year, month, 1);
  let startDow = firstOfMonth.getDay() - 1;
  if (startDow < 0) startDow = 6;

  const gridStart = new Date(year, month, 1 - startDow);

  const cells: DayCell[] = [];
  const d = new Date(gridStart);
  const totalCells = 42;
  for (let i = 0; i < totalCells; i++) {
    const wd = d.getDay();
    cells.push({
      date: new Date(d),
      inMonth: d.getMonth() === month,
      isToday: sameDay(d, today),
      isWeekend: wd === 0 || wd === 6,
    });
    d.setDate(d.getDate() + 1);
  }

  const lastUsedRow = Math.ceil(
    cells.findLastIndex((c) => c.inMonth) / 7,
  );
  return cells.slice(0, (lastUsedRow + 1) * 7);
}

export function CalendarView({ issues }: { issues: Issue[] }) {
  const { t } = useT("issues");
  const today = useMemo(() => new Date(), []);

  const [year, setYear] = useState(today.getFullYear());
  const [month, setMonth] = useState(today.getMonth());
  const [selectedDay, setSelectedDay] = useState<string | null>(null);

  const goToday = useCallback(() => {
    setYear(today.getFullYear());
    setMonth(today.getMonth());
  }, [today]);

  const prevMonth = useCallback(() => {
    setMonth((m) => {
      if (m === 0) {
        setYear((y) => y - 1);
        return 11;
      }
      return m - 1;
    });
  }, []);

  const nextMonth = useCallback(() => {
    setMonth((m) => {
      if (m === 11) {
        setYear((y) => y + 1);
        return 0;
      }
      return m + 1;
    });
  }, []);

  const grid = useMemo(() => buildGrid(year, month, today), [year, month, today]);

  const issuesByDay = useMemo(() => {
    const map = new Map<string, Issue[]>();
    for (const issue of issues) {
      const d = issueDate(issue);
      if (!d) continue;
      const key = dateKey(d);
      const list = map.get(key);
      if (list) list.push(issue);
      else map.set(key, [issue]);
    }
    return map;
  }, [issues]);

  const hasAnyDates = issuesByDay.size > 0;

  const locale = typeof navigator !== "undefined" ? navigator.language : "en";
  const monthLabel = new Date(year, month).toLocaleDateString(locale, {
    month: "long",
    year: "numeric",
  });

  const selectedIssues = useMemo(() => {
    if (!selectedDay) return [];
    return issuesByDay.get(selectedDay) ?? [];
  }, [selectedDay, issuesByDay]);

  const weekdayKey = (wd: typeof WEEKDAYS[number]) =>
    t(($) => $.calendar[`wd_${wd}` as keyof typeof $.calendar]);

  return (
    <div className="flex-1 min-h-0 overflow-y-auto p-4">
      <div className="mx-auto max-w-3xl">
        {/* Navigation */}
        <div className="mb-4 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <button
              onClick={prevMonth}
              className="rounded-md p-1 hover:bg-accent text-muted-foreground"
            >
              <ChevronLeft className="size-4" />
            </button>
            <h2 className="text-sm font-medium min-w-[140px] text-center">
              {monthLabel}
            </h2>
            <button
              onClick={nextMonth}
              className="rounded-md p-1 hover:bg-accent text-muted-foreground"
            >
              <ChevronRight className="size-4" />
            </button>
          </div>
          <button
            onClick={goToday}
            className="rounded-md px-2.5 py-1 text-xs text-muted-foreground hover:bg-accent"
          >
            {t(($) => $.calendar.today)}
          </button>
        </div>

        {/* Weekday headers */}
        <div className="grid grid-cols-7 mb-1">
          {WEEKDAYS.map((wd) => (
            <div
              key={wd}
              className="py-1 text-center text-xs font-medium text-muted-foreground"
            >
              {weekdayKey(wd)}
            </div>
          ))}
        </div>

        {/* Day grid */}
        <div className="grid grid-cols-7 border-t border-l">
          {grid.map((cell) => {
            const key = dateKey(cell.date);
            const dayIssues = issuesByDay.get(key) ?? [];
            const isSelected = selectedDay === key;
            const count = dayIssues.length;

            return (
              <button
                key={key}
                onClick={() => setSelectedDay(isSelected ? null : key)}
                className={[
                  "relative flex flex-col items-start border-r border-b p-1.5 min-h-[72px] text-left transition-colors",
                  cell.inMonth ? "" : "bg-muted/30",
                  cell.isWeekend && cell.inMonth ? "bg-muted/15" : "",
                  isSelected ? "bg-accent" : "hover:bg-accent/50",
                ]
                  .filter(Boolean)
                  .join(" ")}
              >
                <span
                  className={[
                    "inline-flex h-6 w-6 items-center justify-center rounded-full text-xs tabular-nums",
                    cell.isToday
                      ? "bg-brand text-white font-semibold"
                      : cell.inMonth
                      ? "text-foreground"
                      : "text-muted-foreground/40",
                  ].join(" ")}
                >
                  {cell.date.getDate()}
                </span>

                {count > 0 && (
                  <div className="mt-0.5 flex flex-wrap gap-0.5">
                    {dayIssues.slice(0, 5).map((issue) => (
                      <span
                        key={issue.id}
                        className={`inline-block h-1.5 w-1.5 rounded-full ${STATUS_DOT_COLOR[issue.status]}`}
                      />
                    ))}
                    {count > 5 && (
                      <span className="text-[9px] leading-none text-muted-foreground">
                        +{count - 5}
                      </span>
                    )}
                  </div>
                )}
              </button>
            );
          })}
        </div>

        {/* Selected day detail */}
        {selectedDay && selectedIssues.length > 0 && (
          <div className="mt-3 rounded-lg border bg-card p-3">
            <div className="space-y-1.5">
              {selectedIssues.map((issue) => (
                <div
                  key={issue.id}
                  className="flex items-center gap-2 text-sm"
                >
                  <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
                  <span className="text-muted-foreground shrink-0">
                    {issue.identifier}
                  </span>
                  <span className="truncate">{issue.title}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Empty state */}
        {!hasAnyDates && (
          <div className="mt-6 text-center text-sm text-muted-foreground">
            {t(($) => $.calendar.empty)}
          </div>
        )}
      </div>
    </div>
  );
}
