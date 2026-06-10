"use client";

import { useState } from "react";
import { Check, Loader2, Users } from "lucide-react";
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
import { useAuthStore } from "@multica/core/auth";
import { useT } from "../i18n";

type Step = "form" | "joining" | "success" | "error";

export function JoinWorkspaceDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT("settings");
  const localUser = useAuthStore((s) => s.user);
  const [step, setStep] = useState<Step>("form");
  const [serverUrl, setServerUrl] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [errorMsg, setErrorMsg] = useState("");

  const canSubmit =
    serverUrl.trim().length > 0 && inviteCode.trim().length >= 4;

  const handleJoin = async () => {
    setStep("joining");
    setErrorMsg("");
    try {
      const base = serverUrl.trim().replace(/\/+$/, "");
      const url = base.startsWith("http") ? base : `http://${base}`;

      // Redeem the invite code on the remote server.
      const redeemRes = await fetch(`${url}/api/invitations/redeem`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          code: inviteCode.trim().toUpperCase(),
          device_name: localUser?.name || "",
        }),
      });

      if (!redeemRes.ok) {
        const body = await redeemRes.text().catch(() => "");
        throw new Error(body || `${redeemRes.status} ${redeemRes.statusText}`);
      }

      const data: { token?: string; auth_token?: string; workspace_id?: string; user_id?: string } =
        await redeemRes.json();

      // Switch the frontend API to the remote server.
      const desktopAPI = (window as unknown as Record<string, unknown>).desktopAPI as
        | { switchRuntimeConfig?: (c: { apiUrl: string; wsUrl: string }) => Promise<void> }
        | undefined;

      if (desktopAPI?.switchRuntimeConfig) {
        const wsUrl = url.replace(/^http/, "ws") + "/ws";
        await desktopAPI.switchRuntimeConfig({ apiUrl: url, wsUrl });
      }

      // Store the remote server's JWT so the post-reload auth
      // initializer authenticates against the remote server.
      if (data.auth_token) {
        localStorage.setItem("multica_token", data.auth_token);
      }

      // Tell daemon-manager the remote URL BEFORE syncToken, so
      // syncToken writes server_url to the daemon CLI profile.
      // Then restart the daemon to register against the remote server.
      if (data.token) {
        const daemonAPI = (window as unknown as Record<string, unknown>).daemonAPI as
          | { setTargetApiUrl?: (u: string) => Promise<void>;
              syncToken?: (t: string, u: string) => Promise<void>;
              restart?: () => Promise<unknown> }
          | undefined;
        try {
          await daemonAPI?.setTargetApiUrl?.(url);
          await daemonAPI?.syncToken?.(data.token, data.user_id ?? "");
          await daemonAPI?.restart?.();
        } catch { /* best effort */ }
      }

      setStep("success");
    } catch (e) {
      setErrorMsg(e instanceof Error ? e.message : String(e));
      setStep("error");
    }
  };

  const handleDone = () => {
    onClose();
    window.location.reload();
  };

  if (step === "success") {
    return (
      <Dialog open onOpenChange={(v) => !v && handleDone()}>
        <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-md">
          <DialogHeader className="px-6 pt-6 pb-2">
            <DialogTitle className="text-base text-balance">
              {t(($) => $.members.join_success_title)}
            </DialogTitle>
            <DialogDescription className="text-xs text-balance">
              {t(($) => $.members.join_success_description)}
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col items-center gap-3 px-6 py-8">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-success/10" aria-hidden>
              <Check className="h-6 w-6 text-success" />
            </div>
          </div>
          <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
            <Button size="sm" onClick={handleDone}>
              {t(($) => $.members.join_done)}
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
            {t(($) => $.members.join_title)}
          </DialogTitle>
          <DialogDescription className="text-xs text-balance">
            {t(($) => $.members.join_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 space-y-4 px-6 py-4">
          <div>
            <label className="mb-1.5 block text-xs font-medium text-foreground">
              {t(($) => $.members.join_server_label)}
            </label>
            <Input
              type="text"
              placeholder="192.168.1.100:18080"
              value={serverUrl}
              onChange={(e) => setServerUrl(e.target.value)}
              disabled={step === "joining"}
            />
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-medium text-foreground">
              {t(($) => $.members.join_code_label)}
            </label>
            <Input
              type="text"
              placeholder="XP39KM"
              value={inviteCode}
              onChange={(e) => setInviteCode(e.target.value.toUpperCase())}
              disabled={step === "joining"}
              className={cn("font-mono tracking-widest", CODE_LIGATURE_CLASS)}
              maxLength={8}
            />
          </div>

          {step === "error" && errorMsg && (
            <p className="text-xs text-destructive">{errorMsg}</p>
          )}
        </div>

        <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
          <Button variant="outline" size="sm" onClick={onClose} disabled={step === "joining"}>
            {t(($) => $.members.join_cancel)}
          </Button>
          <Button size="sm" onClick={handleJoin} disabled={!canSubmit || step === "joining"}>
            {step === "joining" ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                {t(($) => $.members.join_joining)}
              </>
            ) : (
              <>
                <Users className="h-3.5 w-3.5" />
                {t(($) => $.members.join_button)}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
