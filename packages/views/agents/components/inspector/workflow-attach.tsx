"use client";

import { useState } from "react";
import { Plus, Workflow } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@rimedeck/core/types";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import {
  workflowListOptions,
  workspaceKeys,
} from "@rimedeck/core/workspace/queries";
import { api } from "@rimedeck/core/api";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@rimedeck/ui/components/ui/dialog";
import { useT } from "../../../i18n";

/**
 * Inline "+ Attach" trigger for the inspector's Workflows row. Mirrors the
 * SkillAttach component: a dashed-border chip that opens a dialog listing
 * unattached workflows. Hidden when nothing is left to attach.
 */
export function WorkflowAttach({
  agent,
  canEdit = true,
}: {
  agent: Agent;
  /** When false, hide the attach trigger entirely. */
  canEdit?: boolean;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: workspaceWorkflows = [] } = useQuery(workflowListOptions(wsId));
  const [open, setOpen] = useState(false);

  const agentWorkflowIds = new Set((agent.workflows ?? []).map((w) => w.id));
  const available = workspaceWorkflows.filter(
    (w) => !agentWorkflowIds.has(w.id) && w.status !== "archived",
  );

  if (!canEdit || available.length === 0) return null;

  async function handleAttach(workflowId: string) {
    try {
      await api.addAgentWorkflows(agent.id, [workflowId]);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.workflows.attach_success_toast));
      setOpen(false);
    } catch {
      toast.error(t(($) => $.workflows.attach_failed_toast));
    }
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={t(($) => $.workflow_attach.trigger_aria)}
        title={t(($) => $.workflow_attach.trigger_aria)}
        className="inline-flex cursor-pointer items-center gap-0.5 rounded-md border border-dashed border-muted-foreground/30 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground/70 transition-colors hover:border-muted-foreground/60 hover:bg-accent/50 hover:text-muted-foreground"
      >
        <Plus className="h-2.5 w-2.5" />
        {t(($) => $.workflow_attach.trigger_label)}
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.workflows.attach_dialog_title)}
            </DialogTitle>
          </DialogHeader>
          {available.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {t(($) => $.workflows.attach_dialog_empty)}
            </p>
          ) : (
            <div className="max-h-64 space-y-1 overflow-y-auto">
              {available.map((wf) => (
                <button
                  key={wf.id}
                  type="button"
                  className="flex w-full items-center gap-2.5 rounded-md p-2.5 text-left transition-colors hover:bg-muted/50"
                  onClick={() => handleAttach(wf.id)}
                >
                  <Workflow className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{wf.name}</p>
                    {wf.description && (
                      <p className="truncate text-xs text-muted-foreground">
                        {wf.description}
                      </p>
                    )}
                  </div>
                </button>
              ))}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
