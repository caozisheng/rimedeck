import { useCallback, useState } from "react";
import { RefreshCw, Unplug, Link2 } from "lucide-react";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useAuthStore } from "@multica/core/auth";
import { JoinWorkspaceDialog } from "@multica/views/workspace/join-workspace-dialog";

type Phase = "failed" | "expired" | "rejoin";

interface Props {
  apiUrl: string;
}

export function RemoteReconnectPage({ apiUrl }: Props) {
  const [phase, setPhase] = useState<Phase>(() => {
    const hasToken = !!localStorage.getItem("multica_token");
    return hasToken ? "failed" : "expired";
  });
  const [newUrl, setNewUrl] = useState("");
  const initialize = useAuthStore((s) => s.initialize);

  const handleRetry = useCallback(async () => {
    // Restore token from disk if localStorage was cleared.
    const desktopAPI = (window as unknown as Record<string, unknown>).desktopAPI as
      | { getRemoteAuthToken?: () => Promise<string | null> }
      | undefined;
    const diskToken = await desktopAPI?.getRemoteAuthToken?.();
    if (diskToken && !localStorage.getItem("multica_token")) {
      localStorage.setItem("multica_token", diskToken);
    }

    if (!localStorage.getItem("multica_token")) {
      setPhase("expired");
      return;
    }

    await initialize();
    const currentUser = useAuthStore.getState().user;
    if (currentUser) {
      window.location.reload();
    }
  }, [initialize]);

  const handleChangeUrl = useCallback(async () => {
    const base = newUrl.trim().replace(/\/+$/, "");
    if (!base) return;
    const url = base.startsWith("http") ? base : `http://${base}`;

    const desktopAPI = (window as unknown as Record<string, unknown>).desktopAPI as
      | { switchRuntimeConfig?: (c: { apiUrl: string; wsUrl: string; authToken?: string }) => Promise<void> }
      | undefined;
    if (desktopAPI?.switchRuntimeConfig) {
      const wsUrl = url.replace(/^http/, "ws") + "/ws";
      const token = localStorage.getItem("multica_token") ?? undefined;
      await desktopAPI.switchRuntimeConfig({ apiUrl: url, wsUrl, authToken: token });
    }
    window.location.reload();
  }, [newUrl]);

  const handleDisconnect = useCallback(async () => {
    const dAPI = (window as unknown as Record<string, {
      disconnectRuntimeConfig?: () => Promise<void>;
    }>).desktopAPI;
    const daemon = (window as unknown as Record<string, {
      removeRemoteServer?: (url: string) => Promise<void>;
    }>).daemonAPI;

    if (daemon?.removeRemoteServer) {
      try { await daemon.removeRemoteServer(apiUrl); } catch { /* best effort */ }
    }
    await dAPI?.disconnectRuntimeConfig?.();
    localStorage.removeItem("multica_token");
    localStorage.removeItem("rimedeck_remote_server");
    window.location.reload();
  }, [apiUrl]);

  if (phase === "rejoin") {
    return <JoinWorkspaceDialog onClose={() => setPhase("expired")} />;
  }

  const displayUrl = apiUrl.replace(/^https?:\/\//, "");

  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <div className="flex flex-1 flex-col items-center justify-center gap-6 px-6">
        <MulticaIcon bordered size="lg" />

        {phase === "failed" && (
          <div className="flex w-full max-w-sm flex-col gap-4 text-center">
            <p className="text-sm text-muted-foreground">
              Cannot reach <span className="font-mono text-foreground">{displayUrl}</span>
            </p>

            <Button size="sm" onClick={handleRetry}>
              <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
              Retry
            </Button>

            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">
                Server address changed?
              </label>
              <div className="flex gap-2">
                <Input
                  type="text"
                  placeholder="new-address:18080"
                  value={newUrl}
                  onChange={(e) => setNewUrl(e.target.value)}
                  className="font-mono text-sm"
                  onKeyDown={(e) => e.key === "Enter" && handleChangeUrl()}
                />
                <Button size="sm" onClick={handleChangeUrl} disabled={!newUrl.trim()}>
                  Connect
                </Button>
              </div>
            </div>

            <Button variant="ghost" size="sm" className="text-destructive" onClick={handleDisconnect}>
              <Unplug className="mr-1.5 h-3.5 w-3.5" />
              Disconnect
            </Button>
          </div>
        )}

        {phase === "expired" && (
          <div className="flex w-full max-w-sm flex-col gap-4 text-center">
            <p className="text-sm text-muted-foreground">
              Credentials for <span className="font-mono text-foreground">{displayUrl}</span> have expired.
            </p>
            <p className="text-xs text-muted-foreground">
              Ask the workspace admin for a new invite code.
            </p>

            <Button size="sm" onClick={() => setPhase("rejoin")}>
              <Link2 className="mr-1.5 h-3.5 w-3.5" />
              Rejoin workspace
            </Button>

            <Button variant="ghost" size="sm" className="text-destructive" onClick={handleDisconnect}>
              <Unplug className="mr-1.5 h-3.5 w-3.5" />
              Disconnect
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
