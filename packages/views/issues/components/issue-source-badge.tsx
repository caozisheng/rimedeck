import { useState } from "react";
import { ExternalLink, GitBranch } from "lucide-react";
import type { Issue } from "@rimedeck/core/types";
import { cn } from "@rimedeck/ui/lib/utils";
import { GitlabConflictDialog } from "./gitlab-conflict-dialog";

const syncLabels: Record<string, string> = {
  pending: "Pending sync",
  syncing: "Syncing",
  failed: "Sync failed",
  pending_delete: "Pending delete",
  detached: "Detached",
};

export function IssueSourceBadge({ issue }: { issue: Issue }) {
  const [dialogOpen, setDialogOpen] = useState(false);
  if (issue.source_type === "local") {
    return <span className="inline-flex items-center rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">Local</span>;
  }

  const external = issue.external;
  const content = (
    <span className={cn(
      "inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px]",
      issue.source_type === "detached" ? "bg-muted/60 text-muted-foreground" : "bg-orange-500/10 text-orange-700 dark:text-orange-300",
    )}>
      <GitBranch className="size-3" aria-hidden="true" />
      <span>{issue.source_type === "detached" ? "Detached" : external?.iid ? `#${external.iid}` : "GitLab"}</span>
      {issue.external?.url && <ExternalLink className="size-2.5" aria-hidden="true" />}
    </span>
  );

  const badge = external?.url && issue.source_type !== "detached" ? (
    <a href={external.url} target="_blank" rel="noreferrer" onClick={(event) => event.stopPropagation()} aria-label={`Open GitLab issue #${external.iid}`}>
      {content}
    </a>
  ) : content;

  const syncLabel = syncLabels[issue.sync_state];
  const isFailed = issue.sync_state === "failed";
  const canResolve = isFailed && !!issue.project_id;
  return (
    <span className="inline-flex items-center gap-1">
      {badge}
      {syncLabel && (canResolve ? (
        <button
          type="button"
          onClick={(event) => { event.stopPropagation(); setDialogOpen(true); }}
          className={cn(
            "rounded-full px-1.5 py-0.5 text-[10px] transition-colors",
            "bg-destructive/10 text-destructive hover:bg-destructive/20",
          )}
          aria-label="Resolve GitLab sync failure"
        >
          {syncLabel}
        </button>
      ) : (
        <span className={cn(
          "rounded-full px-1.5 py-0.5 text-[10px]",
          isFailed ? "bg-destructive/10 text-destructive" : "bg-muted text-muted-foreground",
        )}>
          {syncLabel}
        </span>
      ))}
      {canResolve && issue.project_id && dialogOpen && (
        <GitlabConflictDialog issue={issue} projectId={issue.project_id} open={dialogOpen} onOpenChange={setDialogOpen} />
      )}
    </span>
  );
}
