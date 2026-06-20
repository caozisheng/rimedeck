"use client";

import { useState, useCallback, useRef } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useWorkspacePaths } from "@rimedeck/core/paths";
import { sopDetailOptions, workspaceKeys } from "@rimedeck/core/workspace/queries";
import { api } from "@rimedeck/core/api";
import { Button } from "@rimedeck/ui/components/ui/button";
import { Skeleton } from "@rimedeck/ui/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@rimedeck/ui/components/ui/dropdown-menu";
import { ArrowLeft, Workflow as SOPIcon, Upload, MoreHorizontal, Download } from "lucide-react";
import { toast } from "sonner";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { SOPEditor } from "./sop-editor";
import { SOPStats } from "./sop-stats";
import type { RuleGoChain, RuleGoChainInfo } from "../utils/rulego-adapter";

interface SOPDetailPageProps {
  sopId: string;
}

export function SOPDetailPage({ sopId }: SOPDetailPageProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const { t } = useT("sops");

  const { data: sop, isLoading } = useQuery(
    sopDetailOptions(wsId, sopId),
  );

  const publishMutation = useMutation({
    mutationFn: () => api.publishSOP(sopId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workspaceKeys.sops(wsId) });
      queryClient.invalidateQueries({
        queryKey: [...workspaceKeys.sops(wsId), sopId],
      });
      toast.success(t(($) => $.detail.published));
    },
    onError: () => toast.error(t(($) => $.detail.publish_failed)),
  });

  if (isLoading) {
    return (
      <div className="flex flex-1 flex-col">
        <div className="flex h-12 items-center gap-3 border-b px-4">
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="h-4 w-48" />
        </div>
        <div className="flex-1">
          <Skeleton className="h-full w-full" />
        </div>
      </div>
    );
  }

  if (!sop) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        {t(($) => $.detail.not_found)}
      </div>
    );
  }

  const graph = (sop.graph as unknown as RuleGoChain) ?? { ruleChain: {}, metadata: { firstNodeIndex: 0, nodes: [], connections: [] } };
  const chainInfo: RuleGoChainInfo = graph.ruleChain ?? {
    id: sopId,
    name: sop.name,
  };

  async function handleGraphChange(updated: RuleGoChain): Promise<void> {
    try {
      await api.updateSOP(sopId, { graph: updated as unknown as Record<string, unknown> });
      queryClient.invalidateQueries({
        queryKey: [...workspaceKeys.sops(wsId), sopId],
      });
      toast.success(t(($) => $.detail.saved));
    } catch {
      toast.error(t(($) => $.detail.save_failed));
    }
  }

  function handleExport(format: "json" | "n8n" | "dify") {
    const ext = format === "dify" ? "yaml" : "json";
    const suffix = format === "json" ? "" : `_${format}`;
    api
      .exportSOP(sopId, format)
      .then((blob) => {
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${sop!.name}${suffix}.${ext}`;
        a.click();
        URL.revokeObjectURL(url);
      })
      .catch(() => toast.error(t(($) => $.detail.export_failed)));
  }

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* Header bar */}
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <div className="flex items-center gap-3">
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground"
            onClick={() => navigation.push(paths.sops())}
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <SOPIcon className="h-4 w-4 text-muted-foreground" />
          <EditableName
            value={sop.name}
            onSave={async (name) => {
              await api.updateSOP(sopId, { name });
              queryClient.invalidateQueries({
                queryKey: [...workspaceKeys.sops(wsId), sopId],
              });
              queryClient.invalidateQueries({
                queryKey: workspaceKeys.sops(wsId),
              });
            }}
          />
          <StatusBadge status={sop.status} />
        </div>
        <div className="flex items-center gap-2">
          {sop.status === "draft" && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => publishMutation.mutate()}
              disabled={publishMutation.isPending}
            >
              <Upload className="mr-1 h-3 w-3" />
              {t(($) => $.detail.publish)}
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon-sm" />}
            >
              <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-auto">
              <DropdownMenuItem onClick={() => handleExport("json")}>
                <Download className="h-3.5 w-3.5" />
                {t(($) => $.detail.export_json)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleExport("n8n")}>
                <Download className="h-3.5 w-3.5" />
                {t(($) => $.detail.export_n8n)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleExport("dify")}>
                <Download className="h-3.5 w-3.5" />
                {t(($) => $.detail.export_dify)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Stats panel */}
      <SOPStats sopId={sopId} />

      {/* Editor */}
      <div className="flex-1 overflow-hidden">
        <SOPEditor
          sopId={sopId}
          graph={graph}
          chainInfo={chainInfo}
          onChange={handleGraphChange}
        />
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useT("sops");
  const label = (t(($) => $.status[status as keyof typeof $.status]) as string) ?? status;
  const colors: Record<string, string> = {
    draft: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400",
    published: "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400",
    archived: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",
  };
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${colors[status] ?? colors.draft}`}
    >
      {label}
    </span>
  );
}

/** Click-to-edit inline name field. Saves on blur or Enter; reverts on Escape. */
function EditableName({
  value,
  onSave,
}: {
  value: string;
  onSave: (name: string) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const inputRef = useRef<HTMLInputElement>(null);

  const startEdit = useCallback(() => {
    setDraft(value);
    setEditing(true);
    // Focus after render.
    setTimeout(() => inputRef.current?.select(), 0);
  }, [value]);

  const commit = useCallback(async () => {
    const trimmed = draft.trim();
    setEditing(false);
    if (!trimmed || trimmed === value) return;
    try {
      await onSave(trimmed);
    } catch {
      toast.error("Failed to rename SOP");
    }
  }, [draft, value, onSave]);

  if (!editing) {
    return (
      <button
        type="button"
        className="text-sm font-medium hover:underline hover:underline-offset-2 cursor-text"
        onClick={startEdit}
      >
        {value}
      </button>
    );
  }

  return (
    <input
      ref={inputRef}
      autoFocus
      className="text-sm font-medium bg-transparent border-b border-primary outline-none px-0 py-0 w-48"
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          commit();
        } else if (e.key === "Escape") {
          setEditing(false);
        }
      }}
    />
  );
}
