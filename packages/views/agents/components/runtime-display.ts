import type { AgentRuntime, RuntimeDevice } from "@multica/core/types";

type RuntimeLike = AgentRuntime | RuntimeDevice;

export function runtimeWslDistro(runtime: RuntimeLike | null | undefined): string | null {
  const metadata = runtime?.metadata;
  if (!metadata || metadata.host_kind !== "wsl") return null;
  const distro = metadata.wsl_distro;
  return typeof distro === "string" && distro.trim() ? distro.trim() : null;
}

export function isWslRuntime(runtime: RuntimeLike | null | undefined): boolean {
  return runtimeWslDistro(runtime) !== null;
}

export function runtimeDisplayName(runtime: RuntimeLike): string {
  const distro = runtimeWslDistro(runtime);
  return distro ? `${runtime.name} · WSL ${distro}` : runtime.name;
}

export function runtimeEnvironmentLabel(runtime: RuntimeLike): string {
  const distro = runtimeWslDistro(runtime);
  if (distro) return `WSL · ${distro}`;
  return runtime.device_info;
}

export function runtimeSubtitle(runtime: RuntimeLike, ownerName?: string | null): string {
  const environment = runtimeEnvironmentLabel(runtime);
  return [ownerName, environment].filter(Boolean).join(" · ");
}
