"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useWorkspacePaths } from "@rimedeck/core/paths";
import { workspaceKeys, sopTemplateListOptions } from "@rimedeck/core/workspace/queries";
import { api } from "@rimedeck/core/api";
import type { SOPTemplate } from "@rimedeck/core/types";
import { Skeleton } from "@rimedeck/ui/components/ui/skeleton";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Category tabs
// ---------------------------------------------------------------------------

const CATEGORY_KEYS = ["all", "document", "scraper", "sales", "data"] as const;

type CategoryKey = (typeof CATEGORY_KEYS)[number];

// ---------------------------------------------------------------------------
// Template card
// ---------------------------------------------------------------------------

const CATEGORY_ICONS: Record<string, string> = {
  document: "📄",
  scraper: "🕷️",
  sales: "💼",
  data: "📊",
};

function TemplateCard({
  template,
  onSelect,
  busy,
}: {
  template: SOPTemplate;
  onSelect: (t: SOPTemplate) => void;
  busy: boolean;
}) {
  const { t } = useT("sops");
  return (
    <button
      type="button"
      disabled={busy}
      className="flex items-start gap-3 rounded-lg border p-4 text-left transition-colors hover:bg-muted/50 disabled:opacity-60"
      onClick={() => onSelect(template)}
    >
      <span className="mt-0.5 text-xl leading-none shrink-0">
        {CATEGORY_ICONS[template.category] ?? "⚙️"}
      </span>
      <div className="flex flex-1 flex-col gap-1 min-w-0">
        <span className="truncate text-sm font-medium">{template.name}</span>
        <p className="line-clamp-2 text-xs text-muted-foreground">
          {template.description}
        </p>
        <div className="flex items-center gap-2 text-[11px] text-muted-foreground/70">
          <span>{t(($) => $.run.node_count, { count: template.node_count })}</span>
          {template.tags.length > 0 && (
            <>
              <span>·</span>
              <span className="truncate">{template.tags.join(", ")}</span>
            </>
          )}
        </div>
      </div>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Gallery
// ---------------------------------------------------------------------------

export function TemplateGallery({ onClose }: { onClose?: () => void }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const { t } = useT("sops");

  const [category, setCategory] = useState<CategoryKey>("all");
  const [cloning, setCloning] = useState<string | null>(null);

  const { data: templates = [], isLoading } = useQuery(
    sopTemplateListOptions(wsId),
  );

  const filtered =
    category === "all"
      ? templates
      : templates.filter((tmpl) => tmpl.category === category);

  async function handleSelect(tmpl: SOPTemplate) {
    if (cloning) return;
    setCloning(tmpl.id);
    try {
      const sop = await api.cloneSOPTemplate(tmpl.id);
      queryClient.invalidateQueries({
        queryKey: workspaceKeys.sops(wsId),
      });
      toast.success(t(($) => $.template_gallery.created_from_template, { name: sop.name }));
      onClose?.();
      navigation.push(paths.sopDetail(sop.id));
    } catch {
      toast.error(t(($) => $.template_gallery.create_failed));
    } finally {
      setCloning(null);
    }
  }

  // --- Loading skeleton ---
  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <div className="flex gap-1">
          {CATEGORY_KEYS.map((key) => (
            <Skeleton key={key} className="h-7 w-16 rounded-md" />
          ))}
        </div>
        <div className="flex flex-col gap-3">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-20 rounded-lg" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Category tabs */}
      <div className="flex gap-1 overflow-x-auto">
        {CATEGORY_KEYS.map((key) => (
          <button
            key={key}
            type="button"
            className={`shrink-0 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
              category === key
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:bg-muted"
            }`}
            onClick={() => setCategory(key)}
          >
            {t(($) => $.categories[key])}
          </button>
        ))}
      </div>

      {/* Grid */}
      {filtered.length === 0 ? (
        <p className="py-8 text-center text-sm text-muted-foreground">
          {t(($) => $.template_gallery.no_templates)}
        </p>
      ) : (
        <div className="flex flex-col gap-3">
          {filtered.map((tmpl) => (
            <TemplateCard
              key={tmpl.id}
              template={tmpl}
              onSelect={handleSelect}
              busy={cloning === tmpl.id}
            />
          ))}
        </div>
      )}

      {/* Inline loading indicator for clone */}
      {cloning && (
        <div className="flex items-center justify-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="size-3.5 animate-spin" />
          <span>{t(($) => $.template_gallery.creating)}</span>
        </div>
      )}
    </div>
  );
}
