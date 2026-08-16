"use client";

import { useState } from "react";
import { toast } from "sonner";
import type { Issue } from "@rimedeck/core/types";
import {
  useDetachIssueTracker,
  useDiscardIssuePending,
} from "@rimedeck/core/issues/mutations";
import { useRetryGitlabTracker } from "@rimedeck/core";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@rimedeck/ui/components/ui/alert-dialog";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useT } from "../../i18n";

export function GitlabConflictDialog({
  issue,
  projectId,
  open,
  onOpenChange,
}: {
  issue: Issue;
  projectId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [busy, setBusy] = useState(false);
  const retry = useRetryGitlabTracker(wsId, projectId);
  const detach = useDetachIssueTracker();
  const discard = useDiscardIssuePending();
  const trackerId = issue.tracker_connection_id ?? "";

  const run = async (action: () => Promise<unknown>, okMessage: string, failFallback: string) => {
    if (busy) return;
    setBusy(true);
    try {
      await action();
      toast.success(okMessage);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : failFallback);
    } finally {
      setBusy(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.gitlab_conflict.title)}</AlertDialogTitle>
          <AlertDialogDescription>{t(($) => $.gitlab_conflict.description)}</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="flex flex-col gap-2 py-2">
          <button
            type="button"
            disabled={busy || !trackerId}
            onClick={() => void run(() => retry.mutateAsync(trackerId), t(($) => $.gitlab_conflict.retry_ok), t(($) => $.gitlab_conflict.retry_failed))}
            className="rounded-md border px-3 py-2 text-left text-sm hover:bg-accent disabled:opacity-50"
          >
            <div className="font-medium">{t(($) => $.gitlab_conflict.retry_title)}</div>
            <div className="text-xs text-muted-foreground">{t(($) => $.gitlab_conflict.retry_hint)}</div>
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => void run(() => detach.mutateAsync(issue.id), t(($) => $.gitlab_conflict.detach_ok), t(($) => $.gitlab_conflict.detach_failed))}
            className="rounded-md border px-3 py-2 text-left text-sm hover:bg-accent disabled:opacity-50"
          >
            <div className="font-medium">{t(($) => $.gitlab_conflict.detach_title)}</div>
            <div className="text-xs text-muted-foreground">{t(($) => $.gitlab_conflict.detach_hint)}</div>
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => void run(() => discard.mutateAsync(issue.id), t(($) => $.gitlab_conflict.discard_ok), t(($) => $.gitlab_conflict.discard_failed))}
            className="rounded-md border border-destructive/40 px-3 py-2 text-left text-sm hover:bg-destructive/10 disabled:opacity-50"
          >
            <div className="font-medium text-destructive">{t(($) => $.gitlab_conflict.discard_title)}</div>
            <div className="text-xs text-muted-foreground">{t(($) => $.gitlab_conflict.discard_hint)}</div>
          </button>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.gitlab_conflict.cancel)}</AlertDialogCancel>
          <AlertDialogAction disabled>{t(($) => $.gitlab_conflict.pick_action)}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
