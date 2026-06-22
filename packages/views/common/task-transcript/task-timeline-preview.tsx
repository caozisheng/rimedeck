"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Brain, ChevronDown, ChevronRight, FileText, TerminalSquare } from "lucide-react";
import { taskMessagesOptions } from "@rimedeck/core/chat/queries";
import { cn } from "@rimedeck/ui/lib/utils";
import { buildTimeline, type TimelineItem } from "./build-timeline";
import { getToolDisplayName, getToolResultDisplayName } from "./tool-labels";

interface TaskTimelinePreviewProps {
  taskId: string;
  className?: string;
  maxItems?: number;
  emptyFallback?: ReactNode;
}

export function TaskTimelinePreview({
  taskId,
  className,
  maxItems = 6,
  emptyFallback,
}: TaskTimelinePreviewProps) {
  const { data: messages } = useQuery(taskMessagesOptions(taskId));
  const items = useMemo(() => buildTimeline(messages ?? []), [messages]);
  const visibleItems = items.slice(Math.max(0, items.length - maxItems));

  if (visibleItems.length === 0) return emptyFallback ? <>{emptyFallback}</> : null;

  return (
    <div
      className={cn(
        "space-y-1 rounded-md border border-border/60 bg-muted/20 p-2",
        className,
      )}
      data-testid="task-timeline-preview"
    >
      {visibleItems.map((item) => (
        <TaskTimelinePreviewRow key={item.seq} item={item} />
      ))}
    </div>
  );
}

function TaskTimelinePreviewRow({ item }: { item: TimelineItem }) {
  const [expanded, setExpanded] = useState(false);
  const label = getPreviewLabel(item);
  const fullText = getPreviewFullText(item);
  const canExpand = fullText !== "";

  return (
    <div className="min-w-0 text-xs">
      <div className="flex min-w-0 items-center gap-1.5">
        <span className={cn("shrink-0", getPreviewTone(item))}>
          {getPreviewIcon(item)}
        </span>
        <span className="shrink-0 font-medium text-muted-foreground">
          {label}
        </span>
        <div className="min-w-0 flex-1" />
        {canExpand && (
          <button
            type="button"
            className="shrink-0 rounded p-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            aria-label={`${expanded ? "Collapse" : "Expand"} ${label}`}
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
          </button>
        )}
      </div>
      {!expanded && fullText && (
        <div
          className="mt-0.5 ml-5 line-clamp-2 min-w-0 whitespace-pre-wrap break-words text-muted-foreground"
          data-testid="task-timeline-row-summary"
        >
          {fullText}
        </div>
      )}
      {expanded && canExpand && (
        <div
          className="mt-1 ml-5 whitespace-pre-wrap break-words rounded-sm bg-background/60 px-2 py-1.5 font-mono text-[11px] leading-relaxed text-muted-foreground"
          data-testid="task-timeline-row-detail"
        >
          {fullText}
        </div>
      )}
    </div>
  );
}

function getPreviewIcon(item: TimelineItem) {
  switch (item.type) {
    case "thinking":
      return <Brain className="h-3.5 w-3.5" />;
    case "tool_use":
    case "tool_result":
      return <TerminalSquare className="h-3.5 w-3.5" />;
    case "error":
      return <AlertCircle className="h-3.5 w-3.5" />;
    default:
      return <FileText className="h-3.5 w-3.5" />;
  }
}

function getPreviewTone(item: TimelineItem): string {
  switch (item.type) {
    case "thinking":
      return "text-violet-500";
    case "tool_use":
    case "tool_result":
      return "text-info";
    case "error":
      return "text-destructive";
    default:
      return "text-success";
  }
}

function getPreviewLabel(item: TimelineItem): string {
  switch (item.type) {
    case "thinking":
      return "Thinking";
    case "tool_use":
      return getToolDisplayName(item.tool) ?? "Tool";
    case "tool_result":
      return getToolResultDisplayName(item.tool);
    case "error":
      return "Error";
    default:
      return "Agent";
  }
}

function getPreviewFullText(item: TimelineItem): string {
  if (item.type === "tool_use") {
    return getInputText(item.input);
  }
  if (item.type === "tool_result") {
    return item.output?.trim() ?? "";
  }
  return item.content?.trim() ?? "";
}

function getInputText(input: Record<string, unknown> | undefined): string {
  if (!input) return "";
  for (const key of ["command", "file_path", "path", "query", "pattern", "description", "prompt"]) {
    const value = input[key];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  const firstString = Object.values(input).find((value): value is string =>
    typeof value === "string" && value.trim().length > 0,
  );
  return firstString ? firstString.trim() : "";
}
