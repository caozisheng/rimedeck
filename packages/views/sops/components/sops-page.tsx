"use client";

import { useMemo, useState } from "react";
import {
  Workflow,
  Plus,
  FileText,
  LayoutTemplate,
  Upload,
  Search,
  AlertCircle,
} from "lucide-react";
import type { SOPSummary } from "@rimedeck/core/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getCoreRowModel, useReactTable } from "@tanstack/react-table";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useWorkspacePaths } from "@rimedeck/core/paths";
import {
  agentListOptions,
  sopListOptions,
  workspaceKeys,
  selectSOPAssignments,
} from "@rimedeck/core/workspace/queries";
import { api } from "@rimedeck/core/api";
import { Button } from "@rimedeck/ui/components/ui/button";
import { DataTable } from "@rimedeck/ui/components/ui/data-table";
import { Input } from "@rimedeck/ui/components/ui/input";
import { Skeleton } from "@rimedeck/ui/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@rimedeck/ui/components/ui/dialog";
import { toast } from "sonner";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { TemplateGallery } from "./template-gallery";
import { ImportDialog } from "./import-dialog";
import {
  useSOPColumns,
  type SOPRow,
} from "./sop-columns";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type StatusFilter = "all" | "draft" | "published" | "archived";

const STATUS_FILTER_KEYS: StatusFilter[] = ["all", "draft", "published", "archived"];

// ---------------------------------------------------------------------------
// Page header
// ---------------------------------------------------------------------------

function PageHeaderBar({
  totalCount,
  onCreateClick,
}: {
  totalCount: number;
  onCreateClick: () => void;
}) {
  const { t: tLayout } = useT("layout");
  const { t } = useT("sops");
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <Workflow className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{tLayout(($) => $.nav.sops)}</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
      </div>
      <Button type="button" size="sm" onClick={onCreateClick}>
        <Plus className="h-3 w-3" />
        {t(($) => $.page.create)}
      </Button>
    </PageHeader>
  );
}

// ---------------------------------------------------------------------------
// Card toolbar — search + status filters
// ---------------------------------------------------------------------------

function CardToolbar({
  search,
  setSearch,
  filter,
  setFilter,
}: {
  search: string;
  setSearch: (v: string) => void;
  filter: StatusFilter;
  setFilter: (v: StatusFilter) => void;
}) {
  const { t } = useT("sops");
  return (
    <div className="flex h-auto shrink-0 flex-col gap-2 border-b px-3 py-3 sm:h-12 sm:flex-row sm:items-center sm:px-4 sm:py-0">
      <div className="relative flex-1 sm:max-w-xs">
        <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          placeholder={t(($) => $.page.search_placeholder)}
          className="h-8 pl-8 text-sm"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>
      <div className="flex gap-1">
        {STATUS_FILTER_KEYS.map((key) => (
          <button
            key={key}
            type="button"
            onClick={() => setFilter(key)}
            className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
              filter === key
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:bg-muted"
            }`}
          >
            {t(($) => $.page.filters[key])}
          </button>
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

function EmptyState({ onCreate }: { onCreate: () => void }) {
  const { t } = useT("sops");
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <Workflow className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">{t(($) => $.page.empty.title)}</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        {t(($) => $.page.empty.description)}
      </p>
      <Button type="button" size="sm" className="mt-4" onClick={onCreate}>
        <Plus className="h-3 w-3" />
        {t(($) => $.page.create)}
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Delete confirmation dialog
// ---------------------------------------------------------------------------

function DeleteDialog({
  sop,
  open,
  onOpenChange,
  onConfirm,
  deleting,
}: {
  sop: SOPSummary | null;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onConfirm: () => void;
  deleting: boolean;
}) {
  const { t } = useT("sops");
  if (!sop) return null;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.delete_dialog.title)}</DialogTitle>
          <DialogDescription
            dangerouslySetInnerHTML={{
              __html: t(($) => $.delete_dialog.description, { name: sop.name }),
            }}
          />
        </DialogHeader>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={deleting}
          >
            {t(($) => $.delete_dialog.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            onClick={onConfirm}
            disabled={deleting}
          >
            {deleting ? t(($) => $.delete_dialog.deleting) : t(($) => $.delete_dialog.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function SOPsPage() {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const { t } = useT("sops");

  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [createOpen, setCreateOpen] = useState(false);
  const [templateOpen, setTemplateOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [creatingBlank, setCreatingBlank] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SOPSummary | null>(null);
  const [deleting, setDeleting] = useState(false);

  const {
    data: sops = [],
    isLoading,
    error: listError,
    refetch,
  } = useQuery(sopListOptions(wsId));

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const assignments = useMemo(() => selectSOPAssignments(agents), [agents]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return sops.filter((wf) => {
      if (statusFilter !== "all" && wf.status !== statusFilter) return false;
      if (
        q &&
        !wf.name.toLowerCase().includes(q) &&
        !(wf.description ?? "").toLowerCase().includes(q)
      )
        return false;
      return true;
    });
  }, [sops, search, statusFilter]);

  const sopRows = useMemo<SOPRow[]>(() => {
    return filtered.map((wf) => ({
      sop: wf,
      agents: assignments.get(wf.id) ?? [],
    }));
  }, [filtered, assignments]);

  const columns = useSOPColumns((wf) => setDeleteTarget(wf));

  const table = useReactTable({
    data: sopRows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    enableColumnResizing: true,
  });

  async function handleCreateBlank() {
    setCreatingBlank(true);
    try {
      const wf = await api.createSOP({ name: t(($) => $.page.untitled_sop) });
      queryClient.invalidateQueries({
        queryKey: workspaceKeys.sops(wsId),
      });
      setCreateOpen(false);
      navigation.push(paths.sopDetail(wf.id));
    } catch {
      toast.error(t(($) => $.toast.create_failed));
    } finally {
      setCreatingBlank(false);
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteSOP(deleteTarget.id);
      queryClient.invalidateQueries({
        queryKey: workspaceKeys.sops(wsId),
      });
      toast.success(t(($) => $.toast.deleted, { name: deleteTarget.name }));
      setDeleteTarget(null);
    } catch {
      toast.error(t(($) => $.toast.delete_failed));
    } finally {
      setDeleting(false);
    }
  }

  // --- Loading ---
  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeaderBar totalCount={0} onCreateClick={() => setCreateOpen(true)} />
        <div className="flex flex-1 min-h-0 flex-col gap-4 p-3 sm:p-6">
          <div className="flex flex-1 min-h-0 flex-col overflow-hidden rounded-lg border">
            <div className="flex h-auto shrink-0 flex-col gap-2 border-b px-3 py-3 sm:h-12 sm:flex-row sm:items-center sm:px-4 sm:py-0">
              <Skeleton className="h-8 w-full rounded-md sm:w-64" />
              <Skeleton className="h-7 w-12 rounded-md" />
              <Skeleton className="h-7 w-16 rounded-md" />
              <Skeleton className="h-7 w-20 rounded-md" />
            </div>
            <div className="space-y-2 p-4">
              {Array.from({ length: 4 }, (_, i) => (
                <Skeleton key={i} className="h-14 w-full rounded-md" />
              ))}
            </div>
          </div>
        </div>
      </div>
    );
  }

  // --- Error ---
  if (listError) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeaderBar totalCount={0} onCreateClick={() => setCreateOpen(true)} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <p className="text-sm font-medium">{t(($) => $.page.load_failed)}</p>
          <p className="mt-1 text-xs text-muted-foreground">
            {listError instanceof Error ? listError.message : t(($) => $.page.unknown_error)}
          </p>
          <Button type="button" variant="outline" size="sm" onClick={() => refetch()}>
            {t(($) => $.page.retry)}
          </Button>
        </div>
      </div>
    );
  }

  const totalCount = sops.length;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeaderBar
        totalCount={totalCount}
        onCreateClick={() => setCreateOpen(true)}
      />

      <div className="flex flex-1 min-h-0 flex-col gap-4 p-3 sm:p-6">
        {totalCount === 0 ? (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState onCreate={() => setCreateOpen(true)} />
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 flex-col overflow-hidden rounded-lg border bg-background">
            <CardToolbar
              search={search}
              setSearch={setSearch}
              filter={statusFilter}
              setFilter={setStatusFilter}
            />
            {filtered.length === 0 ? (
              <div className="flex flex-1 flex-col items-center justify-center gap-2 px-4 py-16 text-center text-muted-foreground">
                <Search className="h-8 w-8 text-muted-foreground/40" />
                <p className="text-sm">{t(($) => $.page.no_matches.title)}</p>
                <p className="max-w-xs text-xs">
                  {search
                    ? t(($) => $.page.no_matches.with_query, {
                        query: search,
                        filterSuffix:
                          statusFilter !== "all"
                            ? t(($) => $.page.no_matches.with_query_filter_suffix, { status: statusFilter })
                            : "",
                      })
                    : t(($) => $.page.no_matches.filter_only, { status: statusFilter })}
                  {t(($) => $.page.no_matches.try_different)}
                </p>
              </div>
            ) : (
              <DataTable
                table={table}
                onRowClick={(row) =>
                  navigation.push(paths.sopDetail(row.original.sop.id))
                }
              />
            )}
          </div>
        )}
      </div>

      {/* Delete confirmation */}
      <DeleteDialog
        sop={deleteTarget}
        open={deleteTarget !== null}
        onOpenChange={(v) => {
          if (!v) setDeleteTarget(null);
        }}
        onConfirm={handleDelete}
        deleting={deleting}
      />

      {/* Create choice dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{t(($) => $.create_dialog.title)}</DialogTitle>
          </DialogHeader>
          <div className="mt-2 flex flex-col gap-2">
            <button
              type="button"
              disabled={creatingBlank}
              className="flex items-center gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-muted/50 disabled:opacity-60"
              onClick={handleCreateBlank}
            >
              <FileText className="size-5 shrink-0 text-muted-foreground" />
              <div>
                <p className="text-sm font-medium">{t(($) => $.create_dialog.blank)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.create_dialog.blank_description)}
                </p>
              </div>
            </button>
            <button
              type="button"
              className="flex items-center gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-muted/50"
              onClick={() => {
                setCreateOpen(false);
                setTemplateOpen(true);
              }}
            >
              <LayoutTemplate className="size-5 shrink-0 text-muted-foreground" />
              <div>
                <p className="text-sm font-medium">{t(($) => $.create_dialog.from_template)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.create_dialog.from_template_description)}
                </p>
              </div>
            </button>
            <button
              type="button"
              className="flex items-center gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-muted/50"
              onClick={() => {
                setCreateOpen(false);
                setImportOpen(true);
              }}
            >
              <Upload className="size-5 shrink-0 text-muted-foreground" />
              <div>
                <p className="text-sm font-medium">{t(($) => $.create_dialog.import)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.create_dialog.import_description)}
                </p>
              </div>
            </button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Template gallery dialog */}
      <Dialog open={templateOpen} onOpenChange={setTemplateOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t(($) => $.template_gallery.title)}</DialogTitle>
          </DialogHeader>
          <div className="mt-2">
            <TemplateGallery onClose={() => setTemplateOpen(false)} />
          </div>
        </DialogContent>
      </Dialog>

      {/* Import dialog */}
      <ImportDialog open={importOpen} onOpenChange={setImportOpen} />
    </div>
  );
}
