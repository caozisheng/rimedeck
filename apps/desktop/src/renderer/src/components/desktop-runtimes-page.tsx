import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { RuntimesPage, type RuntimeMachine } from "@rimedeck/views/runtimes";
import { Activity, Monitor, Play, RotateCw, Server, Square } from "lucide-react";
import { Button } from "@rimedeck/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@rimedeck/ui/components/ui/dialog";
import { toast } from "sonner";
import { DaemonRuntimeActions } from "./daemon-runtime-card";
import type {
  DaemonStatus,
  WslDaemonStatus,
  WslDistroInfo,
} from "../../../shared/daemon-types";

const WSL_MACHINE_PREFIX = "local-wsl:";

/**
 * Desktop wrapper around the shared `RuntimesPage`. The Desktop process owns
 * multiple local runtime hosts: the Windows/macOS/Linux host daemon plus any
 * managed WSL distros. Keep them as sibling machine rows so each Start/Stop
 * controls exactly the selected host.
 */
export function DesktopRuntimesPage() {
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });
  const [lastIdentity, setLastIdentity] = useState<{
    daemonId: string | null;
    deviceName: string | null;
  }>({ daemonId: null, deviceName: null });
  const [hostName, setHostName] = useState<string | null>(null);
  const wsl = useWslRuntimeState(hostName);

  useEffect(() => {
    const apply = (s: DaemonStatus) => {
      setStatus(s);
      if (s.daemonId) {
        setLastIdentity({
          daemonId: s.daemonId,
          deviceName: s.deviceName ?? null,
        });
      }
    };
    window.daemonAPI.getStatus().then(apply);
    window.daemonAPI.getHostName().then((name) => setHostName(name || null));
    return window.daemonAPI.onStatusChange(apply);
  }, []);

  const bootstrapping =
    status.state === "installing_cli" ||
    status.state === "starting" ||
    status.state === "running" ||
    wsl.machines.some((machine) => machine.health === "online");

  return (
    <RuntimesPage
      localDaemonId={status.daemonId ?? lastIdentity.daemonId}
      localMachineName={status.deviceName ?? lastIdentity.deviceName ?? hostName}
      extraLocalMachines={wsl.machines}
      machineActions={(machine) =>
        machine.id.startsWith(WSL_MACHINE_PREFIX) ? (
          <WslRuntimeActions machine={machine} busy={wsl.busy} onRun={wsl.run} />
        ) : machine.isCurrent ? (
          <DaemonRuntimeActions />
        ) : undefined
      }
      connectComputerDialog={({ onClose, defaultDialog }) => (
        <DesktopConnectComputerDialog
          machines={wsl.machines}
          busy={wsl.busy}
          error={wsl.error}
          onRun={wsl.run}
          onClose={onClose}
          defaultDialog={defaultDialog}
        />
      )}
      hasLocalMachine
      bootstrapping={bootstrapping}
    />
  );
}

function useWslRuntimeState(hostName: string | null): {
  machines: RuntimeMachine[];
  busy: string | null;
  error: string | null;
  run: (distro: string, action: "start" | "stop") => Promise<boolean>;
} {
  const [distros, setDistros] = useState<WslDistroInfo[]>([]);
  const [statuses, setStatuses] = useState<Record<string, WslDaemonStatus>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    window.daemonAPI
      .listWslDistros()
      .then(async (items) => {
        if (cancelled) return;
        setDistros(items);
        const pairs = await Promise.all(
          items.map(
            async (item) =>
              [item.name, await window.daemonAPI.getWslStatus(item.name)] as const,
          ),
        );
        if (!cancelled) {
          setStatuses(Object.fromEntries(pairs));
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setDistros([]);
          setError(errorMessage(err));
        }
      });
    const unsubscribe = window.daemonAPI.onWslStatusChange((status) => {
      setStatuses((current) => ({ ...current, [status.distro]: status }));
    });
    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, []);

  const machines = useMemo(
    () =>
      distros.map((distro) =>
        wslMachineFromStatus({
          distro: distro.name,
          status: statuses[distro.name],
          hostName,
        }),
      ),
    [distros, hostName, statuses],
  );

  const run = async (distro: string, action: "start" | "stop"): Promise<boolean> => {
    setBusy(`${action}:${distro}`);
    setError(null);
    try {
      const result =
        action === "start"
          ? await window.daemonAPI.startWsl(distro)
          : await window.daemonAPI.stopWsl(distro);
      if (!result.success) {
        const message = result.error ?? `Failed to ${action} ${distro}`;
        setError(message);
        toast.error(`Failed to ${action} WSL runtime`, { description: message });
        return false;
      }
      const status = await window.daemonAPI.getWslStatus(distro);
      setStatuses((current) => ({ ...current, [distro]: status }));
      return true;
    } catch (err) {
      const message = errorMessage(err);
      setError(message);
      toast.error(`Failed to ${action} WSL runtime`, { description: message });
      return false;
    } finally {
      setBusy(null);
    }
  };

  return { machines, busy, error, run };
}

function wslMachineFromStatus({
  distro,
  status,
  hostName,
}: {
  distro: string;
  status?: WslDaemonStatus;
  hostName: string | null;
}): RuntimeMachine {
  const state = status?.state ?? "stopped";
  const online = state === "running";
  return {
    id: `${WSL_MACHINE_PREFIX}${distro}`,
    daemonId: status?.daemonId ?? null,
    title: hostName ? `${hostName} WSL` : "This machine WSL",
    subtitle: distro,
    deviceInfo: distro,
    cliVersion: null,
    mode: "local",
    section: "local",
    isCurrent: false,
    tags: ["WSL"],
    health: online ? "online" : state === "starting" || state === "stopping" ? "recently_lost" : "offline",
    runtimes: [],
    onlineCount: online ? 1 : 0,
    issueCount: online ? 0 : 1,
    runningCount: 0,
    queuedCount: 0,
    providerNames: [],
    lastSeenAt: null,
  };
}

function DesktopConnectComputerDialog({
  machines,
  busy,
  error,
  onRun,
  onClose,
  defaultDialog,
}: {
  machines: RuntimeMachine[];
  busy: string | null;
  error: string | null;
  onRun: (distro: string, action: "start" | "stop") => Promise<boolean>;
  onClose: () => void;
  defaultDialog: ReactNode;
}) {
  const [showRemote, setShowRemote] = useState(false);

  if (showRemote) {
    return <>{defaultDialog}</>;
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-lg">
        <DialogHeader className="px-6 pt-6 pb-2">
          <DialogTitle className="text-base text-balance">
            Add a computer
          </DialogTitle>
          <DialogDescription className="text-xs text-balance">
            Add a local WSL distro or connect another computer to this RimeDeck server.
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-4">
          <section className="space-y-2">
            <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <Monitor className="h-3.5 w-3.5" />
              Local WSL computers
            </div>
            {machines.length === 0 ? (
              <div className="rounded-lg border border-dashed px-3 py-4 text-xs text-muted-foreground">
                No WSL distros were found on this Windows machine.
              </div>
            ) : (
              <div className="space-y-2">
                {machines.map((machine) => (
                  <WslComputerOption
                    key={machine.id}
                    machine={machine}
                    busy={busy}
                    onAdd={onRun}
                  />
                ))}
              </div>
            )}
          </section>

          <section className="space-y-2">
            <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <Server className="h-3.5 w-3.5" />
              Remote computer
            </div>
            <button
              type="button"
              className="flex w-full items-center justify-between gap-3 rounded-lg border bg-background px-3 py-3 text-left transition-colors hover:bg-accent/50"
              onClick={() => setShowRemote(true)}
            >
              <span className="min-w-0">
                <span className="block text-sm font-medium">
                  Add another computer
                </span>
                <span className="mt-1 block text-xs text-muted-foreground">
                  Use pairing code or CLI commands for another Desktop or server.
                </span>
              </span>
              <span className="inline-flex h-8 shrink-0 items-center justify-center rounded-md border bg-background px-3 text-xs font-medium">
                Open
              </span>
            </button>
          </section>

          {error && (
            <p className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {error}
            </p>
          )}
        </div>

        <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function WslComputerOption({
  machine,
  busy,
  onAdd,
}: {
  machine: RuntimeMachine;
  busy: string | null;
  onAdd: (distro: string, action: "start" | "stop") => Promise<boolean>;
}) {
  const distro = wslDistroFromMachine(machine);
  const running = machine.health === "online";
  const isBusy = busy === `start:${distro}`;
  const isBlocked = busy !== null || running;
  const stateLabel = isBusy ? "Setting up..." : running ? "Added" : "Available";

  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border bg-background px-3 py-3">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-medium">WSL: {distro}</span>
          <span className="shrink-0 rounded border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
            WSL
          </span>
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          Runs tasks inside Linux paths such as /home/...
        </p>
      </div>
      <Button
        type="button"
        size="sm"
        disabled={isBlocked}
        onClick={() => {
          void onAdd(distro, "start");
        }}
      >
        {isBusy ? (
          <Activity className="mr-1.5 h-3.5 w-3.5 animate-pulse" />
        ) : !running ? (
          <Play className="mr-1.5 h-3.5 w-3.5" />
        ) : null}
        {stateLabel}
      </Button>
    </div>
  );
}

function WslRuntimeActions({
  machine,
  busy,
  onRun,
}: {
  machine: RuntimeMachine;
  busy: string | null;
  onRun: (distro: string, action: "start" | "stop") => Promise<boolean>;
}) {
  const [restarting, setRestarting] = useState(false);
  const distro = wslDistroFromMachine(machine);
  const busyForDistro = busy === `start:${distro}` || busy === `stop:${distro}`;
  const actionLoading = busyForDistro || restarting;
  const running = machine.health === "online";
  const transitioning = machine.health === "recently_lost" && !actionLoading;

  const handleRestart = async () => {
    setRestarting(true);
    try {
      const stopped = await onRun(distro, "stop");
      if (stopped) {
        await onRun(distro, "start");
      }
    } finally {
      setRestarting(false);
    }
  };

  return (
    <div className="flex flex-wrap items-center justify-end gap-1.5">
      {running ? (
        <>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              void handleRestart();
            }}
            disabled={actionLoading}
          >
            {restarting ? (
              <Activity className="size-3.5 mr-1.5 animate-pulse" />
            ) : (
              <RotateCw className="size-3.5 mr-1.5" />
            )}
            Restart
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => {
              void onRun(distro, "stop");
            }}
            disabled={actionLoading}
          >
            {busy === `stop:${distro}` ? (
              <Activity className="size-3.5 mr-1.5 animate-pulse" />
            ) : (
              <Square className="size-3.5 mr-1.5" />
            )}
            Stop
          </Button>
        </>
      ) : transitioning ? (
        <Button size="sm" variant="outline" disabled>
          <Activity className="size-3.5 mr-1.5 animate-pulse" />
          Updating
        </Button>
      ) : (
        <Button
          size="sm"
          onClick={() => {
            void onRun(distro, "start");
          }}
          disabled={actionLoading}
        >
          {busy === `start:${distro}` ? (
            <Activity className="size-3.5 mr-1.5 animate-pulse" />
          ) : (
            <Play className="size-3.5 mr-1.5" />
          )}
          Start
        </Button>
      )}
    </div>
  );
}

function wslDistroFromMachine(machine: RuntimeMachine): string {
  if (machine.id.startsWith(WSL_MACHINE_PREFIX)) {
    return machine.id.slice(WSL_MACHINE_PREFIX.length);
  }
  return machine.subtitle ?? machine.title.replace(/^WSL:\s*/, "");
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
