"use client";

import { useState, useEffect } from "react";
import { Check, Copy, Globe, Wifi } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@rimedeck/core/api";
import type { NetworkAddress } from "@rimedeck/core/types";
import { copyText } from "@rimedeck/ui/lib/clipboard";
import { cn } from "@rimedeck/ui/lib/utils";
import { CODE_LIGATURE_CLASS } from "@rimedeck/ui/lib/code-style";
import { useT } from "../i18n";

function formatAddress(addr: NetworkAddress, port: number): string {
  if (addr.domain) return `${addr.domain}:${port}`;
  return `${addr.ip}:${port}`;
}

function formatCopyUrl(addr: NetworkAddress, port: number): string {
  if (addr.domain) return `http://${addr.domain}:${port}`;
  return `http://${addr.ip}:${port}`;
}

function AddressCopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(t);
  }, [copied]);

  return (
    <button
      type="button"
      onClick={() => void copyText(text).then((ok) => ok && setCopied(true))}
      className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {copied ? (
        <Check className="h-3 w-3 text-success" aria-hidden />
      ) : (
        <Copy className="h-3 w-3" aria-hidden />
      )}
    </button>
  );
}

const TYPE_LABELS: Record<string, string> = {
  lan: "LAN",
  tailscale: "Tailscale",
  vpn: "VPN",
};

const TYPE_ORDER: Record<string, number> = { lan: 0, tailscale: 1, vpn: 2 };

function TypeIcon({ type }: { type: string }) {
  if (type === "tailscale" || type === "vpn") {
    return <Globe className="h-3 w-3 text-muted-foreground" aria-hidden />;
  }
  return <Wifi className="h-3 w-3 text-muted-foreground" aria-hidden />;
}

export function ServerAddressBar({ className }: { className?: string }) {
  const { t } = useT("runtimes");
  const { data, isLoading } = useQuery({
    queryKey: ["server-info"],
    queryFn: () => api.getServerInfo(),
    staleTime: 60_000,
    refetchOnWindowFocus: true,
  });

  if (isLoading || !data || data.addresses.length === 0) return null;

  const sorted = [...data.addresses].sort(
    (a, b) => (TYPE_ORDER[a.type] ?? 9) - (TYPE_ORDER[b.type] ?? 9),
  );

  return (
    <div
      className={cn(
        "rounded-lg border bg-muted/40 px-3 py-2.5 text-xs",
        className,
      )}
    >
      <div className="mb-1.5 text-[11px] font-medium text-muted-foreground">
        {t(($) => $.server_address.title)}
      </div>
      <div className="space-y-1">
        {sorted.map((addr) => (
          <div
            key={`${addr.interface}-${addr.ip}`}
            className="flex items-center gap-2"
          >
            <TypeIcon type={addr.type} />
            <span className="w-16 shrink-0 text-[11px] text-muted-foreground">
              {TYPE_LABELS[addr.type] ?? addr.type}
            </span>
            <code
              className={cn(
                "min-w-0 flex-1 truncate text-[12px] text-foreground tabular-nums",
                CODE_LIGATURE_CLASS,
              )}
            >
              {formatAddress(addr, data.port)}
            </code>
            <AddressCopyButton text={formatCopyUrl(addr, data.port)} />
          </div>
        ))}
      </div>
      {sorted.some((a) => a.type === "tailscale") && (
        <p className="mt-1.5 text-[10px] leading-[1.5] text-muted-foreground">
          {t(($) => $.server_address.tailscale_hint)}
        </p>
      )}
    </div>
  );
}
