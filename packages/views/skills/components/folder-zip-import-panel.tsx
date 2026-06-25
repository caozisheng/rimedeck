"use client";

import { useRef, useState } from "react";
import {
  AlertCircle,
  Archive,
  CheckCircle2,
  Download,
  FolderOpen,
  Loader2,
  XCircle,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@rimedeck/core/api";
import type { Skill } from "@rimedeck/core/types";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { workspaceKeys, skillDetailOptions } from "@rimedeck/core/workspace/queries";
import { Button } from "@rimedeck/ui/components/ui/button";
import { Badge } from "@rimedeck/ui/components/ui/badge";
import { Checkbox } from "@rimedeck/ui/components/ui/checkbox";
import { Input } from "@rimedeck/ui/components/ui/input";
import { Label } from "@rimedeck/ui/components/ui/label";
import { Progress } from "@rimedeck/ui/components/ui/progress";
import { Textarea } from "@rimedeck/ui/components/ui/textarea";
import { useScrollFade } from "@rimedeck/ui/hooks/use-scroll-fade";
import {
  scanSkillFolder,
  scanSkillZip,
  readSkillBundle,
  type ScannedSkillEntry,
} from "../../platform";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type ImportStatus = "created" | "failed" | "skipped";

interface ImportResult {
  key: string;
  name: string;
  status: ImportStatus;
  error?: string;
  skill?: Skill;
}

type Phase = "idle" | "scanning" | "scanned" | "importing" | "done";

const IMPORT_CONCURRENCY = 5;

// ---------------------------------------------------------------------------
// Result icon (reused from runtime import panel pattern)
// ---------------------------------------------------------------------------

function ResultIcon({ status }: { status: ImportStatus }) {
  switch (status) {
    case "created":
      return <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />;
    case "failed":
      return <XCircle className="h-3.5 w-3.5 text-destructive" />;
    case "skipped":
      return <AlertCircle className="h-3.5 w-3.5 text-muted-foreground" />;
  }
}

// ---------------------------------------------------------------------------
// Skill item with checkbox
// ---------------------------------------------------------------------------

function ScannedSkillItem({
  skill,
  checked,
  onToggle,
  disabled,
  expanded,
  editName,
  editDescription,
  onNameChange,
  onDescriptionChange,
}: {
  skill: ScannedSkillEntry;
  checked: boolean;
  onToggle: () => void;
  disabled?: boolean;
  expanded?: boolean;
  editName?: string;
  editDescription?: string;
  onNameChange?: (v: string) => void;
  onDescriptionChange?: (v: string) => void;
}) {
  const { t } = useT("skills");
  return (
    <div
      className={`overflow-hidden rounded-lg border transition-colors ${
        checked ? "border-primary bg-primary/5" : "hover:bg-accent/40"
      } ${disabled ? "pointer-events-none opacity-60" : ""}`}
    >
      <div
        role="button"
        tabIndex={disabled ? -1 : 0}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggle();
          }
        }}
        className="flex w-full items-start gap-3 px-4 py-3 text-left"
      >
        <Checkbox
          checked={checked}
          tabIndex={-1}
          className="pointer-events-none mt-0.5"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-medium">{skill.name}</span>
          </div>
          {skill.description && (
            <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
              {skill.description}
            </p>
          )}
          <p className="mt-1 truncate font-mono text-xs text-muted-foreground">
            {skill.dirPath}
          </p>
        </div>
        {skill.fileCount > 0 && (
          <Badge variant="outline" className="shrink-0">
            {t(($) => $.runtime_import.skill_files, { count: skill.fileCount })}
          </Badge>
        )}
      </div>

      {expanded && (
        <div className="space-y-2.5 border-t bg-card px-4 py-3">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.runtime_import.skill_name_label)}
            </Label>
            <Input
              value={editName ?? ""}
              onChange={(e) => onNameChange?.(e.target.value)}
              placeholder={skill.name}
              className="h-8 text-sm"
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.runtime_import.skill_description_label)}
            </Label>
            <Textarea
              value={editDescription ?? ""}
              onChange={(e) => onDescriptionChange?.(e.target.value)}
              placeholder={t(($) => $.runtime_import.skill_description_placeholder)}
              rows={2}
              className="resize-none text-sm"
            />
          </div>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Import summary
// ---------------------------------------------------------------------------

function ImportSummary({ results }: { results: ImportResult[] }) {
  const { t } = useT("skills");
  const created = results.filter((r) => r.status === "created").length;
  const failed = results.filter((r) => r.status === "failed").length;
  const skipped = results.filter((r) => r.status === "skipped").length;

  return (
    <div className="space-y-3">
      <div className="flex gap-4 text-sm">
        {created > 0 && (
          <span className="flex items-center gap-1.5 text-green-600">
            <CheckCircle2 className="h-3.5 w-3.5" />
            {t(($) => $.runtime_import.bulk_summary_created)}: {created}
          </span>
        )}
        {skipped > 0 && (
          <span className="flex items-center gap-1.5 text-muted-foreground">
            <AlertCircle className="h-3.5 w-3.5" />
            {t(($) => $.runtime_import.bulk_summary_skipped)}: {skipped}
          </span>
        )}
        {failed > 0 && (
          <span className="flex items-center gap-1.5 text-destructive">
            <XCircle className="h-3.5 w-3.5" />
            {t(($) => $.runtime_import.bulk_summary_failed)}: {failed}
          </span>
        )}
      </div>
      <div className="max-h-48 space-y-1 overflow-y-auto">
        {results.map((r) => (
          <div
            key={r.key}
            className="flex items-center gap-2 rounded px-2 py-1 text-xs"
          >
            <ResultIcon status={r.status} />
            <span className="min-w-0 flex-1 truncate">{r.name}</span>
            {r.error && (
              <span className="shrink-0 text-destructive">{r.error}</span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Panel
// ---------------------------------------------------------------------------

export function FolderZipImportPanel({
  mode,
  onImported,
  onBulkDone,
}: {
  mode: "folder" | "zip";
  onImported?: (skill: Skill) => void;
  onBulkDone?: () => void;
}) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const [phase, setPhase] = useState<Phase>("idle");
  const [source, setSource] = useState<string>("");
  const [skills, setSkills] = useState<ScannedSkillEntry[]>([]);
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [scanError, setScanError] = useState<string>("");

  // Import state
  const [importTotal, setImportTotal] = useState(0);
  const [importCompleted, setImportCompleted] = useState(0);
  const [importResults, setImportResults] = useState<ImportResult[]>([]);
  const cancelRef = useRef(false);

  // Single-select edit fields
  const [editName, setEditName] = useState("");
  const [editDescription, setEditDescription] = useState("");

  const busy = phase === "scanning" || phase === "importing";
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  // -- Scan handler --

  const handleScan = async () => {
    setPhase("scanning");
    setScanError("");
    setSkills([]);
    setSelectedKeys(new Set());

    const result = mode === "folder" ? await scanSkillFolder() : await scanSkillZip();

    if (!result.ok) {
      if (result.reason === "cancelled") {
        setPhase("idle");
        return;
      }
      setScanError(result.error || t(($) => $.folder_import.scan_failed));
      setPhase("idle");
      return;
    }

    const found = result.skills ?? [];
    setSource(result.source ?? "");
    setSkills(found);

    if (found.length === 0) {
      setPhase("scanned");
      return;
    }

    // Auto-select all
    setSelectedKeys(new Set(found.map((s) => s.key)));
    // If only one, seed edit fields
    if (found.length === 1) {
      setEditName(found[0]!.name);
      setEditDescription(found[0]!.description);
    }
    setPhase("scanned");
  };

  // -- Selection helpers --

  const toggleSkill = (key: string) => {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      if (next.size === 1) {
        const only = skills.find((s) => next.has(s.key));
        if (only) {
          setEditName(only.name);
          setEditDescription(only.description);
        }
      }
      return next;
    });
  };

  const toggleAll = () => {
    if (selectedKeys.size === skills.length) {
      setSelectedKeys(new Set());
    } else {
      setSelectedKeys(new Set(skills.map((s) => s.key)));
    }
  };

  const allSelected = skills.length > 0 && selectedKeys.size === skills.length;
  const someSelected = selectedKeys.size > 0 && !allSelected;

  const singleSelected =
    selectedKeys.size === 1
      ? skills.find((s) => selectedKeys.has(s.key))
      : undefined;

  // -- Import handler --

  const handleImport = async () => {
    if (selectedKeys.size === 0) return;

    const toImport = skills.filter((s) => selectedKeys.has(s.key));
    const total = toImport.length;

    cancelRef.current = false;
    setPhase("importing");
    setImportTotal(total);
    setImportCompleted(0);
    setImportResults([]);

    const results: ImportResult[] = [];

    const importOne = async (entry: ScannedSkillEntry) => {
      const importName =
        total === 1 ? editName.trim() || entry.name : entry.name;
      const importDescription =
        total === 1
          ? editDescription.trim() || entry.description || undefined
          : entry.description || undefined;

      try {
        const bundleResult = await readSkillBundle(source, entry.key);
        if (!bundleResult.ok || !bundleResult.bundle) {
          results.push({
            key: entry.key,
            name: importName,
            status: "failed",
            error: bundleResult.error || "Failed to read skill bundle",
          });
          return;
        }

        const bundle = bundleResult.bundle;
        const skill = await api.createSkill({
          name: importName,
          description: importDescription ?? bundle.description,
          content: bundle.content,
          config: { origin: { type: mode === "folder" ? "folder" : "zip", path: source } },
          files: bundle.files,
        });

        qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, skill);

        results.push({
          key: entry.key,
          name: skill.name,
          status: "created",
          skill,
        });
      } catch (error) {
        const msg = error instanceof Error ? error.message : String(error);
        results.push({
          key: entry.key,
          name: importName,
          status: msg.includes("already exists") ? "skipped" : "failed",
          error: msg,
        });
      }

      setImportCompleted((prev) => prev + 1);
      setImportResults([...results]);
    };

    // Concurrent pool
    const executing = new Set<Promise<void>>();
    for (const entry of toImport) {
      if (cancelRef.current) break;
      const p = importOne(entry).then(() => {
        executing.delete(p);
      });
      executing.add(p);
      if (executing.size >= IMPORT_CONCURRENCY) {
        await Promise.race(executing);
      }
    }
    await Promise.all(executing);

    await qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
    await qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });

    setPhase("done");
  };

  const handleDone = () => {
    const succeeded = importResults.filter((r) => r.status === "created");
    if (
      importTotal === 1 &&
      succeeded.length === 1 &&
      succeeded[0]!.skill
    ) {
      onImported?.(succeeded[0]!.skill);
    } else {
      onBulkDone?.();
    }
  };

  const canImport =
    phase === "scanned" &&
    selectedKeys.size > 0 &&
    (selectedKeys.size > 1 || !!editName.trim());

  // -- Middle content --

  const middle = (() => {
    // Progress during import
    if (phase === "importing") {
      const pct = importTotal > 0 ? Math.round((importCompleted / importTotal) * 100) : 0;
      return (
        <div className="space-y-4 py-4">
          <div className="text-center">
            <Loader2 className="mx-auto h-6 w-6 animate-spin text-primary" />
            <p className="mt-3 text-sm font-medium">
              {t(($) => $.runtime_import.bulk_progress, {
                completed: importCompleted,
                total: importTotal,
              })}
            </p>
          </div>
          <Progress value={pct} />
          <div className="max-h-48 space-y-1 overflow-y-auto">
            {importResults.map((r) => (
              <div key={r.key} className="flex items-center gap-2 rounded px-2 py-1 text-xs">
                <ResultIcon status={r.status} />
                <span className="truncate">{r.name}</span>
              </div>
            ))}
          </div>
        </div>
      );
    }

    // Done summary
    if (phase === "done") {
      return <ImportSummary results={importResults} />;
    }

    // Scanning spinner
    if (phase === "scanning") {
      return (
        <div className="flex flex-col items-center py-10">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t(($) => $.folder_import.scanning)}
          </p>
        </div>
      );
    }

    // Error state
    if (scanError) {
      return (
        <div className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {scanError}
        </div>
      );
    }

    // Idle — show browse prompt
    if (phase === "idle") {
      return (
        <div className="flex flex-col items-center gap-4 py-8">
          <div className="flex h-14 w-14 items-center justify-center rounded-xl bg-muted text-muted-foreground">
            {mode === "folder" ? (
              <FolderOpen className="h-7 w-7" />
            ) : (
              <Archive className="h-7 w-7" />
            )}
          </div>
          <div className="text-center">
            <p className="text-sm font-medium">
              {mode === "folder"
                ? t(($) => $.folder_import.browse_folder_title)
                : t(($) => $.folder_import.browse_zip_title)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {t(($) => $.folder_import.browse_hint)}
            </p>
          </div>
          <Button type="button" variant="outline" onClick={handleScan}>
            {mode === "folder" ? (
              <FolderOpen className="mr-2 h-4 w-4" />
            ) : (
              <Archive className="mr-2 h-4 w-4" />
            )}
            {mode === "folder"
              ? t(($) => $.folder_import.browse_folder_button)
              : t(($) => $.folder_import.browse_zip_button)}
          </Button>
        </div>
      );
    }

    // Scanned — empty
    if (skills.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {t(($) => $.folder_import.no_skills_title)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.folder_import.no_skills_hint)}
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="mt-3"
            onClick={handleScan}
          >
            {t(($) => $.folder_import.rescan_button)}
          </Button>
        </div>
      );
    }

    // Scanned — list
    return (
      <div className="space-y-2">
        {/* Source path */}
        <div className="flex items-center gap-2 rounded-md border bg-muted/20 px-3 py-1.5 text-xs text-muted-foreground">
          {mode === "folder" ? (
            <FolderOpen className="h-3.5 w-3.5 shrink-0" />
          ) : (
            <Archive className="h-3.5 w-3.5 shrink-0" />
          )}
          <span className="min-w-0 flex-1 truncate font-mono">{source}</span>
          <Badge variant="secondary">
            {t(($) => $.folder_import.found_count, { count: skills.length })}
          </Badge>
        </div>

        {/* Select all */}
        {skills.length > 1 && (
          <div className="flex items-center gap-2 px-1">
            <Checkbox
              checked={allSelected}
              indeterminate={someSelected}
              onCheckedChange={toggleAll}
            />
            <span className="text-xs text-muted-foreground">
              {t(($) => $.runtime_import.select_all, { count: skills.length })}
            </span>
          </div>
        )}

        {/* Skill list */}
        {skills.map((skill) => (
          <ScannedSkillItem
            key={skill.key}
            skill={skill}
            checked={selectedKeys.has(skill.key)}
            onToggle={() => toggleSkill(skill.key)}
            disabled={busy}
            expanded={selectedKeys.size === 1 && selectedKeys.has(skill.key)}
            editName={selectedKeys.size === 1 && selectedKeys.has(skill.key) ? editName : undefined}
            editDescription={
              selectedKeys.size === 1 && selectedKeys.has(skill.key) ? editDescription : undefined
            }
            onNameChange={setEditName}
            onDescriptionChange={setEditDescription}
          />
        ))}
      </div>
    );
  })();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Scrollable middle */}
      <div
        ref={scrollRef}
        style={fadeStyle}
        aria-disabled={busy || undefined}
        className={`min-h-0 flex-1 overflow-y-auto px-5 py-3 ${
          busy ? "pointer-events-none opacity-60" : ""
        }`}
      >
        {middle}
      </div>

      {/* Sticky bottom */}
      {phase !== "idle" && phase !== "scanning" && (
        <div className="flex shrink-0 items-center gap-3 border-t bg-muted/30 px-5 py-3">
          {phase === "done" ? (
            <>
              <div className="min-w-0 flex-1 text-xs text-muted-foreground">
                {t(($) => $.runtime_import.bulk_complete_hint)}
              </div>
              <Button type="button" size="sm" onClick={handleDone}>
                {t(($) => $.runtime_import.bulk_done_button)}
              </Button>
            </>
          ) : phase === "importing" ? (
            <>
              <div className="min-w-0 flex-1 text-xs text-muted-foreground">
                {t(($) => $.runtime_import.bulk_progress, {
                  completed: importCompleted,
                  total: importTotal,
                })}
              </div>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => {
                  cancelRef.current = true;
                }}
              >
                {t(($) => $.runtime_import.bulk_cancel_button)}
              </Button>
            </>
          ) : (
            <>
              <div className="min-w-0 flex-1 text-xs text-muted-foreground">
                {singleSelected ? (
                  <>
                    {t(($) => $.runtime_import.ready)}{" "}
                    <span className="font-medium text-foreground">
                      {editName.trim() || singleSelected.name}
                    </span>{" "}
                    {t(($) => $.runtime_import.into_workspace)}
                  </>
                ) : selectedKeys.size > 1 ? (
                  t(($) => $.runtime_import.bulk_ready, {
                    count: selectedKeys.size,
                  })
                ) : (
                  t(($) => $.runtime_import.select_skill)
                )}
              </div>
              <Button
                type="button"
                size="sm"
                onClick={handleImport}
                disabled={!canImport}
              >
                <Download className="h-3 w-3" />
                {selectedKeys.size > 1
                  ? t(($) => $.runtime_import.bulk_import_button, {
                      count: selectedKeys.size,
                    })
                  : t(($) => $.runtime_import.import_button)}
              </Button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
