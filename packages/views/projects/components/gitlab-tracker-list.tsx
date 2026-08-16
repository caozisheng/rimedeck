"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, GitBranch, MoreHorizontal, RotateCw, Trash2, RefreshCw, PowerOff, KeyRound } from "lucide-react";
import {
  projectGitlabTrackersOptions,
  useSyncGitlabTracker,
  useRetryGitlabTracker,
  useDisableGitlabTracker,
  useDeleteGitlabTrackerMirrors,
  useRotateGitlabTrackerToken,
} from "@rimedeck/core";
import type { GitlabTracker } from "@rimedeck/core/types";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { Button } from "@rimedeck/ui/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@rimedeck/ui/components/ui/dropdown-menu";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@rimedeck/ui/components/ui/alert-dialog";
import { useT } from "../../i18n";
import { useTimeAgo } from "../../i18n/use-time-ago";

// ProjectGitlabTrackerSection renders one row per active GitLab tracker
// attached to the project. Owner/admin-only controls (sync, retry, rotate,
// disable, delete-mirrors) are hidden when the backend flag `can_manage`
// is false; the underlying endpoints double-check the role.
export function ProjectGitlabTrackerSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const { data: trackers, isPending } = useQuery(projectGitlabTrackersOptions(wsId, projectId));

  if (isPending || !trackers || trackers.length === 0) {
    return null;
  }

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen((current) => !current)}
      >
        {t(($) => $.detail.gitlab_trackers_section)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="space-y-1.5">
          {trackers.map((tracker) => (
            <ProjectGitlabTrackerRow key={tracker.id} projectId={projectId} tracker={tracker} />
          ))}
        </div>
      )}
    </div>
  );
}

function ProjectGitlabTrackerRow({ projectId, tracker }: { projectId: string; tracker: GitlabTracker }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const sync = useSyncGitlabTracker(wsId, projectId);
  const retry = useRetryGitlabTracker(wsId, projectId);
  const rotate = useRotateGitlabTrackerToken(wsId, projectId);
  const disable = useDisableGitlabTracker(wsId, projectId);
  const deleteMirrors = useDeleteGitlabTrackerMirrors(wsId, projectId);
  const [rotateOpen, setRotateOpen] = useState(false);
  const [rotateToken, setRotateToken] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const canManage = tracker.can_manage;

  const busy = sync.isPending || retry.isPending || rotate.isPending || disable.isPending || deleteMirrors.isPending;

  const runSync = async () => {
    try { await sync.mutateAsync(tracker.id); toast.success(t(($) => $.detail.gitlab_trackers_sync_ok)); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.detail.gitlab_trackers_sync_failed)); }
  };
  const runRetry = async () => {
    try { const response = await retry.mutateAsync(tracker.id); toast.success(t(($) => $.detail.gitlab_trackers_retry_ok, { count: response.reset_count })); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.detail.gitlab_trackers_retry_failed)); }
  };
  const runRotate = async () => {
    try {
      await rotate.mutateAsync({ trackerId: tracker.id, accessToken: rotateToken.trim() });
      toast.success(t(($) => $.detail.gitlab_trackers_rotate_ok));
      setRotateOpen(false);
      setRotateToken("");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.detail.gitlab_trackers_rotate_failed));
    }
  };
  const runDisable = async () => {
    try { await disable.mutateAsync(tracker.id); toast.success(t(($) => $.detail.gitlab_trackers_disable_ok)); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.detail.gitlab_trackers_disable_failed)); }
  };
  const runDelete = async () => {
    try { await deleteMirrors.mutateAsync(tracker.id); toast.success(t(($) => $.detail.gitlab_trackers_delete_ok)); setConfirmDelete(false); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.detail.gitlab_trackers_delete_failed)); }
  };

  return (
    <div className="rounded-md border px-2.5 py-2 text-xs space-y-1.5">
      <div className="flex items-start gap-2">
        <GitBranch className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate font-medium" title={tracker.path_with_namespace}>{tracker.path_with_namespace}</span>
            <span className={`shrink-0 rounded px-1 py-0.5 text-[10px] ${tracker.state === "disabled" ? "bg-muted text-muted-foreground" : tracker.state === "degraded" ? "bg-amber-500/10 text-amber-600 dark:text-amber-400" : "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"}`}>
              {tracker.state}
            </span>
          </div>
          <div className="mt-0.5 flex items-center gap-2 text-[10px] text-muted-foreground">
            {tracker.last_pull_at && <span>{t(($) => $.detail.gitlab_trackers_last_pull, { when: timeAgo(tracker.last_pull_at) })}</span>}
            {tracker.pending_outbox_count > 0 && <span>{t(($) => $.detail.gitlab_trackers_pending, { count: tracker.pending_outbox_count })}</span>}
            {tracker.failed_outbox_count > 0 && <span className="text-destructive">{t(($) => $.detail.gitlab_trackers_failed, { count: tracker.failed_outbox_count })}</span>}
          </div>
        </div>
        {canManage && (
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" disabled={busy}><MoreHorizontal className="size-3.5" /></Button>} />
            <DropdownMenuContent align="end" className="w-44 text-xs">
              <DropdownMenuItem onClick={() => void runSync()} disabled={tracker.state === "disabled"}><RefreshCw className="mr-2 size-3.5" />{t(($) => $.detail.gitlab_trackers_sync_now)}</DropdownMenuItem>
              <DropdownMenuItem onClick={() => void runRetry()} disabled={tracker.failed_outbox_count === 0}><RotateCw className="mr-2 size-3.5" />{t(($) => $.detail.gitlab_trackers_retry_failed)}</DropdownMenuItem>
              <DropdownMenuItem onClick={() => setRotateOpen(true)} disabled={tracker.state === "disabled"}><KeyRound className="mr-2 size-3.5" />{t(($) => $.detail.gitlab_trackers_rotate)}</DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => void runDisable()} disabled={tracker.state === "disabled"}><PowerOff className="mr-2 size-3.5" />{t(($) => $.detail.gitlab_trackers_disable)}</DropdownMenuItem>
              <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={() => setConfirmDelete(true)}><Trash2 className="mr-2 size-3.5" />{t(($) => $.detail.gitlab_trackers_delete)}</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      {tracker.state === "degraded" && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/5 px-2 py-1.5 text-[11px] text-amber-700 dark:text-amber-300" role="alert">
          {t(($) => $.detail.gitlab_trackers_degraded_banner, { code: tracker.last_error_code ?? "error" })}
        </div>
      )}

      <AlertDialog open={rotateOpen} onOpenChange={setRotateOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.detail.gitlab_trackers_rotate_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.detail.gitlab_trackers_rotate_description)}</AlertDialogDescription>
          </AlertDialogHeader>
          <input type="password" value={rotateToken} onChange={(event) => setRotateToken(event.target.value)} placeholder={t(($) => $.detail.gitlab_trackers_rotate_placeholder)} className="h-8 w-full rounded-md border bg-transparent px-2 text-xs outline-none" />
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => { setRotateOpen(false); setRotateToken(""); }}>{t(($) => $.detail.gitlab_trackers_cancel)}</AlertDialogCancel>
            <AlertDialogAction disabled={!rotateToken.trim() || rotate.isPending} onClick={() => void runRotate()}>{t(($) => $.detail.gitlab_trackers_rotate)}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.detail.gitlab_trackers_delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.detail.gitlab_trackers_delete_description, { path: tracker.path_with_namespace })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.detail.gitlab_trackers_cancel)}</AlertDialogCancel>
            <AlertDialogAction className="bg-destructive text-destructive-foreground" onClick={() => void runDelete()} disabled={deleteMirrors.isPending}>{t(($) => $.detail.gitlab_trackers_delete_confirm)}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
