"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Workflow, Plus, X } from "lucide-react";
import type { Agent } from "@rimedeck/core/types";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useWorkspacePaths } from "@rimedeck/core/paths";
import {
  workflowListOptions,
  workspaceKeys,
} from "@rimedeck/core/workspace/queries";
import { api } from "@rimedeck/core/api";
import { Button } from "@rimedeck/ui/components/ui/button";
import { Skeleton } from "@rimedeck/ui/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@rimedeck/ui/components/ui/dialog";
import { toast } from "sonner";
import { useNavigation } from "../../../navigation";
import { useT } from "../../../i18n";

export function WorkflowsTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const qc = useQueryClient();

  const [addOpen, setAddOpen] = useState(false);

  const mountedWorkflows = agent.workflows ?? [];
  const mountedIds = useMemo(
    () => new Set(mountedWorkflows.map((w) => w.id)),
    [mountedWorkflows],
  );

  const { data: allWorkflows = [], isLoading: loadingAll } = useQuery({
    ...workflowListOptions(wsId),
    enabled: addOpen,
  });

  const available = useMemo(
    () => allWorkflows.filter((w) => !mountedIds.has(w.id) && w.status !== "archived"),
    [allWorkflows, mountedIds],
  );

  async function handleAttach(workflowId: string) {
    try {
      await api.addAgentWorkflows(agent.id, [workflowId]);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.workflows.attach_success_toast));
      setAddOpen(false);
    } catch {
      toast.error(t(($) => $.workflows.attach_failed_toast));
    }
  }

  async function handleDetach(workflowId: string) {
    try {
      const remaining = mountedWorkflows
        .filter((w) => w.id !== workflowId)
        .map((w) => w.id);
      await api.setAgentWorkflows(agent.id, remaining);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.workflows.detach_success_toast));
    } catch {
      toast.error(t(($) => $.workflows.detach_failed_toast));
    }
  }

  return (
    <div className="space-y-4 p-4">
      <div className="flex items-center justify-between">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.workflows.intro)}
        </p>
        <Button type="button" size="sm" variant="outline" onClick={() => setAddOpen(true)}>
          <Plus className="h-3 w-3" />
          {t(($) => $.workflows.add_action)}
        </Button>
      </div>

      {mountedWorkflows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-8 text-center text-muted-foreground">
          <Workflow className="h-8 w-8 text-muted-foreground/40" />
          <p className="text-sm">{t(($) => $.workflows.empty_title)}</p>
          <p className="text-xs max-w-xs">
            {t(($) => $.workflows.empty_hint)}
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {mountedWorkflows.map((wf) => (
            <div
              key={wf.id}
              className="flex items-center justify-between rounded-lg border p-3 cursor-pointer hover:bg-muted/50 transition-colors"
              onClick={() => navigation.push(paths.workflowDetail(wf.id))}
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <Workflow className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0">
                  <p className="text-sm font-medium truncate">{wf.name}</p>
                  {wf.description && (
                    <p className="text-xs text-muted-foreground truncate">{wf.description}</p>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <span className="text-[10px] text-muted-foreground">{wf.status}</span>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDetach(wf.id);
                  }}
                  className="rounded p-1 text-muted-foreground hover:text-destructive transition-colors"
                  aria-label={t(($) => $.workflows.detach_aria)}
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Attach dialog */}
      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.workflows.attach_dialog_title)}</DialogTitle>
          </DialogHeader>
          {loadingAll ? (
            <div className="space-y-2 py-4">
              {Array.from({ length: 3 }, (_, i) => (
                <Skeleton key={i} className="h-12 w-full rounded-md" />
              ))}
            </div>
          ) : available.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {t(($) => $.workflows.attach_dialog_empty)}
            </p>
          ) : (
            <div className="max-h-64 overflow-y-auto space-y-1">
              {available.map((wf) => (
                <button
                  key={wf.id}
                  type="button"
                  className="flex w-full items-center gap-2.5 rounded-md p-2.5 text-left hover:bg-muted/50 transition-colors"
                  onClick={() => handleAttach(wf.id)}
                >
                  <Workflow className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{wf.name}</p>
                    {wf.description && (
                      <p className="text-xs text-muted-foreground truncate">{wf.description}</p>
                    )}
                  </div>
                </button>
              ))}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
