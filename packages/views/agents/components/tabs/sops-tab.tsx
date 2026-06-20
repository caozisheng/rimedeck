"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Workflow, Plus, X } from "lucide-react";
import type { Agent } from "@rimedeck/core/types";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useWorkspacePaths } from "@rimedeck/core/paths";
import {
  sopListOptions,
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

export function SOPsTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const qc = useQueryClient();

  const [addOpen, setAddOpen] = useState(false);

  const mountedSOPs = agent.sops ?? [];
  const mountedIds = useMemo(
    () => new Set(mountedSOPs.map((w) => w.id)),
    [mountedSOPs],
  );

  const { data: allSOPs = [], isLoading: loadingAll } = useQuery({
    ...sopListOptions(wsId),
    enabled: addOpen,
  });

  const available = useMemo(
    () => allSOPs.filter((w) => !mountedIds.has(w.id) && w.status !== "archived"),
    [allSOPs, mountedIds],
  );

  async function handleAttach(sopId: string) {
    try {
      await api.addAgentSOPs(agent.id, [sopId]);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.sops.attach_success_toast));
      setAddOpen(false);
    } catch {
      toast.error(t(($) => $.sops.attach_failed_toast));
    }
  }

  async function handleDetach(sopId: string) {
    try {
      const remaining = mountedSOPs
        .filter((w) => w.id !== sopId)
        .map((w) => w.id);
      await api.setAgentSOPs(agent.id, remaining);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.sops.detach_success_toast));
    } catch {
      toast.error(t(($) => $.sops.detach_failed_toast));
    }
  }

  return (
    <div className="space-y-4 p-4">
      <div className="flex items-center justify-between">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.sops.intro)}
        </p>
        <Button type="button" size="sm" variant="outline" onClick={() => setAddOpen(true)}>
          <Plus className="h-3 w-3" />
          {t(($) => $.sops.add_action)}
        </Button>
      </div>

      {mountedSOPs.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-8 text-center text-muted-foreground">
          <Workflow className="h-8 w-8 text-muted-foreground/40" />
          <p className="text-sm">{t(($) => $.sops.empty_title)}</p>
          <p className="text-xs max-w-xs">
            {t(($) => $.sops.empty_hint)}
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {mountedSOPs.map((wf) => (
            <div
              key={wf.id}
              className="flex items-center justify-between rounded-lg border p-3 cursor-pointer hover:bg-muted/50 transition-colors"
              onClick={() => navigation.push(paths.sopDetail(wf.id))}
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
                  aria-label={t(($) => $.sops.detach_aria)}
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
            <DialogTitle>{t(($) => $.sops.attach_dialog_title)}</DialogTitle>
          </DialogHeader>
          {loadingAll ? (
            <div className="space-y-2 py-4">
              {Array.from({ length: 3 }, (_, i) => (
                <Skeleton key={i} className="h-12 w-full rounded-md" />
              ))}
            </div>
          ) : available.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {t(($) => $.sops.attach_dialog_empty)}
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
