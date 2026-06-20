"use client";

import { useState, useMemo, type DragEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useWorkspacePaths } from "@rimedeck/core/paths";
import { workspaceKeys } from "@rimedeck/core/workspace/queries";
import { api } from "@rimedeck/core/api";
import type { ImportWarning } from "@rimedeck/core/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@rimedeck/ui/components/ui/dialog";
import { Button } from "@rimedeck/ui/components/ui/button";
import { Input } from "@rimedeck/ui/components/ui/input";
import { Textarea } from "@rimedeck/ui/components/ui/textarea";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@rimedeck/ui/components/ui/tabs";
import {
  Upload,
  ClipboardPaste,
  Globe,
  Loader2,
  AlertTriangle,
  CheckCircle2,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// n8n node type → RuleGo mapping table (from context)
// ---------------------------------------------------------------------------

const N8N_NODE_MAP: Record<string, { ruleGo: string; status: "mapped" | "degraded" }> = {
  "n8n-nodes-base.httpRequest":   { ruleGo: "restApiCall",  status: "mapped" },
  "n8n-nodes-base.code":          { ruleGo: "jsTransform",  status: "mapped" },
  "n8n-nodes-base.function":      { ruleGo: "jsTransform",  status: "mapped" },
  "n8n-nodes-base.if":            { ruleGo: "jsFilter",     status: "mapped" },
  "n8n-nodes-base.set":           { ruleGo: "jsTransform",  status: "mapped" },
  "n8n-nodes-base.rssFeedRead":   { ruleGo: "rssFetch",     status: "mapped" },
  "n8n-nodes-base.emailSend":     { ruleGo: "sendEmail",    status: "mapped" },
};

/** Node types that are silently skipped (start triggers / no-ops). */
const N8N_SKIP: Record<string, true> = {
  "n8n-nodes-base.webhook": true,
  "n8n-nodes-base.noOp": true,
};

interface NodeMapping {
  name: string;
  n8nType: string;
  ruleGoType: string | null;
  status: "mapped" | "degraded" | "skipped" | "unsupported";
}

function classifyN8nNodes(raw: string): NodeMapping[] {
  try {
    const parsed = JSON.parse(raw);
    const nodes = parsed.nodes as Array<{ name?: string; type?: string }> | undefined;
    if (!Array.isArray(nodes)) return [];
    return nodes.map((n) => {
      const type = n.type ?? "";
      const name = n.name ?? type;
      if (type in N8N_SKIP) {
        return { name, n8nType: type, ruleGoType: null, status: "skipped" as const };
      }
      const entry = N8N_NODE_MAP[type];
      if (entry) {
        return { name, n8nType: type, ruleGoType: entry.ruleGo, status: entry.status };
      }
      // LangChain nodes → agentLLM (degraded)
      if (type.startsWith("@n8n/n8n-nodes-langchain.")) {
        return { name, n8nType: type, ruleGoType: "agentLLM", status: "degraded" as const };
      }
      // Unknown → restApiCall fallback (degraded)
      return { name, n8nType: type, ruleGoType: "restApiCall", status: "unsupported" as const };
    });
  } catch {
    return [];
  }
}

// ---------------------------------------------------------------------------
// Format detection
// ---------------------------------------------------------------------------

function detectFormat(raw: string): "n8n" | "dify" | "unknown" {
  const trimmed = raw.trim();
  if (trimmed.startsWith("{")) {
    try {
      const obj = JSON.parse(trimmed);
      if (obj.nodes && obj.connections) return "n8n";
    } catch {
      // fall through
    }
  }
  if (/^(app|workflow)\s*:/m.test(trimmed)) return "dify";
  return "unknown";
}

// ---------------------------------------------------------------------------
// Status icon
// ---------------------------------------------------------------------------

const STATUS_ICON = {
  mapped:      <CheckCircle2 className="size-3.5 shrink-0 text-green-500" />,
  degraded:    <AlertTriangle className="size-3.5 shrink-0 text-yellow-500" />,
  unsupported: <XCircle className="size-3.5 shrink-0 text-red-500" />,
  skipped:     <XCircle className="size-3.5 shrink-0 text-muted-foreground" />,
} as const;

// ---------------------------------------------------------------------------
// Import preview with per-node mapping
// ---------------------------------------------------------------------------

function ImportPreview({
  raw,
  format,
  onImport,
  importing,
}: {
  raw: string;
  format: "n8n" | "dify" | "unknown";
  onImport: () => void;
  importing: boolean;
}) {
  const { t } = useT("sops");
  const mappings = useMemo(
    () => (format === "n8n" ? classifyN8nNodes(raw) : []),
    [raw, format],
  );

  const mapped = mappings.filter((m) => m.status === "mapped").length;
  const degraded = mappings.filter((m) => m.status === "degraded").length;
  const unsupported = mappings.filter((m) => m.status === "unsupported").length;
  const skipped = mappings.filter((m) => m.status === "skipped").length;

  return (
    <div className="flex flex-col gap-3">
      {/* Format badge */}
      <div className="flex items-center gap-2 rounded-md border bg-muted/30 px-3 py-2 text-xs">
        <CheckCircle2 className="size-3.5 shrink-0 text-green-500" />
        <span>
          {t(($) => $.import_dialog.detected_format)}{" "}
          <strong>
            {format === "n8n" ? "n8n" : format === "dify" ? "Dify" : t(($) => $.page.unknown_error)}
          </strong>
        </span>
      </div>

      {/* Per-node mapping summary */}
      {mappings.length > 0 && (
        <>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            {mapped > 0 && (
              <span className="flex items-center gap-1">
                {STATUS_ICON.mapped} {t(($) => $.import_dialog.mapped, { count: mapped })}
              </span>
            )}
            {degraded > 0 && (
              <span className="flex items-center gap-1">
                {STATUS_ICON.degraded} {t(($) => $.import_dialog.degraded, { count: degraded })}
              </span>
            )}
            {unsupported > 0 && (
              <span className="flex items-center gap-1">
                {STATUS_ICON.unsupported} {t(($) => $.import_dialog.unsupported, { count: unsupported })}
              </span>
            )}
            {skipped > 0 && (
              <span className="flex items-center gap-1">
                {STATUS_ICON.skipped} {t(($) => $.import_dialog.skipped, { count: skipped })}
              </span>
            )}
          </div>

          {/* Per-node detail list */}
          <ul className="max-h-40 space-y-0.5 overflow-y-auto rounded-md border bg-muted/20 p-2">
            {mappings.map((m, i) => (
              <li key={i} className="flex items-center gap-1.5 text-xs">
                {STATUS_ICON[m.status]}
                <span className="font-medium">{m.name}</span>
                {m.ruleGoType && (
                  <span className="text-muted-foreground">→ {m.ruleGoType}</span>
                )}
              </li>
            ))}
          </ul>
        </>
      )}

      {format === "dify" && (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.import_dialog.dify_note)}
        </p>
      )}

      <Button size="sm" onClick={onImport} disabled={importing}>
        {importing && <Loader2 className="mr-1.5 size-3.5 animate-spin" />}
        {t(($) => $.import_dialog.import_edit)}
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Post-import warnings
// ---------------------------------------------------------------------------

function PostImportWarnings({ warnings }: { warnings: ImportWarning[] }) {
  const { t } = useT("sops");
  if (warnings.length === 0) return null;
  return (
    <div className="flex flex-col gap-1.5">
      <p className="text-xs font-medium text-muted-foreground">
        {t(($) => $.import_dialog.warnings_title)}
      </p>
      <ul className="max-h-36 space-y-1 overflow-y-auto">
        {warnings.map((w, i) => (
          <li key={i} className="flex items-start gap-1.5 text-xs">
            {STATUS_ICON[w.type]}
            <span>
              <strong>{w.node_name}</strong>: {w.message}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// ---------------------------------------------------------------------------
// File reader helper
// ---------------------------------------------------------------------------

function readFileAsText(file: File, onDone: (text: string) => void) {
  const reader = new FileReader();
  reader.onload = () => {
    if (typeof reader.result === "string") onDone(reader.result);
  };
  reader.readAsText(file);
}

// ---------------------------------------------------------------------------
// Dialog
// ---------------------------------------------------------------------------

export function ImportDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const { t } = useT("sops");

  const [raw, setRaw] = useState("");
  const [format, setFormat] = useState<"n8n" | "dify" | "unknown" | null>(null);
  const [importing, setImporting] = useState(false);
  const [warnings, setWarnings] = useState<ImportWarning[]>([]);
  const [urlValue, setUrlValue] = useState("");
  const [fetchingUrl, setFetchingUrl] = useState(false);
  const [dragOver, setDragOver] = useState(false);

  function reset() {
    setRaw("");
    setFormat(null);
    setWarnings([]);
    setUrlValue("");
    setFetchingUrl(false);
    setImporting(false);
    setDragOver(false);
  }

  function loadContent(content: string) {
    setRaw(content);
    setFormat(detectFormat(content));
    setWarnings([]);
  }

  function handleFiles(files: FileList | null) {
    if (!files?.length) return;
    const file = files.item(0);
    if (!file) return;
    readFileAsText(file, loadContent);
  }

  function handleDrop(e: DragEvent) {
    e.preventDefault();
    setDragOver(false);
    handleFiles(e.dataTransfer.files);
  }

  async function handleFetchUrl() {
    if (!urlValue.trim()) return;
    setFetchingUrl(true);
    try {
      const res = await fetch(urlValue.trim());
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const text = await res.text();
      loadContent(text);
    } catch {
      toast.error(t(($) => $.import_dialog.fetch_failed));
    } finally {
      setFetchingUrl(false);
    }
  }

  async function handleImport() {
    if (!raw) return;
    setImporting(true);
    try {
      const result = await api.importSOP(raw);
      setWarnings(result.warnings);
      queryClient.invalidateQueries({
        queryKey: workspaceKeys.sops(wsId),
      });
      toast.success(t(($) => $.import_dialog.imported, { name: result.sop.name }));
      onOpenChange(false);
      reset();
      navigation.push(paths.sopDetail(result.sop.id));
    } catch {
      toast.error(t(($) => $.import_dialog.import_failed));
    } finally {
      setImporting(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
        if (!v) reset();
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.import_dialog.title)}</DialogTitle>
        </DialogHeader>

        <Tabs defaultValue="upload" className="mt-2">
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="upload" className="gap-1.5 text-xs">
              <Upload className="size-3.5" /> {t(($) => $.import_dialog.tab_upload)}
            </TabsTrigger>
            <TabsTrigger value="paste" className="gap-1.5 text-xs">
              <ClipboardPaste className="size-3.5" /> {t(($) => $.import_dialog.tab_paste)}
            </TabsTrigger>
            <TabsTrigger value="url" className="gap-1.5 text-xs">
              <Globe className="size-3.5" /> {t(($) => $.import_dialog.tab_url)}
            </TabsTrigger>
          </TabsList>

          {/* Upload File */}
          <TabsContent value="upload" className="mt-4 space-y-3">
            <div
              className={`flex min-h-[120px] cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed px-4 py-6 text-center transition-colors ${
                dragOver
                  ? "border-primary bg-primary/5"
                  : "border-muted-foreground/25 hover:border-muted-foreground/50"
              }`}
              onDragOver={(e) => {
                e.preventDefault();
                setDragOver(true);
              }}
              onDragLeave={() => setDragOver(false)}
              onDrop={handleDrop}
              onClick={() => {
                const input = document.createElement("input");
                input.type = "file";
                input.accept = ".json,.yml,.yaml";
                input.onchange = () => handleFiles(input.files);
                input.click();
              }}
            >
              <Upload className="size-6 text-muted-foreground" />
              <p
                className="text-xs text-muted-foreground"
                dangerouslySetInnerHTML={{ __html: t(($) => $.import_dialog.drop_hint) }}
              />
            </div>
          </TabsContent>

          {/* Paste */}
          <TabsContent value="paste" className="mt-4 space-y-3">
            <Textarea
              placeholder={t(($) => $.import_dialog.paste_placeholder)}
              className="min-h-[140px] font-mono text-xs"
              value={raw}
              onChange={(e) => loadContent(e.target.value)}
            />
          </TabsContent>

          {/* From URL */}
          <TabsContent value="url" className="mt-4 space-y-3">
            <div className="flex gap-2">
              <Input
                placeholder={t(($) => $.import_dialog.url_placeholder)}
                className="flex-1 text-xs"
                value={urlValue}
                onChange={(e) => setUrlValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleFetchUrl();
                }}
              />
              <Button
                size="sm"
                variant="secondary"
                disabled={fetchingUrl || !urlValue.trim()}
                onClick={handleFetchUrl}
              >
                {fetchingUrl ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  t(($) => $.import_dialog.fetch)
                )}
              </Button>
            </div>
          </TabsContent>
        </Tabs>

        {/* Preview / Import */}
        {raw && format && (
          <div className="mt-4 space-y-3 border-t pt-4">
            <ImportPreview
              raw={raw}
              format={format}
              onImport={handleImport}
              importing={importing}
            />
            <PostImportWarnings warnings={warnings} />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
