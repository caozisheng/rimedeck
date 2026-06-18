"use client";

import { X, ExternalLink, Trash2 } from "lucide-react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import type { Issue } from "@multica/core/types";
import { StatusIcon } from "../status-icon";
import { PriorityIcon } from "../priority-icon";
import { useT } from "../../../i18n";

interface DagDetailPanelProps {
  issue: Issue;
  onClose: () => void;
  onNavigateToIssue?: (issueId: string) => void;
}

function DependencySection({
  title,
  issues,
  onNavigate,
}: {
  title: string;
  issues: Issue[];
  onNavigate?: (id: string) => void;
}) {
  if (issues.length === 0) return null;
  return (
    <div className="space-y-1">
      <span className="text-xs font-medium text-muted-foreground">{title}</span>
      {issues.map((issue) => (
        <button
          key={issue.id}
          className="flex items-center gap-2 w-full rounded px-2 py-1 text-xs hover:bg-accent transition-colors"
          onClick={() => onNavigate?.(issue.id)}
        >
          <StatusIcon status={issue.status} className="size-3" />
          <span className="text-muted-foreground font-mono">{issue.identifier}</span>
          <span className="truncate">{issue.title}</span>
        </button>
      ))}
    </div>
  );
}

export function DagDetailPanel({
  issue,
  onClose,
  onNavigateToIssue,
}: DagDetailPanelProps) {
  const wsId = useWorkspaceId();
  const { t } = useT("issues");
  const qc = useQueryClient();

  const { data: deps } = useQuery({
    queryKey: ["issue-dependencies", wsId, issue.id],
    queryFn: () => api.listIssueDependencies(issue.id),
  });

  const deleteMutation = useMutation({
    mutationFn: (depId: string) => api.deleteIssueDependency(issue.id, depId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["issue-dependencies", wsId, issue.id] });
      qc.invalidateQueries({ queryKey: ["issue-dependency-graph"] });
      qc.invalidateQueries({ queryKey: ["project-dependency-graph"] });
    },
  });

  return (
    <div className="absolute right-0 top-0 bottom-0 w-80 border-l bg-card z-20 flex flex-col shadow-lg">
      {/* Header */}
      <div className="flex items-center justify-between border-b p-3">
        <div className="flex items-center gap-2 min-w-0">
          <StatusIcon status={issue.status} className="size-4 shrink-0" />
          <span className="font-mono text-xs text-muted-foreground shrink-0">{issue.identifier}</span>
          <span className="text-sm font-medium truncate">{issue.title}</span>
        </div>
        <Button variant="ghost" size="sm" className="size-7 p-0 shrink-0" onClick={onClose}>
          <X className="size-3.5" />
        </Button>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto p-3 space-y-4 text-sm">
        {/* Status & Priority */}
        <div className="grid grid-cols-2 gap-3">
          <div>
            <span className="text-xs text-muted-foreground block mb-1">{t(($) => $.dag_view.status)}</span>
            <div className="flex items-center gap-1.5">
              <StatusIcon status={issue.status} className="size-3.5" />
              <span className="capitalize">{issue.status.replace("_", " ")}</span>
            </div>
          </div>
          <div>
            <span className="text-xs text-muted-foreground block mb-1">{t(($) => $.dag_view.priority)}</span>
            <div className="flex items-center gap-1.5">
              <PriorityIcon priority={issue.priority} className="size-3.5" />
              <span className="capitalize">{issue.priority}</span>
            </div>
          </div>
        </div>

        {/* Dates */}
        {(issue.start_date || issue.due_date) && (
          <div className="grid grid-cols-2 gap-3">
            {issue.start_date && (
              <div>
                <span className="text-xs text-muted-foreground block mb-1">{t(($) => $.dag_view.start)}</span>
                <span className="text-xs">{issue.start_date}</span>
              </div>
            )}
            {issue.due_date && (
              <div>
                <span className="text-xs text-muted-foreground block mb-1">{t(($) => $.dag_view.due)}</span>
                <span className="text-xs">{issue.due_date}</span>
              </div>
            )}
          </div>
        )}

        {/* Dependencies */}
        {deps && (
          <div className="space-y-3 border-t pt-3">
            <DependencySection
              title="Blocks"
              issues={deps.blocks}
              onNavigate={onNavigateToIssue}
            />
            <DependencySection
              title="Blocked by"
              issues={deps.blocked_by}
              onNavigate={onNavigateToIssue}
            />
            <DependencySection
              title="Related"
              issues={deps.relates_to}
              onNavigate={onNavigateToIssue}
            />

            {/* Raw deps for deletion */}
            {deps.raw.length > 0 && (
              <div className="space-y-1 border-t pt-2">
                <span className="text-xs font-medium text-muted-foreground">
                  {t(($) => $.dag_view.all_deps, { count: deps.raw.length })}
                </span>
                {deps.raw.map((dep) => (
                  <div
                    key={dep.id}
                    className="flex items-center justify-between rounded px-2 py-1 text-xs hover:bg-accent"
                  >
                    <span className="text-muted-foreground">
                      {dep.dep_type === "blocks" ? "→ blocks" : "↔ related"}
                    </span>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="size-6 p-0 text-destructive hover:text-destructive"
                      onClick={() => deleteMutation.mutate(dep.id)}
                      disabled={deleteMutation.isPending}
                    >
                      <Trash2 className="size-3" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="border-t p-3">
        <Button
          variant="outline"
          size="sm"
          className="w-full gap-1.5"
          onClick={() => onNavigateToIssue?.(issue.id)}
        >
          <ExternalLink className="size-3.5" />
          {t(($) => $.dag_view.go_to_issue)}
        </Button>
      </div>
    </div>
  );
}
