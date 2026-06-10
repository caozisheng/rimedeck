"use client";

import { useEffect, useState } from "react";
import { Loader2, Users, Clock, ChevronRight } from "lucide-react";
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

type Step = "history" | "form" | "joining" | "reconnecting" | "error";

interface HistoryEntry {
  apiUrl: string;
  authToken?: string;
  label?: string;
  lastConnected: string;
}

export function JoinWorkspaceDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT("settings");
  const localUser = useAuthStore((s) => s.user);
  const [step, setStep] = useState<Step>("form");
  const [serverUrl, setServerUrl] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [errorMsg, setErrorMsg] = useState("");
  const [history, setHistory] = useState<HistoryEntry[]>([]);

  useEffect(() => {
    const desktopAPI = (window as unknown as Record<string, unknown>).desktopAPI as
      | { getRemoteHistory?: () => Promise<HistoryEntry[]> }
      | undefined;
    desktopAPI?.getRemoteHistory?.().then((h) => {
      if (h && h.length > 0) {
        setHistory(h);
        setStep("history");
      }
    }).catch(() => {});
  }, []);

  const switchAndReload = async (url: string, authToken: string, daemonToken?: string, userId?: string, workspaceId?: string) => {
    const desktopAPI = (window as unknown as Record<string, unknown>).desktopAPI as
      | { switchRuntimeConfig?: (c: { apiUrl: string; wsUrl: string; authToken?: string; workspaceId?: string }) => Promise<void> }
      | undefined;
    if (desktopAPI?.switchRuntimeConfig) {
      const wsUrl = url.replace(/^http/, "ws") + "/ws";
      await desktopAPI.switchRuntimeConfig({ apiUrl: url, wsUrl, authToken, workspaceId });
    }
    localStorage.setItem("multica_token", authToken);
    if (daemonToken) {
      localStorage.setItem("rimedeck_pending_daemon_token", JSON.stringify({
        token: daemonToken,
        userId: userId ?? "",
        serverUrl: url,
      }));
    }
    window.location.reload();
  };

  const handleReconnect = async (entry: HistoryEntry) => {
    if (!entry.authToken) {
      setServerUrl(entry.apiUrl.replace(/^https?:\/\//, ""));
      setStep("form");
      return;
    }
    setStep("reconnecting");
    setErrorMsg("");
    try {
      const url = entry.apiUrl;
      const res = await fetch(`${url}/api/me`, {
        headers: { Authorization: `Bearer ${entry.authToken}` },
        signal: AbortSignal.timeout(5000),
      });
      if (res.ok) {
        await switchAndReload(url, entry.authToken);
        return;
      }
      if (res.status === 401) {
        setServerUrl(url.replace(/^https?:\/\//, ""));
        setErrorMsg(t(($) => $.members.join_token_expired));
        setStep("form");
        return;
      }
      throw new Error(`${res.status} ${res.statusText}`);
    } catch {
      setServerUrl(entry.apiUrl.replace(/^https?:\/\//, ""));
      setErrorMsg(t(($) => $.members.join_reconnect_failed));
      setStep("form");
    }
  };

  const handleJoin = async () => {
    setStep("joining");
    setErrorMsg("");
    try {
      const base = serverUrl.trim().replace(/\/+$/, "");
      const url = base.startsWith("http") ? base : `http://${base}`;

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

      if (!data.auth_token) {
        throw new Error("Server did not return auth credentials");
      }

      await switchAndReload(url, data.auth_token, data.token, data.user_id, data.workspace_id);
    } catch (e) {
      setErrorMsg(e instanceof Error ? e.message : String(e));
      setStep("error");
    }
  };

  if (step === "history") {
    return (
      <Dialog open onOpenChange={(v) => !v && onClose()}>
        <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-md">
          <DialogHeader className="px-6 pt-6 pb-2">
            <DialogTitle className="text-base text-balance">
              {t(($) => $.members.join_title)}
            </DialogTitle>
            <DialogDescription className="text-xs text-balance">
              {t(($) => $.members.join_history_description)}
            </DialogDescription>
          </DialogHeader>

          <div className="min-h-0 flex-1 space-y-2 px-6 py-4">
            {history.map((entry) => (
              <button
                key={entry.apiUrl}
                type="button"
                onClick={() => handleReconnect(entry)}
                className="flex w-full items-center gap-3 rounded-lg border bg-card px-3 py-2.5 text-left transition-colors hover:bg-accent/40"
              >
                <Clock className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium font-mono">
                    {entry.apiUrl.replace(/^https?:\/\//, "")}
                  </div>
                  {entry.label && (
                    <div className="truncate text-xs text-muted-foreground">{entry.label}</div>
                  )}
                </div>
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              </button>
            ))}
          </div>

          <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
            <Button variant="outline" size="sm" onClick={onClose}>
              {t(($) => $.members.join_cancel)}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setStep("form")}>
              {t(($) => $.members.join_new_server)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }

  if (step === "reconnecting") {
    return (
      <Dialog open onOpenChange={(v) => !v && onClose()}>
        <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-md">
          <div className="flex flex-col items-center gap-3 px-6 py-12">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            <p className="text-sm text-muted-foreground">{t(($) => $.members.join_reconnecting)}</p>
          </div>
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

          {(step === "error" || errorMsg) && errorMsg && (
            <p className="text-xs text-destructive">{errorMsg}</p>
          )}
        </div>

        <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
          {history.length > 0 ? (
            <Button variant="outline" size="sm" onClick={() => { setErrorMsg(""); setStep("history"); }}>
              {t(($) => $.members.join_back_to_history)}
            </Button>
          ) : (
            <Button variant="outline" size="sm" onClick={onClose} disabled={step === "joining"}>
              {t(($) => $.members.join_cancel)}
            </Button>
          )}
          <Button size="sm" onClick={handleJoin} disabled={!serverUrl.trim() || !inviteCode.trim() || step === "joining"}>
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
