"use client";

import { useRef, useState } from "react";
import { Download, Upload, Loader2 } from "lucide-react";
import { Button } from "@rimedeck/ui/components/ui/button";
import { Card, CardContent } from "@rimedeck/ui/components/ui/card";
import { Checkbox } from "@rimedeck/ui/components/ui/checkbox";
import { Label } from "@rimedeck/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@rimedeck/ui/components/ui/select";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@rimedeck/ui/components/ui/alert-dialog";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@rimedeck/core/auth";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { memberListOptions, workspaceKeys } from "@rimedeck/core/workspace/queries";
import { useCurrentWorkspace } from "@rimedeck/core/paths";
import { api } from "@rimedeck/core/api";
import type { BackupData } from "@rimedeck/core/types";
import { useT } from "../../i18n";

export function BackupTab() {
  const { t } = useT("settings");
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const [exporting, setExporting] = useState(false);
  const [importing, setImporting] = useState(false);
  const [preview, setPreview] = useState<BackupData | null>(null);
  const [overwrite, setOverwrite] = useState(false);
  const [runtimeId, setRuntimeId] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { data: runtimes = [] } = useQuery({
    queryKey: ["runtimes", wsId],
    queryFn: () => api.listRuntimes(),
    enabled: canManage,
  });

  const handleExport = async () => {
    setExporting(true);
    try {
      const data = await api.exportBackup();
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${workspace?.slug ?? "backup"}-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.backup.toast_export_failed),
      );
    } finally {
      setExporting(false);
    }
  };

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";

    const reader = new FileReader();
    reader.onload = () => {
      try {
        const data = JSON.parse(reader.result as string) as BackupData;
        if (!data.version || !data.skills || !data.agents || !data.squads) {
          toast.error(t(($) => $.backup.toast_import_failed));
          return;
        }
        setPreview(data);
        setOverwrite(false);
        if (runtimes.length > 0 && !runtimeId && runtimes[0]) {
          setRuntimeId(runtimes[0].id);
        }
      } catch {
        toast.error(t(($) => $.backup.toast_import_failed));
      }
    };
    reader.readAsText(file);
  };

  const handleImport = async () => {
    if (!preview || !runtimeId) return;
    setImporting(true);
    try {
      const result = await api.importBackup(preview, runtimeId, overwrite);
      const created =
        result.created.skills + result.created.agents + result.created.squads;
      const skipped =
        result.skipped.skills + result.skipped.agents + result.skipped.squads;
      toast.success(
        t(($) => $.backup.toast_import_success, {
          created,
          skipped,
        }),
      );
      if (result.warnings.length > 0) {
        toast.warning(
          t(($) => $.backup.toast_import_warnings, {
            count: result.warnings.length,
          }),
        );
      }
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      setPreview(null);
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.backup.toast_import_failed),
      );
    } finally {
      setImporting(false);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">
          {t(($) => $.backup.section_title)}
        </h2>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.backup.section_description)}
        </p>

        {/* Export */}
        <Card>
          <CardContent className="space-y-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm font-medium">
                  {t(($) => $.backup.export_title)}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.backup.export_description)}
                </p>
              </div>
              <Button
                size="sm"
                onClick={handleExport}
                disabled={exporting || !canManage}
              >
                {exporting ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Download className="h-3 w-3" />
                )}
                {exporting
                  ? t(($) => $.backup.exporting)
                  : t(($) => $.backup.export_button)}
              </Button>
            </div>
          </CardContent>
        </Card>

        {/* Import */}
        <Card>
          <CardContent className="space-y-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm font-medium">
                  {t(($) => $.backup.import_title)}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.backup.import_description)}
                </p>
              </div>
              <Button
                size="sm"
                variant="outline"
                onClick={() => fileInputRef.current?.click()}
                disabled={!canManage}
              >
                <Upload className="h-3 w-3" />
                {t(($) => $.backup.import_choose_file)}
              </Button>
              <input
                ref={fileInputRef}
                type="file"
                accept=".json"
                className="hidden"
                onChange={handleFileSelect}
              />
            </div>
          </CardContent>
        </Card>

        {!canManage && (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.backup.manage_hint)}
          </p>
        )}
      </section>

      {/* Import preview dialog */}
      <AlertDialog
        open={!!preview}
        onOpenChange={(v) => {
          if (!v) setPreview(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.backup.import_preview_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.backup.import_preview_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-3 px-6 pb-2">
            <ul className="list-disc pl-5 text-sm">
              <li>
                {t(($) => $.backup.import_preview_skills, {
                  count: preview?.skills.length ?? 0,
                })}
              </li>
              <li>
                {t(($) => $.backup.import_preview_agents, {
                  count: preview?.agents.length ?? 0,
                })}
              </li>
              <li>
                {t(($) => $.backup.import_preview_squads, {
                  count: preview?.squads.length ?? 0,
                })}
              </li>
            </ul>

            <div className="space-y-2 pt-2">
              <div>
                <Label className="text-xs text-muted-foreground">
                  {t(($) => $.backup.import_runtime_label)}
                </Label>
                <Select value={runtimeId} onValueChange={(v) => { if (v) setRuntimeId(v); }}>
                  <SelectTrigger className="mt-1">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {runtimes.map((rt) => (
                      <SelectItem key={rt.id} value={rt.id}>
                        {rt.name ?? rt.id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="flex items-center gap-2">
                <Checkbox
                  id="overwrite"
                  checked={overwrite}
                  onCheckedChange={(v) => setOverwrite(v === true)}
                />
                <Label htmlFor="overwrite" className="text-xs">
                  {t(($) => $.backup.import_overwrite_label)}
                </Label>
              </div>
            </div>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.backup.import_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleImport}
              disabled={importing || !runtimeId}
            >
              {importing ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : null}
              {importing
                ? t(($) => $.backup.importing)
                : t(($) => $.backup.import_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
