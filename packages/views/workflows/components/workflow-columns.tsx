"use client";

import { Workflow, Trash2, ChevronRight } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import type { Agent, WorkflowSummary } from "@rimedeck/core/types";
import { useTimeAgo, useT } from "../../i18n";
import { ActorAvatar } from "@rimedeck/ui/components/common/actor-avatar";
import { resolvePublicFileUrl } from "@rimedeck/core/workspace/avatar-url";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@rimedeck/ui/components/ui/tooltip";

// ---------------------------------------------------------------------------
// Row shape
// ---------------------------------------------------------------------------

export interface WorkflowRow {
  workflow: WorkflowSummary;
  agents: Agent[];
}

// ---------------------------------------------------------------------------
// Column widths
// ---------------------------------------------------------------------------

const COL_WIDTHS = {
  name: 260,
  usedBy: 140,
  category: 120,
  status: 100,
  updated: 100,
  actions: 48,
  chevron: 48,
} as const;

// ---------------------------------------------------------------------------
// Constants moved from workflows-page
// ---------------------------------------------------------------------------

export const STATUS_COLORS: Record<string, string> = {
  published: "bg-emerald-500/10 text-emerald-600",
  draft: "bg-amber-500/10 text-amber-600",
  archived: "bg-zinc-500/10 text-zinc-500",
};

const CATEGORY_ICONS: Record<string, string> = {
  document: "📄",
  scraper: "🕷️",
  subscription: "📰",
  spreadsheet: "📊",
  sales: "💼",
  general: "⚙️",
};

// ---------------------------------------------------------------------------
// Column definitions
// ---------------------------------------------------------------------------

export function useWorkflowColumns(
  onDelete: (wf: WorkflowSummary) => void,
): ColumnDef<WorkflowRow>[] {
  const timeAgo = useTimeAgo();
  const { t } = useT("workflows");
  return [
    {
      id: "name",
      header: t(($) => $.table.name),
      size: COL_WIDTHS.name,
      meta: { grow: true },
      cell: ({ row }) => <WorkflowNameCell row={row.original} />,
    },
    {
      id: "usedBy",
      header: t(($) => $.table.used_by),
      size: COL_WIDTHS.usedBy,
      cell: ({ row }) => <AgentAssignees agents={row.original.agents} notMountedLabel={t(($) => $.table.not_mounted)} />,
    },
    {
      id: "category",
      header: t(($) => $.table.category),
      cell: ({ row }) => {
        const cat = row.original.workflow.category;
        const icon = CATEGORY_ICONS[cat] ?? "";
        const label = (t(($) => $.categories[cat as keyof typeof $.categories]) as string) ?? cat;
        return (
          <span className="whitespace-nowrap text-xs text-muted-foreground">
            {icon ? `${icon} ${label}` : label}
          </span>
        );
      },
    },
    {
      id: "status",
      header: t(($) => $.table.status),
      size: COL_WIDTHS.status,
      cell: ({ row }) => <StatusBadge status={row.original.workflow.status} />,
    },
    {
      id: "updated",
      header: t(($) => $.table.updated),
      size: COL_WIDTHS.updated,
      cell: ({ row }) => (
        <span className="whitespace-nowrap text-xs text-muted-foreground">
          {timeAgo(row.original.workflow.updated_at)}
        </span>
      ),
    },
    {
      id: "_actions",
      header: () => null,
      size: COL_WIDTHS.actions,
      enableResizing: false,
      cell: ({ row }) => (
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onDelete(row.original.workflow);
                }}
                className="rounded p-1 text-muted-foreground/0 transition-colors group-hover:text-muted-foreground hover:!text-destructive"
                aria-label={t(($) => $.table.delete_aria)}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            }
          />
          <TooltipContent>{t(($) => $.table.delete_tooltip)}</TooltipContent>
        </Tooltip>
      ),
    },
    {
      id: "_chevron",
      header: () => null,
      size: COL_WIDTHS.chevron,
      enableResizing: false,
      cell: () => (
        <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground" />
      ),
    },
  ];
}

// ---------------------------------------------------------------------------
// Cell renderers
// ---------------------------------------------------------------------------

function WorkflowNameCell({ row }: { row: WorkflowRow }) {
  const { workflow } = row;
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-2">
        <Workflow className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="block min-w-0 truncate font-medium">
          {workflow.name}
        </span>
      </div>
      {workflow.description && (
        <div className="mt-0.5 max-w-xl truncate text-xs text-muted-foreground">
          {workflow.description}
        </div>
      )}
    </div>
  );
}

function AgentAssignees({ agents, notMountedLabel }: { agents: Agent[]; notMountedLabel: string }) {
  if (agents.length === 0) {
    return (
      <span className="text-xs text-muted-foreground/70">{notMountedLabel}</span>
    );
  }
  const visible = agents.slice(0, 3);
  const extra = agents.length - visible.length;
  return (
    <div className="flex items-center -space-x-1.5">
      {visible.map((a) => (
        <Tooltip key={a.id}>
          <TooltipTrigger
            render={
              <span className="inline-flex rounded-full ring-2 ring-background">
                <ActorAvatar
                  name={a.name}
                  initials={a.name.slice(0, 2).toUpperCase()}
                  avatarUrl={resolvePublicFileUrl(a.avatar_url)}
                  isAgent
                  size={22}
                />
              </span>
            }
          />
          <TooltipContent>{a.name}</TooltipContent>
        </Tooltip>
      ))}
      {extra > 0 && (
        <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground ring-2 ring-background">
          +{extra}
        </span>
      )}
    </div>
  );
}

export function StatusBadge({ status }: { status: string }) {
  const { t } = useT("workflows");
  const label = (t(($) => $.status[status as keyof typeof $.status]) as string) ?? status;
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium ${STATUS_COLORS[status] ?? STATUS_COLORS.draft}`}
    >
      {label}
    </span>
  );
}
