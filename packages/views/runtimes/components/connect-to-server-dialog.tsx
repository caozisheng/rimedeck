"use client";

import { useState } from "react";
import { Check, Link2, Loader2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { CODE_LIGATURE_CLASS } from "@multica/ui/lib/code-style";
import { useT } from "../../i18n";

type Step = "form" | "connecting" | "success" | "error";

export function ConnectToServerDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT("runtimes");
  const [step, setStep] = useState<Step>("form");
  const [serverUrl, setServerUrl] = useState("");
  const [pairingCode, setPairingCode] = useState("");
  const [errorMsg, setErrorMsg] = useState("");

  const canSubmit =
    serverUrl.trim().length > 0 && pairingCode.trim().length >= 4;

  const handleConnect = async () => {
    setStep("connecting");
    setErrorMsg("");
    try {
      const base = serverUrl.trim().replace(/\/+$/, "");
      const url = base.startsWith("http") ? base : `http://${base}`;

      const res = await fetch(`${url}/api/auth/pair`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          code: pairingCode.trim().toUpperCase(),
          device_name: "",
        }),
      });

      if (!res.ok) {
        const body = await res.text().catch(() => "");
        throw new Error(
          body || `${res.status} ${res.statusText}`,
        );
      }

      const data: { token: string; workspace_id: string } = await res.json();

      // Tell daemon-manager the remote URL BEFORE syncToken, so
      // syncToken writes server_url to the daemon CLI profile.
      // Frontend stays on local — this is compute sharing only.
      const daemonAPI = (window as unknown as Record<string, unknown>).daemonAPI as
        | { setTargetApiUrl?: (u: string) => Promise<void>;
            syncToken?: (t: string, u: string) => Promise<void>;
            restart?: () => Promise<unknown> }
        | undefined;
      if (daemonAPI?.syncToken && data.token) {
        try {
          await daemonAPI.setTargetApiUrl?.(url);
          await daemonAPI.syncToken(data.token, "");
          await daemonAPI.restart?.();
        } catch { /* best effort */ }
      }

      // Persist for reference (daemon already has the token in its profile).
      localStorage.setItem(
        "rimedeck_remote_server",
        JSON.stringify({ url, token: data.token, workspaceId: data.workspace_id }),
      );

      setStep("success");
    } catch (e) {
      setErrorMsg(e instanceof Error ? e.message : String(e));
      setStep("error");
    }
  };

  if (step === "success") {
    return (
      <Dialog open onOpenChange={(v) => !v && onClose()}>
        <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-md">
          <DialogHeader className="px-6 pt-6 pb-2">
            <DialogTitle className="text-base text-balance">
              {t(($) => $.connect_to_server.success_title)}
            </DialogTitle>
            <DialogDescription className="text-xs text-balance">
              {t(($) => $.connect_to_server.success_description)}
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col items-center gap-3 px-6 py-8">
            <div
              className="flex h-12 w-12 items-center justify-center rounded-full bg-success/10"
              aria-hidden
            >
              <Check className="h-6 w-6 text-success" />
            </div>
          </div>
          <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
            <Button size="sm" onClick={() => window.location.reload()}>
              {t(($) => $.connect_to_server.done)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-md">
        <DialogHeader className="px-6 pt-6 pb-2">
          <DialogTitle className="text-base text-balance">
            {t(($) => $.connect_to_server.title)}
          </DialogTitle>
          <DialogDescription className="text-xs text-balance">
            {t(($) => $.connect_to_server.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 space-y-4 px-6 py-4">
          <div>
            <label className="mb-1.5 block text-xs font-medium text-foreground">
              {t(($) => $.connect_to_server.server_url_label)}
            </label>
            <Input
              type="text"
              placeholder="192.168.1.100:18080"
              value={serverUrl}
              onChange={(e) => setServerUrl(e.target.value)}
              disabled={step === "connecting"}
            />
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-medium text-foreground">
              {t(($) => $.connect_to_server.pairing_code_label)}
            </label>
            <Input
              type="text"
              placeholder="K3M9ZP"
              value={pairingCode}
              onChange={(e) => setPairingCode(e.target.value.toUpperCase())}
              disabled={step === "connecting"}
              className={cn("font-mono tracking-widest", CODE_LIGATURE_CLASS)}
              maxLength={8}
            />
          </div>

          {step === "error" && errorMsg && (
            <p className="text-xs text-destructive">{errorMsg}</p>
          )}
        </div>

        <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
          <Button variant="outline" size="sm" onClick={onClose} disabled={step === "connecting"}>
            {t(($) => $.connect_to_server.cancel)}
          </Button>
          <Button
            size="sm"
            onClick={handleConnect}
            disabled={!canSubmit || step === "connecting"}
          >
            {step === "connecting" ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                {t(($) => $.connect_to_server.connecting)}
              </>
            ) : (
              <>
                <Link2 className="h-3.5 w-3.5" />
                {t(($) => $.connect_to_server.connect)}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
