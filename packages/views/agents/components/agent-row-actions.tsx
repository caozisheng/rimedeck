"use client";

import { useState } from "react";
import {
  AlertCircle,
  Copy,
  MoreHorizontal,
  RotateCcw,
  Square,
  Trash2,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent, Squad } from "@multica/core/types";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

interface AgentRowActionsProps {
  agent: Agent;
  presence: AgentPresenceDetail | null | undefined;
  // True when the current user can manage this agent (owner of agent or
  // workspace admin/owner). Mirrors the back-end's canManageAgent check —
  // the server is still the source of truth, this only hides UI for ops
  // the user can't perform.
  canManage: boolean;
  // Called when the user picks "Duplicate" — the page opens a Create
  // dialog pre-populated with this agent's config as a template.
  onDuplicate: (agent: Agent) => void;
}

/**
 * Per-row dropdown menu for the agents list. The set of actions is derived
 * from (a) the agent's lifecycle state (active vs archived) and (b) the
 * caller's permission level. If no actions apply, the trigger is omitted so
 * the row renders an empty cell (column width still preserved by the parent
 * `<TableCell className="w-10" />`).
 *
 * All triggers stop event propagation so clicks don't bubble up to the
 * row's navigate-to-detail handler.
 */
export function AgentRowActions({
  agent,
  presence,
  canManage,
  onDuplicate,
}: AgentRowActionsProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const qc = useQueryClient();

  const [confirmArchive, setConfirmArchive] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [leaderWarningSquads, setLeaderWarningSquads] = useState<Squad[]>([]);

  const { data: squads = [] } = useQuery<Squad[]>({
    queryKey: workspaceKeys.squads(wsId),
    queryFn: () => api.listSquads(),
    enabled: !!wsId,
  });

  const handleArchiveClick = () => {
    const ledSquads = squads.filter(
      (s) => s.leader_id === agent.id && !s.archived_at,
    );
    if (ledSquads.length > 0) {
      setLeaderWarningSquads(ledSquads);
    } else {
      setConfirmArchive(true);
    }
  };

  const isArchived = !!agent.archived_at;
  const runningCount = presence?.runningCount ?? 0;
  const queuedCount = presence?.queuedCount ?? 0;
  const hasActiveWork = runningCount + queuedCount > 0;

  // Derive which menu items to render. Doing this once here keeps the JSX
  // below a flat list of conditionals rather than a tangle of role/state
  // branches.
  const showStop = canManage && !isArchived && hasActiveWork;
  const showDuplicate = !isArchived; // any workspace member can duplicate
  const showArchive = canManage && !isArchived;
  const showRestore = canManage && isArchived;

  const hasAnyAction = showStop || showDuplicate || showArchive || showRestore;

  const invalidateAgents = () => {
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  };

  const handleArchive = async () => {
    try {
      await api.archiveAgent(agent.id);
      invalidateAgents();
      toast.success(t(($) => $.row_actions.agent_archived_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.row_actions.archive_failed_toast));
    }
  };

  const handleRestore = async () => {
    try {
      await api.restoreAgent(agent.id);
      invalidateAgents();
      toast.success(t(($) => $.row_actions.agent_restored_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.row_actions.restore_failed_toast));
    }
  };

  const handleCancelTasks = async () => {
    try {
      const { cancelled } = await api.cancelAgentTasks(agent.id);
      invalidateAgents();
      toast.success(
        cancelled === 0
          ? t(($) => $.row_actions.no_tasks_to_cancel_toast)
          : t(($) => $.row_actions.cancelled_tasks_toast, { count: cancelled }),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.row_actions.cancel_failed_toast));
    }
  };

  if (!hasAnyAction) {
    return null;
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.row.actions_aria)}
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => e.stopPropagation()}
            />
          }
        >
          <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="end"
          className="w-auto"
          // Prevent the row's onClick from firing if a click on a menu item
          // somehow bubbles back through the portal.
          onClick={(e) => e.stopPropagation()}
        >
          {showStop && (
            <DropdownMenuItem
              onClick={() => setConfirmCancel(true)}
            >
              <Square className="h-3.5 w-3.5" />
              {t(($) => $.row_actions.cancel_all_tasks)}
            </DropdownMenuItem>
          )}
          {showDuplicate && (
            <DropdownMenuItem onClick={() => onDuplicate(agent)}>
              <Copy className="h-3.5 w-3.5" />
              {t(($) => $.row_actions.duplicate)}
            </DropdownMenuItem>
          )}
          {showRestore && (
            <DropdownMenuItem onClick={handleRestore}>
              <RotateCcw className="h-3.5 w-3.5" />
              {t(($) => $.row_actions.restore)}
            </DropdownMenuItem>
          )}
          {showArchive && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className="text-destructive"
                onClick={handleArchiveClick}
              >
                <Trash2 className="h-3.5 w-3.5" />
                {t(($) => $.row_actions.archive)}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      {confirmCancel && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setConfirmCancel(false);
          }}
        >
          <AlertDialogContent
            // Keep clicks inside the dialog from bubbling to the row.
            onClick={(e) => e.stopPropagation()}
          >
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.row_actions.cancel_dialog_title, { name: agent.name })}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {describeCancelImpact(runningCount, queuedCount, t)}
                {runningCount > 0 && t(($) => $.row_actions.cancel_dialog_running_note)}
                {t(($) => $.row_actions.cancel_dialog_irreversible)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.row_actions.cancel_dialog_keep)}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmCancel(false);
                  void handleCancelTasks();
                }}
              >
                {t(($) => $.row_actions.cancel_dialog_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}

      {confirmArchive && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setConfirmArchive(false);
          }}
        >
          <AlertDialogContent onClick={(e) => e.stopPropagation()}>
            <AlertDialogHeader>
              <div className="flex items-start gap-3">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
                  <AlertCircle className="h-5 w-5 text-destructive" />
                </div>
                <div className="flex-1">
                  <AlertDialogTitle>
                    {t(($) => $.row_actions.archive_dialog_title, { name: agent.name })}
                  </AlertDialogTitle>
                  <AlertDialogDescription>
                    {t(($) => $.row_actions.archive_dialog_description)}
                  </AlertDialogDescription>
                </div>
              </div>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.row_actions.archive_dialog_cancel)}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmArchive(false);
                  void handleArchive();
                }}
              >
                {t(($) => $.row_actions.archive_dialog_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}

      {leaderWarningSquads.length > 0 && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setLeaderWarningSquads([]);
          }}
        >
          <AlertDialogContent onClick={(e) => e.stopPropagation()}>
            <AlertDialogHeader>
              <div className="flex items-start gap-3">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/30">
                  <AlertCircle className="h-5 w-5 text-amber-600 dark:text-amber-400" />
                </div>
                <div className="flex-1">
                  <AlertDialogTitle>
                    {t(($) => $.row_actions.leader_warning_title)}
                  </AlertDialogTitle>
                  <AlertDialogDescription>
                    {t(($) => $.row_actions.leader_warning_description)}
                  </AlertDialogDescription>
                  <ul className="mt-2 space-y-1">
                    {leaderWarningSquads.map((s) => (
                      <li key={s.id}>
                        <AppLink
                          href={wsPaths.squadDetail(s.id)}
                          className="text-sm text-foreground underline underline-offset-2 hover:text-foreground/80"
                          onClick={() => setLeaderWarningSquads([])}
                        >
                          {s.name}
                        </AppLink>
                      </li>
                    ))}
                  </ul>
                </div>
              </div>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.row_actions.leader_warning_cancel)}</AlertDialogCancel>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}

type AgentsT = ReturnType<typeof useT<"agents">>["t"];

function describeCancelImpact(running: number, queued: number, t: AgentsT): string {
  if (running === 0 && queued === 0) {
    return t(($) => $.row_actions.cancel_dialog_no_tasks);
  }
  const parts: string[] = [];
  if (running > 0) parts.push(t(($) => $.row_actions.cancel_dialog_running, { count: running }));
  if (queued > 0) parts.push(t(($) => $.row_actions.cancel_dialog_queued, { count: queued }));
  return t(($) => $.row_actions.cancel_dialog_impact, {
    summary: parts.join(" + "),
    count: running + queued,
  });
}
