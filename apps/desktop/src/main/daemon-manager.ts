import { app, ipcMain, BrowserWindow, shell } from "electron";
import { execFile } from "child_process";
import {
  readFile,
  writeFile,
  mkdir,
  rm,
  open,
  stat,
} from "fs/promises";
import {
  existsSync,
  watchFile,
  unwatchFile,
  type StatsListener,
} from "fs";
import { join } from "path";
import { homedir, hostname } from "os";
import type {
  DaemonStatus,
  DaemonPrefs,
  WslDaemonStatus,
  WslDistroInfo,
} from "../shared/daemon-types";
import { daemonStatusAlive } from "../shared/daemon-types";
import {
  GITHUB_LATEST_BASE,
  ensureManagedCli,
  managedCliPath,
} from "./cli-bootstrap";
import { decideVersionAction } from "./version-decision";
import {
  classifyAuthProbe,
  isAuthStatusError,
  type AuthProbeResult,
} from "./daemon-auth-probe";

const DEFAULT_HEALTH_PORT = 19514;
const POLL_INTERVAL_MS = 5_000;
const PREFS_PATH = join(homedir(), ".rimedeck", "desktop_prefs.json");
const LOG_TAIL_RETRY_MS = 2_000;
const LOG_TAIL_MAX_RETRIES = 5;
// How long a start may sit in "starting" (with no /health) before we probe the
// token to find out whether login expired. The daemon's own startup can legitimately
// take a while (it renews the PAT and lists workspaces before serving /health), so we
// wait past the common case to avoid probing healthy-but-slow starts.
const AUTH_PROBE_GRACE_MS = 10_000;
// `multica daemon start` blocks until the daemon reports ready, polling /health
// for up to its own startup timeout (45s in server/cmd/multica/cmd_daemon.go) to
// cover cold-start agent-version detection. This execFile timeout MUST stay
// above that — otherwise Electron kills the CLI supervisor mid-startup and a
// healthy-but-slow start is misreported as a failure (the detached daemon child
// keeps running, so the UI flashes "stopped" then "running").
const DAEMON_START_EXEC_TIMEOUT_MS = 60_000;

const DEFAULT_PREFS: DaemonPrefs = { autoStart: true, autoStop: false };

interface ActiveProfile {
  name: string; // "" = default profile
  port: number;
}

interface WslRunResult {
  stdout: string;
  stderr: string;
}

let statusPollTimer: ReturnType<typeof setInterval> | null = null;
let logTailWatcher: { path: string; listener: StatsListener } | null = null;
let currentState: DaemonStatus["state"] = "installing_cli";
let getMainWindow: () => BrowserWindow | null = () => null;
let operationInProgress = false;
let cachedCliBinary: string | null | undefined = undefined;
let cliResolvePromise: Promise<string | null> | null = null;
let cachedCliBinaryVersion: string | null | undefined = undefined;
// Set when a CLI version mismatch was detected but the running daemon is
// busy executing tasks. The poll loop retries the check on each tick and
// fires the restart once active_task_count drops to 0.
let pendingVersionRestart = false;
let targetApiBaseUrl: string | null = null;
let activeProfile: ActiveProfile | null = null;

// ── WSL daemon management ──────────────────────────────────────────────

// Auth-probe state for the current start attempt. When a start fails to reach
// "running", we probe the daemon's token once (after AUTH_PROBE_GRACE_MS) to
// decide whether the cause is an expired/invalid login. `authExpired` is sticky
// until the next start attempt or a successful /health, so the UI keeps showing
// the re-login prompt instead of flapping back to "starting". See #3512.
let startingSince: number | null = null;
let authProbeDone = false;
let authExpired = false;

// Serialize all writes to any profile config file. Multiple paths
// (syncToken, resolveActiveProfile, clearToken, watch/unwatch handlers)
// may try to write concurrently; chaining them avoids interleaved writes
// corrupting the JSON.
let configWriteChain: Promise<void> = Promise.resolve();

// Keep the Go impl in sync: server/cmd/multica/cmd_daemon.go healthPortForProfile.
function healthPortForProfile(profile: string): number {
  if (!profile) return DEFAULT_HEALTH_PORT;
  let sum = 0;
  for (const b of Buffer.from(profile, "utf-8")) sum += b;
  return DEFAULT_HEALTH_PORT + 1 + (sum % 1000);
}

function profileDir(profile: string): string {
  return profile
    ? join(homedir(), ".rimedeck", "profiles", profile)
    : join(homedir(), ".rimedeck");
}

function profileConfigPath(profile: string): string {
  return join(profileDir(profile), "config.json");
}

function profileLogPath(profile: string): string {
  return join(profileDir(profile), "daemon.log");
}

function wslProfileName(distro: string): string {
  const slug = distro
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `wsl-${slug || "default"}`;
}

function runWsl(
  distro: string,
  args: string[],
  timeout = 10_000,
): Promise<WslRunResult> {
  return new Promise((resolve, reject) => {
    execFile(
      "wsl.exe",
      ["-d", distro, "-e", ...args],
      { timeout, windowsHide: true },
      (err, stdout, stderr) => {
        if (err) {
          reject(Object.assign(err, { stdout, stderr }));
          return;
        }
        resolve({ stdout, stderr });
      },
    );
  });
}

function runWslWithInput(
  distro: string,
  args: string[],
  input: string,
  timeout = 10_000,
): Promise<WslRunResult> {
  return new Promise((resolve, reject) => {
    const child = execFile(
      "wsl.exe",
      ["-d", distro, "-e", ...args],
      { timeout, windowsHide: true },
      (err, stdout, stderr) => {
        if (err) {
          reject(Object.assign(err, { stdout, stderr }));
          return;
        }
        resolve({ stdout, stderr });
      },
    );
    child.stdin?.end(input);
  });
}

function wslShellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

function wslManagedMulticaShellPath(): string {
  return '"$HOME/.local/bin/multica"';
}

function bundledWslMulticaPath(goarch: "amd64" | "arm64"): string {
  return join(
    app.getAppPath(),
    "resources",
    "bin",
    "wsl",
    `linux-${goarch}`,
    "multica",
  ).replace("app.asar", "app.asar.unpacked");
}

function windowsPathToWslPath(path: string): string | null {
  const normalized = path.replace(/\\/g, "/");
  const match = normalized.match(/^([A-Za-z]):\/(.*)$/);
  if (!match) return null;
  return `/mnt/${match[1].toLowerCase()}/${match[2]}`;
}

function wslProbeScript(binExpr: string): string {
  if (binExpr === "multica") {
    return [
      "p=\"$(command -v multica)\"",
      "[ -n \"$p\" ]",
      "multica version --output json",
      "printf '\\n__MULTICA_BIN__%s\\n' \"$p\"",
    ].join(" && ");
  }
  return [
    `[ -x ${binExpr} ]`,
    `${binExpr} version --output json`,
    "printf '\\n__MULTICA_BIN__%s\\n' \"$HOME/.local/bin/multica\"",
  ].join(" && ");
}

function parseCliVersionOutput(stdout: string): string | null {
  try {
    const parsed = JSON.parse(stdout.trim()) as { version?: unknown };
    return typeof parsed.version === "string" && parsed.version.length > 0
      ? parsed.version
      : null;
  } catch {
    return null;
  }
}

async function probeWslMultica(
  distro: string,
  binExpr: string,
): Promise<{ path: string; version: string } | null> {
  try {
    const { stdout } = await runWsl(
      distro,
      ["sh", "-lc", wslProbeScript(binExpr)],
      8_000,
    );
    const versionText = stdout.split("\n__MULTICA_BIN__")[0] ?? "";
    const version = parseCliVersionOutput(versionText);
    if (!version) return null;
    const resolved = stdout.split("\n__MULTICA_BIN__")[1]?.trim();
    if (!resolved) return null;
    return { path: resolved, version };
  } catch {
    return null;
  }
}

async function ensureWslMultica(
  distro: string,
): Promise<{ path: string; version: string; installed: boolean }> {
  const managed = await probeWslMultica(distro, wslManagedMulticaShellPath());
  if (managed) return { ...managed, installed: false };

  const onPath = await probeWslMultica(distro, "multica");
  if (onPath) return { ...onPath, installed: false };

  const { stdout: unameStdout } = await runWsl(
    distro,
    ["sh", "-lc", "uname -m"],
    8_000,
  );
  const machine = unameStdout.trim();
  const goarch =
    machine === "x86_64" || machine === "amd64"
      ? "amd64"
      : machine === "aarch64" || machine === "arm64"
        ? "arm64"
        : null;
  if (!goarch) {
    throw new Error(`failed to install multica in WSL: unsupported_arch:${machine}`);
  }

  const bundledCli = bundledWslMulticaPath(goarch);
  if (existsSync(bundledCli)) {
    const wslBundledCli = windowsPathToWslPath(bundledCli);
    if (!wslBundledCli) {
      throw new Error(
        `failed to install bundled multica in WSL: unsupported Windows path ${bundledCli}`,
      );
    }
    try {
      await runWsl(
        distro,
        [
          "sh",
          "-lc",
          [
            "mkdir -p \"$HOME/.local/bin\"",
            `cp ${wslShellQuote(wslBundledCli)} "$HOME/.local/bin/multica"`,
            "chmod 0755 \"$HOME/.local/bin/multica\"",
          ].join(" && "),
        ],
        30_000,
      );
      const installed = await probeWslMultica(distro, wslManagedMulticaShellPath());
      if (!installed) {
        throw new Error("bundled multica was copied but version probe failed");
      }
      return { ...installed, installed: true };
    } catch (err) {
      throw new Error(
        `failed to install bundled multica in WSL: ${errorMessage(err)}`,
      );
    }
  }

  const script = `
set -eu
need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing_tool:$1" >&2
    exit 10
  }
}
need curl
need tar
need sha256sum
need mktemp
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) asset_arch=amd64 ;;
  aarch64|arm64) asset_arch=arm64 ;;
  *) echo "unsupported_arch:$arch" >&2; exit 11 ;;
esac
base=${wslShellQuote(GITHUB_LATEST_BASE)}
asset="multica_linux_\${asset_arch}.tar.gz"
work="$(mktemp -d)"
cleanup() { rm -rf "$work"; }
trap cleanup EXIT
curl -fsSL "$base/checksums.txt" -o "$work/checksums.txt"
expected="$(awk -v a="$asset" '$2 == a || $2 == "*" a { print $1; exit }' "$work/checksums.txt")"
[ -n "$expected" ] || { echo "missing_checksum:$asset" >&2; exit 12; }
curl -fsSL "$base/$asset" -o "$work/$asset"
actual="$(sha256sum "$work/$asset" | awk '{print $1}')"
[ "$actual" = "$expected" ] || { echo "checksum_mismatch:$asset" >&2; exit 13; }
tar -xzf "$work/$asset" -C "$work"
[ -f "$work/multica" ] || { echo "archive_missing_binary:$asset" >&2; exit 14; }
mkdir -p "$HOME/.local/bin"
install -m 0755 "$work/multica" "$HOME/.local/bin/multica"
"$HOME/.local/bin/multica" version --output json
printf '\\n__MULTICA_BIN__%s\\n' "$HOME/.local/bin/multica"
`;

  try {
    const { stdout } = await runWsl(distro, ["sh", "-lc", script], 120_000);
    const versionText = stdout.split("\n__MULTICA_BIN__")[0] ?? "";
    const version = parseCliVersionOutput(versionText);
    const resolved = stdout.split("\n__MULTICA_BIN__")[1]?.trim();
    if (!version) {
      throw new Error("installed multica but version output was invalid");
    }
    if (!resolved) {
      throw new Error("installed multica but install path was not reported");
    }
    return { path: resolved, version, installed: true };
  } catch (err) {
    const withOutput = err as Error & { stderr?: string; stdout?: string };
    const detail = (withOutput.stderr || withOutput.stdout || errorMessage(err)).trim();
    throw new Error(`failed to install multica in WSL: ${detail}`);
  }
}

async function listWslDistros(): Promise<WslDistroInfo[]> {
  if (process.platform !== "win32") return [];
  const result = await new Promise<string>((resolve, reject) => {
    execFile("wsl.exe", ["-l", "-q"], { timeout: 10_000, windowsHide: true }, (err, stdout) => {
      if (err) {
        reject(err);
        return;
      }
      resolve(stdout);
    });
  });
  const names = result
    .replace(/\0/g, "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .filter((line) => !line.toLowerCase().includes("windows subsystem for linux"));
  return names.map((name, index) => ({ name, default: index === 0 }));
}

async function resolveWslServerUrl(
  distro: string,
  apiUrl: string,
): Promise<string> {
  let parsed: URL;
  try {
    parsed = new URL(apiUrl);
  } catch {
    return apiUrl;
  }
  const host = parsed.hostname.toLowerCase();
  if (host !== "localhost" && host !== "127.0.0.1" && host !== "::1") {
    return apiUrl;
  }
  if (await canWslReachUrl(distro, parsed)) {
    return apiUrl;
  }

  const candidates: string[] = [];
  try {
    const { stdout } = await runWsl(
      distro,
      [
        "sh",
        "-lc",
        [
          "sed -n 's/^nameserver //p' /etc/resolv.conf | head -1",
          "ip route 2>/dev/null | awk '/default via/ { print $3 }'",
        ].join("; "),
      ],
      5_000,
    );
    for (const line of stdout.split(/\r?\n/)) {
      const candidate = line.trim();
      if (candidate && !candidates.includes(candidate)) {
        candidates.push(candidate);
      }
    }
  } catch (err) {
    console.warn("[daemon:wsl] failed to resolve Windows host candidates:", err);
  }

  for (const candidate of candidates) {
    const candidateUrl = new URL(parsed.toString());
    candidateUrl.hostname = candidate;
    if (await canWslReachUrl(distro, candidateUrl)) {
      return candidateUrl.toString().replace(/\/$/, "");
    }
  }
  return apiUrl;
}

async function canWslReachUrl(distro: string, url: URL): Promise<boolean> {
  const healthUrl = new URL(url.toString());
  healthUrl.pathname = "/health";
  healthUrl.search = "";
  healthUrl.hash = "";
  try {
    await runWsl(
      distro,
      [
        "curl",
        "-fsS",
        "--connect-timeout",
        "2",
        healthUrl.toString().replace(/\/$/, ""),
      ],
      5_000,
    );
    return true;
  } catch {
    return false;
  }
}

async function syncWslProfile(distro: string, profile: string): Promise<void> {
  const source = await ensureActiveProfile();
  const cfg = await readProfileConfig(source.name);
  const token = typeof cfg.token === "string" ? cfg.token : "";
  const serverUrl =
    targetApiBaseUrl ??
    (typeof cfg.server_url === "string" ? cfg.server_url : "");
  if (!token) {
    throw new Error("Desktop daemon profile is not authenticated yet");
  }
  if (!serverUrl) {
    throw new Error("Desktop daemon profile has no server URL");
  }
  const wslServerUrl = await resolveWslServerUrl(distro, serverUrl);
  const payload = JSON.stringify(
    {
      ...cfg,
      token,
      server_url: wslServerUrl,
    },
    null,
    2,
  );
  const profileDir = `$HOME/.rimedeck/profiles/${profile.replace(/"/g, '\\"')}`;
  const configPath = `${profileDir}/config.json`;
  const script = [
    `mkdir -p "${profileDir}"`,
    `cat > "${configPath}"`,
  ].join(" && ");
  await runWslWithInput(distro, ["sh", "-lc", script], payload, 10_000);
}

async function fetchWslHealth(distro: string): Promise<WslDaemonStatus> {
  distro = distro.trim();
  const profile = wslProfileName(distro);
  if (!distro) return { state: "stopped", distro, hostKind: "wsl", profile };
  const port = healthPortForProfile(profile);
  const data = await fetchHealthAtPort(port);
  if (!data) return { state: "stopped", distro, hostKind: "wsl", profile };
  if (data.status === "running") {
    return {
      state: "running",
      distro,
      hostKind: "wsl",
      profile,
      pid: data.pid,
      uptime: data.uptime,
      daemonId: data.daemon_id,
      deviceName: data.device_name,
      agents: data.agents ?? [],
      workspaceCount: Array.isArray(data.workspaces) ? data.workspaces.length : 0,
      serverUrl: data.server_url,
    };
  }
  if (data.status === "starting") {
    return { state: "starting", distro, hostKind: "wsl", profile };
  }
  return { state: "stopped", distro, hostKind: "wsl", profile };
}

function sendWslStatus(status: WslDaemonStatus): void {
  const win = getMainWindow();
  win?.webContents.send("daemon:wsl-status", status);
}

async function startWslDaemon(distro: string): Promise<{ success: boolean; error?: string }> {
  distro = distro.trim();
  if (!distro) return { success: false, error: "WSL distro is required" };
  if (process.platform !== "win32") {
    return { success: false, error: "WSL runtimes are only supported on Windows" };
  }
  const profile = wslProfileName(distro);
  const existing = await fetchWslHealth(distro);
  if (daemonStatusAlive(existing.state)) {
    sendWslStatus(existing);
    return { success: true };
  }
  try {
    await syncWslProfile(distro, profile);
    const cli = await ensureWslMultica(distro);
    console.log(
      `[daemon:wsl] using multica in ${distro}: ${cli.path} (${cli.version}, installed=${cli.installed})`,
    );
    sendWslStatus({ state: "starting", distro, hostKind: "wsl", profile });
    await new Promise<void>((resolve, reject) => {
      execFile(
        "wsl.exe",
        [
          "-d",
          distro,
          "-e",
          "env",
          "MULTICA_LAUNCHED_BY=desktop",
          "MULTICA_MANAGED_BY_DESKTOP=true",
          "MULTICA_HOST_KIND=wsl",
          "MULTICA_HOST_OS=linux",
          `MULTICA_WSL_DISTRO=${distro}`,
          cli.path,
          "daemon",
          "start",
          "--profile",
          profile,
        ],
        {
          timeout: DAEMON_START_EXEC_TIMEOUT_MS,
          windowsHide: true,
        },
        (err) => {
          if (err) {
            reject(err);
            return;
          }
          resolve();
        },
      );
    });
    sendWslStatus(await fetchWslHealth(distro));
    return { success: true };
  } catch (err) {
    sendWslStatus({ state: "stopped", distro, hostKind: "wsl", profile });
    return { success: false, error: errorMessage(err) };
  }
}

async function stopWslDaemon(distro: string): Promise<{ success: boolean; error?: string }> {
  distro = distro.trim();
  if (!distro) return { success: false, error: "WSL distro is required" };
  if (process.platform !== "win32") {
    return { success: false, error: "WSL runtimes are only supported on Windows" };
  }
  const profile = wslProfileName(distro);
  sendWslStatus({ state: "stopping", distro, hostKind: "wsl", profile });
  try {
    const cli =
      (await probeWslMultica(distro, wslManagedMulticaShellPath())) ??
      (await probeWslMultica(distro, "multica"));
    if (!cli) {
      sendWslStatus({ state: "stopped", distro, hostKind: "wsl", profile });
      return { success: true };
    }
    await runWsl(distro, [cli.path, "daemon", "stop", "--profile", profile], 20_000);
    sendWslStatus({ state: "stopped", distro, hostKind: "wsl", profile });
    return { success: true };
  } catch (err) {
    return { success: false, error: errorMessage(err) };
  }
}

async function validateWslLocalDirectory(
  distro: string,
  path: string,
): Promise<{
  ok: boolean;
  reason?:
    | "not_absolute"
    | "not_found"
    | "not_a_directory"
    | "not_readable"
    | "not_writable"
    | "error";
  error?: string;
}> {
  distro = distro.trim();
  if (!distro) return { ok: false, reason: "error", error: "WSL distro is required" };
  const p = path.trim();
  if (!p.startsWith("/")) return { ok: false, reason: "not_absolute" };
  const script = [
    `p=${wslShellQuote(p)}`,
    `[ -e "$p" ] || { echo not_found; exit 0; }`,
    `[ -d "$p" ] || { echo not_a_directory; exit 0; }`,
    `[ -r "$p" ] || { echo not_readable; exit 0; }`,
    `[ -w "$p" ] || { echo not_writable; exit 0; }`,
    "echo ok",
  ].join("\n");
  try {
    const { stdout } = await runWsl(distro, ["sh", "-lc", script], 10_000);
    const result = stdout.trim();
    if (result === "ok") return { ok: true };
    if (
      result === "not_found" ||
      result === "not_a_directory" ||
      result === "not_readable" ||
      result === "not_writable"
    ) {
      return { ok: false, reason: result };
    }
    return { ok: false, reason: "error", error: result || "WSL validation failed" };
  } catch (err) {
    return { ok: false, reason: "error", error: errorMessage(err) };
  }
}

// Sidecar file that records which Multica user the cached PAT in config.json
// was minted for. The Go CLI/daemon never read or write this file, so it
// survives Go-side config rewrites. Used to detect user switches and mint a
// fresh PAT instead of reusing a token that belongs to a previous user.
function profileUserIdPath(profile: string): string {
  return join(profileDir(profile), ".desktop-user-id");
}

async function readProfileUserId(profile: string): Promise<string | null> {
  try {
    const raw = await readFile(profileUserIdPath(profile), "utf-8");
    const trimmed = raw.trim();
    return trimmed || null;
  } catch {
    return null;
  }
}

async function writeProfileUserId(
  profile: string,
  userId: string,
): Promise<void> {
  await mkdir(profileDir(profile), { recursive: true });
  await writeFile(profileUserIdPath(profile), userId, "utf-8");
}

async function removeProfileUserId(profile: string): Promise<void> {
  try {
    await rm(profileUserIdPath(profile));
  } catch {
    // Already gone — nothing to do.
  }
}

function normalizeUrl(u: string): string {
  if (!u) return "";
  try {
    const parsed = new URL(u);
    return `${parsed.protocol}//${parsed.host}`.toLowerCase();
  } catch {
    return u.replace(/\/+$/, "").toLowerCase();
  }
}

function urlsMatch(a: string, b: string): boolean {
  const na = normalizeUrl(a);
  const nb = normalizeUrl(b);
  return na.length > 0 && na === nb;
}

function sendStatus(status: DaemonStatus): void {
  const win = getMainWindow();
  win?.webContents.send("daemon:status", status);
}

interface HealthPayload {
  status?: string;
  pid?: number;
  uptime?: string;
  daemon_id?: string;
  device_name?: string;
  server_url?: string;
  cli_version?: string;
  active_task_count?: number;
  agents?: string[];
  workspaces?: unknown[];
}

async function fetchHealthAtPort(
  port: number,
): Promise<HealthPayload | null> {
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 2_000);
    const res = await fetch(`http://127.0.0.1:${port}/health`, {
      signal: controller.signal,
    });
    clearTimeout(timeout);
    if (!res.ok) return null;
    return (await res.json()) as HealthPayload;
  } catch {
    return null;
  }
}

/**
 * Validates the daemon profile's token against the backend to find out whether
 * a stuck start is an auth problem. Hits the same endpoint `multica auth status`
 * uses (GET /api/me) with the exact token the daemon loads from config.json, so
 * the verdict matches what the daemon itself would get from the server.
 *
 * Only the HTTP status is inspected (never the body) so a future change to the
 * /api/me response shape can't break this — a 401 means the token is rejected,
 * a 2xx means it's fine, and a thrown request means the network is the problem,
 * not auth. See classifyAuthProbe for the full rule set.
 */
async function probeTokenValidity(profile: string): Promise<AuthProbeResult> {
  if (!targetApiBaseUrl) return "unknown";
  const cfg = await readProfileConfig(profile);
  const token = typeof cfg.token === "string" ? cfg.token : "";
  if (!token) return classifyAuthProbe({ noToken: true });
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 4_000);
    const res = await fetch(`${targetApiBaseUrl.replace(/\/+$/, "")}/api/me`, {
      headers: { Authorization: `Bearer ${token}` },
      signal: controller.signal,
    });
    clearTimeout(timeout);
    return classifyAuthProbe({ status: res.status });
  } catch {
    return classifyAuthProbe({ networkError: true });
  }
}

// Desktop owns a dedicated CLI profile named after the target API host, so it
// never reads or writes the user's hand-configured profiles. Profile dir:
//   ~/.rimedeck/profiles/desktop-<host>/
function deriveProfileName(targetUrl: string): string {
  try {
    const url = new URL(targetUrl);
    const host = url.host.replace(/:/g, "-").toLowerCase();
    return `desktop-${host}`;
  } catch {
    return "desktop";
  }
}

async function readProfileConfig(
  profile: string,
): Promise<Record<string, unknown>> {
  try {
    const raw = await readFile(profileConfigPath(profile), "utf-8");
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

async function writeProfileConfig(
  profile: string,
  cfg: Record<string, unknown>,
): Promise<void> {
  const op = async () => {
    await mkdir(profileDir(profile), { recursive: true });
    await writeFile(
      profileConfigPath(profile),
      JSON.stringify(cfg, null, 2),
      "utf-8",
    );
  };
  const next = configWriteChain.catch(() => {}).then(op);
  configWriteChain = next.catch(() => {});
  return next;
}

/**
 * Returns the Desktop-owned profile for the current target API URL. Creates
 * the profile's config.json on demand with `server_url` pinned to the target.
 *
 * This function never falls back to the default profile, and never touches a
 * profile whose name doesn't start with `desktop-`, so the user's manually
 * configured CLI profiles are untouched.
 */
async function resolveActiveProfile(): Promise<ActiveProfile> {
  const target = targetApiBaseUrl;
  if (!target) return { name: "", port: DEFAULT_HEALTH_PORT };

  const name = deriveProfileName(target);
  const cfg = await readProfileConfig(name);

  if (cfg.server_url !== target) {
    cfg.server_url = target;
    await writeProfileConfig(name, cfg);
    console.log(`[daemon] initialized profile "${name}" → ${target}`);
  }

  return { name, port: healthPortForProfile(name) };
}

async function ensureActiveProfile(): Promise<ActiveProfile> {
  if (activeProfile) return activeProfile;
  activeProfile = await resolveActiveProfile();
  return activeProfile;
}

function invalidateActiveProfile(): void {
  activeProfile = null;
}

async function fetchHealth(): Promise<DaemonStatus> {
  // While the CLI is being downloaded or has permanently failed, short-circuit
  // polling — there's nothing to probe yet and /health calls would just return
  // "stopped", which would overwrite the correct setup state in the UI.
  if (currentState === "installing_cli" || currentState === "cli_not_found") {
    return { state: currentState };
  }

  const active = await ensureActiveProfile();
  const data = await fetchHealthAtPort(active.port);

  if (!data || data.status !== "running") {
    // A start that never reaches "running" is the symptom; an expired/invalid
    // login is the most common cause and the one with no other signal (the
    // daemon exits before it can serve /health, so we can't read the reason
    // from it). Probe the token once per attempt, after a grace period, to
    // surface a re-login prompt instead of spinning on "starting" forever.
    if (
      currentState === "starting" &&
      !authExpired &&
      !authProbeDone &&
      startingSince !== null &&
      Date.now() - startingSince >= AUTH_PROBE_GRACE_MS
    ) {
      authProbeDone = true;
      if ((await probeTokenValidity(active.name)) === "auth_expired") {
        authExpired = true;
      }
    }
    // Sticky: once login is known-expired, keep reporting it (even after
    // currentState flips away from "starting") until the next start attempt or
    // a successful /health clears the flag.
    if (authExpired) {
      return { state: "auth_expired", profile: active.name };
    }
    // The daemon binds /health before preflight finishes and self-reports
    // "starting" until it's ready. Trust that over our own currentState, so a
    // daemon booting on its own — or started via the CLI — surfaces as
    // "starting" instead of "stopped".
    if (data?.status === "starting") {
      return { state: "starting", profile: active.name };
    }
    return {
      state: currentState === "starting" ? "starting" : "stopped",
      profile: active.name,
    };
  }

  // A live, authenticated daemon clears any prior auth-failure verdict so the
  // re-login prompt disappears once the user reconnects.
  authExpired = false;
  startingSince = null;

  // Safety: if we have a target URL and the daemon on our port reports a
  // different server_url, it's not "our" daemon — drop it and re-resolve.
  if (
    targetApiBaseUrl &&
    data.server_url &&
    !urlsMatch(data.server_url, targetApiBaseUrl)
  ) {
    invalidateActiveProfile();
    return { state: "stopped" };
  }

  return {
    state: "running",
    pid: data.pid,
    uptime: data.uptime,
    daemonId: data.daemon_id,
    deviceName: data.device_name,
    agents: data.agents ?? [],
    workspaceCount: Array.isArray(data.workspaces)
      ? data.workspaces.length
      : 0,
    profile: active.name,
    serverUrl: data.server_url,
  };
}

function findCliOnPath(): string | null {
  const candidates = process.platform === "win32" ? ["multica.exe"] : ["multica"];
  const paths = (process.env["PATH"] ?? "").split(
    process.platform === "win32" ? ";" : ":",
  );
  if (process.platform === "darwin") {
    paths.push("/opt/homebrew/bin", "/usr/local/bin");
  }
  for (const name of candidates) {
    for (const dir of paths) {
      const full = join(dir, name);
      if (existsSync(full)) return full;
    }
  }
  return null;
}

/**
 * Returns the path to the CLI binary bundled inside the Desktop app.
 *
 * - Dev (`electron-vite dev`): `app.getAppPath()` → `apps/desktop`, resolving
 *   to `apps/desktop/resources/bin/multica`. `bundle-cli.mjs` populates this
 *   before dev starts, so iterating on Go changes is "make build → restart".
 * - Packaged: `app.getAppPath()` → `<Multica.app>/Contents/Resources/app.asar`.
 *   electron-builder's `asarUnpack: resources/**` extracts the binary to
 *   `app.asar.unpacked/`, so we swap the path segment to execute it.
 */
function bundledCliPath(): string {
  const binName = process.platform === "win32" ? "multica.exe" : "multica";
  return join(app.getAppPath(), "resources", "bin", binName).replace(
    "app.asar",
    "app.asar.unpacked",
  );
}

async function probeCliBinary(
  bin: string,
  source: "bundled" | "managed" | "path",
): Promise<string | null> {
  try {
    const stdout = await new Promise<string>((resolve, reject) => {
      execFile(
        bin,
        ["version", "--output", "json"],
        { timeout: 5_000 },
        (err, out) => {
          if (err) reject(err);
          else resolve(out);
        },
      );
    });
    const parsed = JSON.parse(stdout) as { version?: string };
    if (typeof parsed.version === "string" && parsed.version.length > 0) {
      return parsed.version;
    }
    console.warn(
      `[daemon] ignoring ${source} CLI at ${bin}: version output was missing or invalid`,
    );
    return null;
  } catch (err) {
    console.warn(`[daemon] ignoring ${source} CLI at ${bin}:`, err);
    return null;
  }
}

/**
 * Returns a usable `multica` binary path. Priority:
 *   1. Cached result from a previous successful resolve.
 *   2. Bundled binary shipped with the Desktop app (`bundle-cli.mjs`).
 *   3. Managed binary already installed in userData (`managedCliPath`).
 *   4. Download + install latest release into userData.
 *   5. `multica` on PATH (dev convenience / user-installed via brew).
 * Returns `null` only when all of the above fail.
 *
 * Bundled is preferred so Desktop iterates in lockstep with Go changes in
 * the same repo — avoids the 404 / stale-API problem when the Desktop's
 * TS side is ahead of the last published CLI release.
 *
 * This function is idempotent and safe to call concurrently — in-flight
 * installs are de-duplicated via `cliResolvePromise`.
 */
async function resolveCliBinary(): Promise<string | null> {
  if (cachedCliBinary !== undefined) return cachedCliBinary;
  if (cliResolvePromise) return cliResolvePromise;

  cliResolvePromise = (async () => {
    const bundled = bundledCliPath();
    if (existsSync(bundled)) {
      const version = await probeCliBinary(bundled, "bundled");
      if (version) {
        console.log(`[daemon] using bundled CLI at ${bundled}`);
        cachedCliBinary = bundled;
        cachedCliBinaryVersion = version;
        return bundled;
      }
    }

    const managed = managedCliPath();
    if (existsSync(managed)) {
      const version = await probeCliBinary(managed, "managed");
      if (version) {
        cachedCliBinary = managed;
        cachedCliBinaryVersion = version;
        return managed;
      }
    }

    try {
      const installed = await ensureManagedCli({
        forceInstall: existsSync(managed),
      });
      const version = await probeCliBinary(installed, "managed");
      if (version) {
        cachedCliBinary = installed;
        cachedCliBinaryVersion = version;
        return installed;
      }
      console.warn(
        `[daemon] managed CLI at ${installed} failed validation after install`,
      );
    } catch (err) {
      console.warn("[daemon] CLI auto-install failed, falling back to PATH:", err);
    }

    const onPath = findCliOnPath();
    if (onPath) {
      const version = await probeCliBinary(onPath, "path");
      if (version) {
        cachedCliBinary = onPath;
        cachedCliBinaryVersion = version;
        return onPath;
      }
    }

    cachedCliBinary = null;
    cachedCliBinaryVersion = null;
    return null;
  })();

  try {
    return await cliResolvePromise;
  } finally {
    cliResolvePromise = null;
  }
}

/**
 * Reads the version of the currently resolved CLI binary. Cached for the
 * process lifetime — the bundled binary doesn't change after bundle time.
 * Returns null on any failure (unknown `go` at bundle time, broken binary,
 * wrong-arch bundled binary, etc.) so callers can fail open.
 */
async function getCliBinaryVersion(): Promise<string | null> {
  if (cachedCliBinaryVersion !== undefined) return cachedCliBinaryVersion;
  const bin = await resolveCliBinary();
  if (!bin) {
    cachedCliBinaryVersion = null;
    return null;
  }
  cachedCliBinaryVersion = await probeCliBinary(bin, "path");
  return cachedCliBinaryVersion;
}

/**
 * Compares the running daemon's `cli_version` against the CLI binary we
 * would use to spawn a new one, and restarts only when safe. The decision
 * logic itself is in `version-decision.ts` (pure, unit-tested); this
 * wrapper handles the async plumbing and side effects.
 *
 * Restart is only fired when ALL of:
 *   - a daemon is actually running on the active profile's port
 *   - both sides report a version and the strings differ
 *   - `active_task_count` is 0 (no in-flight agent work would be killed)
 *
 * On a confirmed mismatch while the daemon is busy, `pendingVersionRestart`
 * is set; the poll loop retries this function on each 5s tick and will fire
 * the restart as soon as the daemon drains.
 */
async function ensureRunningDaemonVersionMatches(): Promise<
  "restarted" | "deferred" | "ok" | "not_running"
> {
  const active = await ensureActiveProfile();
  const running = await fetchHealthAtPort(active.port);
  const bundled = await getCliBinaryVersion();
  const action = decideVersionAction(bundled, running);

  switch (action) {
    case "not_running":
      pendingVersionRestart = false;
      return "not_running";
    case "ok":
      pendingVersionRestart = false;
      return "ok";
    case "defer": {
      if (!pendingVersionRestart) {
        const activeTasks = running?.active_task_count ?? 0;
        console.log(
          `[daemon] CLI version mismatch (bundled=${bundled} running=${running?.cli_version}); deferring restart until ${activeTasks} active task(s) finish`,
        );
      }
      pendingVersionRestart = true;
      return "deferred";
    }
    case "restart":
      console.log(
        `[daemon] CLI version mismatch (bundled=${bundled} running=${running?.cli_version}) — restarting daemon`,
      );
      pendingVersionRestart = false;
      await restartDaemon();
      return "restarted";
  }
}

/**
 * Exchange the user's JWT for a long-lived PAT via POST /api/tokens. The
 * daemon needs a PAT (or `mul_` / `mdt_` token) because JWTs expire in 30
 * days and signatures are tied to a specific backend instance.
 */
async function mintPat(jwt: string): Promise<string> {
  if (!targetApiBaseUrl) {
    throw new Error("mint PAT: target API URL not set");
  }
  const url = `${targetApiBaseUrl.replace(/\/+$/, "")}/api/tokens`;
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${jwt}`,
    },
    // Omit expires_in_days → server treats as null → non-expiring PAT.
    body: JSON.stringify({ name: "Multica Desktop" }),
  });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    // Attach the status so callers can tell a genuine auth rejection (401 — the
    // session token is dead) apart from a transient failure (5xx, etc.) without
    // string-matching the message.
    throw Object.assign(
      new Error(`mint PAT failed: ${res.status} ${res.statusText} ${body}`),
      { status: res.status },
    );
  }
  const data = (await res.json()) as { token?: unknown };
  if (typeof data.token !== "string" || !data.token.startsWith("mul_")) {
    throw new Error("mint PAT: response missing token");
  }
  return data.token;
}

/**
 * Ensure the active profile's config.json has a usable token for the daemon.
 *
 * - Input from the renderer is the user's JWT (from localStorage) plus the
 *   current user's id, so we can detect session changes.
 * - If the profile already has a cached PAT (`mul_...`) AND the sidecar user
 *   id matches the caller, reuse it — minting fresh on every launch would
 *   accumulate garbage in the user's tokens page.
 * - On user mismatch (or first run) call POST /api/tokens with the JWT to
 *   mint a fresh PAT, overwriting any stale cached PAT. This is the critical
 *   path: without it, a previous user's PAT would be used by a new session.
 * - If the caller happens to pass a PAT directly, write it through.
 * - When we mint fresh and a daemon is already running, restart it so the
 *   new credentials take effect (the Go daemon reads config at startup).
 */
async function syncToken(
  tokenFromRenderer: string,
  userId: string,
): Promise<void> {
  const active = await ensureActiveProfile();
  const config = await readProfileConfig(active.name);
  const previousUserId = await readProfileUserId(active.name);
  const userChanged = Boolean(previousUserId) && previousUserId !== userId;
  const sameUserWithCachedPat =
    !userChanged &&
    previousUserId === userId &&
    typeof config.token === "string" &&
    config.token.startsWith("mul_");

  let finalToken: string;
  if (tokenFromRenderer.startsWith("mul_") || tokenFromRenderer.startsWith("mdt_")) {
    finalToken = tokenFromRenderer;
  } else if (sameUserWithCachedPat) {
    finalToken = config.token as string;
  } else {
    try {
      finalToken = await mintPat(tokenFromRenderer);
      console.log(
        `[daemon] minted PAT for profile "${active.name}" (user_changed=${userChanged})`,
      );
    } catch (err) {
      console.error("[daemon] failed to mint PAT:", err);
      throw err;
    }
  }

  config.token = finalToken;
  if (targetApiBaseUrl) config.server_url = targetApiBaseUrl;
  await writeProfileConfig(active.name, config);
  if (userId) {
    await writeProfileUserId(active.name, userId);
  }

  // If we just rotated credentials onto a running daemon, restart it so the
  // in-memory token in the Go process matches the new config.
  if (userChanged) {
    try {
      const existing = await fetchHealthAtPort(active.port);
      if (daemonStatusAlive(existing?.status)) {
        // Restart whether it's "running" or still "starting" — a booting daemon
        // already loaded the old token at startup, so it must be restarted to
        // pick up the rotated credentials.
        console.log(
          "[daemon] user switched — restarting daemon with new credentials",
        );
        void restartDaemon();
      }
    } catch (err) {
      console.warn("[daemon] restart-on-user-switch failed:", err);
    }
  }
}

async function loadPrefs(): Promise<DaemonPrefs> {
  try {
    const raw = await readFile(PREFS_PATH, "utf-8");
    const parsed = JSON.parse(raw);
    return { ...DEFAULT_PREFS, ...parsed };
  } catch {
    return { ...DEFAULT_PREFS };
  }
}

async function savePrefs(prefs: DaemonPrefs): Promise<void> {
  const dir = join(homedir(), ".rimedeck");
  await mkdir(dir, { recursive: true });
  await writeFile(PREFS_PATH, JSON.stringify(prefs, null, 2), "utf-8");
}

async function clearToken(): Promise<void> {
  const active = await ensureActiveProfile();
  const config = await readProfileConfig(active.name);
  let changed = false;
  if ("token" in config) { delete config.token; changed = true; }
  if ("server_url" in config) { delete config.server_url; changed = true; }
  if (changed) await writeProfileConfig(active.name, config);
  // Always drop the sidecar so a subsequent syncToken from any user is
  // treated as a fresh mint, not a reuse of a stale cached PAT.
  await removeProfileUserId(active.name);
}

// Result of a user-initiated daemon re-authentication. The distinction matters:
// only `session_invalid` justifies signing the user out of the whole app; a
// `transient` failure must keep them logged in so they can retry.
export type ReauthResult =
  | { ok: true }
  | { ok: false; reason: "session_invalid" }
  | { ok: false; reason: "transient"; message: string };

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Recover the local daemon from the "auth_expired" state. Drops the stale
 * cached PAT, mints a fresh one from the current session token, and restarts
 * the daemon so it loads the new credential.
 *
 * Failures are classified rather than collapsed: a 401 from the mint means the
 * session token itself is dead (`session_invalid` → the renderer drives a full
 * re-login); anything else — mint 5xx, a network blip, a config write error, a
 * restart hiccup — is `transient`, leaving the user signed in so they can retry.
 * This mirrors the conservative classification the startup probe already uses.
 */
async function reauthenticate(
  token: string,
  userId: string,
): Promise<ReauthResult> {
  try {
    await clearToken();
    // syncToken mints a fresh PAT because clearToken just removed any cache.
    await syncToken(token, userId);
  } catch (err) {
    if (isAuthStatusError(err)) return { ok: false, reason: "session_invalid" };
    return { ok: false, reason: "transient", message: errorMessage(err) };
  }
  const restart = await restartDaemon();
  if (!restart.success) {
    return {
      ok: false,
      reason: "transient",
      message: restart.error ?? "failed to restart daemon",
    };
  }
  return { ok: true };
}

async function withGuard<T>(fn: () => Promise<T>): Promise<T | { success: false; error: string }> {
  if (operationInProgress) {
    return { success: false, error: "Another daemon operation is in progress" };
  }
  operationInProgress = true;
  try {
    return await fn();
  } finally {
    operationInProgress = false;
  }
}

function profileArgs(active: ActiveProfile): string[] {
  return active.name ? ["--profile", active.name] : [];
}

// Env passed to every CLI child so the daemon process knows it was spawned
// by the Desktop app. The server uses this to mark runtimes as managed and
// hide CLI self-update UI. Computed lazily so it picks up the PATH fix
// applied by fix-path in main/index.ts — as a top-level const it would
// snapshot process.env at import time, before that block runs.
function desktopSpawnEnv(): NodeJS.ProcessEnv {
  return { ...process.env, MULTICA_LAUNCHED_BY: "desktop" };
}

async function startDaemon(): Promise<{ success: boolean; error?: string }> {
  const bin = await resolveCliBinary();
  if (!bin) return { success: false, error: "multica CLI is not installed" };

  const active = await ensureActiveProfile();
  const existing = await fetchHealthAtPort(active.port);
  if (daemonStatusAlive(existing?.status)) {
    // A daemon is already up ("running") or booting ("starting") on this port —
    // don't spawn a second one (the CLI rejects that as "already running").
    // Let polling track it through to "running".
    pollOnce();
    return { success: true };
  }

  // Refuse to start if the profile has no auth token — the daemon will
  // exit immediately with "not authenticated" and the 45 s startup poll
  // makes the failure look like it hung. Wait for the renderer to call
  // syncToken first (triggered by the [user] effect in App.tsx).
  const config = await readProfileConfig(active.name);
  if (typeof config.token !== "string" || config.token.length === 0) {
    return { success: false, error: "daemon profile has no auth token — sync token before starting" };
  }

  currentState = "starting";
  // Begin a fresh auth-probe window for this attempt.
  startingSince = Date.now();
  authProbeDone = false;
  authExpired = false;
  sendStatus({ state: "starting" });

  const args = ["daemon", "start", ...profileArgs(active)];

  return new Promise((resolve) => {
    execFile(
      bin,
      args,
      { timeout: DAEMON_START_EXEC_TIMEOUT_MS, env: desktopSpawnEnv() },
      (err) => {
        if (err) {
          currentState = "stopped";
          sendStatus({ state: "stopped" });
          resolve({ success: false, error: err.message });
          return;
        }
        // Stay in "starting" until pollOnce confirms /health — the CLI
        // returning 0 only means the supervisor was spawned, not that the
        // daemon process is already listening.
        pollOnce();
        resolve({ success: true });
      },
    );
  });
}

async function stopDaemon(): Promise<{ success: boolean; error?: string }> {
  const bin = await resolveCliBinary();
  if (!bin) return { success: false, error: "multica CLI is not installed" };

  const active = await ensureActiveProfile();
  currentState = "stopping";
  // An explicit stop is a clean reset — drop any pending auth-failure verdict.
  authExpired = false;
  startingSince = null;
  sendStatus({ state: "stopping" });

  const args = ["daemon", "stop", ...profileArgs(active)];

  return new Promise((resolve) => {
    execFile(bin, args, { timeout: 15_000 }, (err) => {
      if (err) {
        resolve({ success: false, error: err.message });
      } else {
        resolve({ success: true });
      }
      currentState = "stopped";
      sendStatus({ state: "stopped" });
    });
  });
}

async function restartDaemon(): Promise<{ success: boolean; error?: string }> {
  const stopResult = await stopDaemon();
  if (!stopResult.success) return stopResult;
  return startDaemon();
}

async function pollOnce(): Promise<void> {
  const status = await fetchHealth();
  currentState = status.state;
  sendStatus(status);
  // Retry a deferred version-mismatch restart once the daemon drains.
  if (pendingVersionRestart && status.state === "running") {
    void ensureRunningDaemonVersionMatches();
  }
}

function startPolling(): void {
  if (statusPollTimer) return;
  pollOnce();
  statusPollTimer = setInterval(pollOnce, POLL_INTERVAL_MS);
}

/**
 * Ensures the CLI binary is available, then transitions into the normal
 * stopped/running state machine. Called once at startup and again on
 * user-triggered `daemon:retry-install`.
 */
async function bootstrapCli(): Promise<void> {
  const bin = await resolveCliBinary();
  if (!bin) {
    currentState = "cli_not_found";
    sendStatus({ state: "cli_not_found" });
    return;
  }
  currentState = "stopped";
  sendStatus({ state: "stopped" });
  startPolling();
}

function stopPolling(): void {
  if (statusPollTimer) {
    clearInterval(statusPollTimer);
    statusPollTimer = null;
  }
}

const LOG_TAIL_INITIAL_WINDOW_BYTES = 32 * 1024;
const LOG_TAIL_INITIAL_LINES = 200;
const LOG_TAIL_POLL_MS = 500;

async function readLogRange(
  path: string,
  startAt: number,
  length: number,
): Promise<string> {
  const handle = await open(path, "r");
  try {
    const buffer = Buffer.alloc(length);
    const { bytesRead } = await handle.read(buffer, 0, length, startAt);
    return buffer.subarray(0, bytesRead).toString("utf-8");
  } finally {
    await handle.close();
  }
}

function sendLines(win: BrowserWindow, text: string): void {
  const lines = text.split("\n").filter((line) => line.length > 0);
  for (const line of lines) {
    win.webContents.send("daemon:log-line", line);
  }
}

// Cross-platform tail -f replacement: read the tail of the file once, then
// poll its stat with fs.watchFile and forward any new bytes since the last
// known offset. watchFile works on macOS, Linux, and Windows; spawn("tail")
// would silently fail on Windows.
function startLogTail(win: BrowserWindow, retryCount = 0): void {
  stopLogTail();

  void ensureActiveProfile().then(async (active) => {
    const logPath = profileLogPath(active.name);
    if (!existsSync(logPath)) {
      if (retryCount < LOG_TAIL_MAX_RETRIES) {
        setTimeout(() => startLogTail(win, retryCount + 1), LOG_TAIL_RETRY_MS);
      }
      return;
    }

    let position = 0;
    try {
      const initialStats = await stat(logPath);
      const windowBytes = Math.min(
        initialStats.size,
        LOG_TAIL_INITIAL_WINDOW_BYTES,
      );
      const startAt = initialStats.size - windowBytes;
      if (windowBytes > 0) {
        const text = await readLogRange(logPath, startAt, windowBytes);
        const lines = text
          .split("\n")
          .filter((line) => line.length > 0)
          .slice(-LOG_TAIL_INITIAL_LINES);
        for (const line of lines) {
          win.webContents.send("daemon:log-line", line);
        }
      }
      position = initialStats.size;
    } catch (err) {
      console.warn("[daemon] log tail initial read failed:", err);
      return;
    }

    const listener: StatsListener = (curr) => {
      const target = getMainWindow();
      if (!target) return;
      // File rotated/truncated — restart from the new beginning.
      if (curr.size < position) position = 0;
      if (curr.size === position) return;
      const from = position;
      const length = curr.size - from;
      position = curr.size;
      readLogRange(logPath, from, length)
        .then((text) => sendLines(target, text))
        .catch((err) => {
          console.warn("[daemon] log tail read failed:", err);
        });
    };

    watchFile(logPath, { interval: LOG_TAIL_POLL_MS }, listener);
    logTailWatcher = { path: logPath, listener };
  });
}

function stopLogTail(): void {
  if (logTailWatcher) {
    unwatchFile(logTailWatcher.path, logTailWatcher.listener);
    logTailWatcher = null;
  }
}

// ── WSL detection & lifecycle ───────────────────────────────────────────

export function setupDaemonManager(
  windowGetter: () => BrowserWindow | null,
): void {
  getMainWindow = windowGetter;

  ipcMain.handle("daemon:set-target-api-url", async (_e, url: string) => {
    const normalized = url || null;
    if (targetApiBaseUrl !== normalized) {
      console.log(`[daemon] target API URL set to ${normalized ?? "(none)"}`);
      targetApiBaseUrl = normalized;
      invalidateActiveProfile();
      await pollOnce();
    }
  });
  ipcMain.handle("daemon:start", () => withGuard(() => startDaemon()));
  ipcMain.handle("daemon:stop", () => withGuard(() => stopDaemon()));
  ipcMain.handle("daemon:restart", () => withGuard(() => restartDaemon()));
  ipcMain.handle("daemon:get-status", () => fetchHealth());
  ipcMain.handle("daemon:wsl-list-distros", () => listWslDistros());
  ipcMain.handle("daemon:wsl-get-status", (_event, distro: string) =>
    fetchWslHealth(String(distro || "")),
  );
  ipcMain.handle("daemon:wsl-start", (_event, distro: string) =>
    startWslDaemon(String(distro || "")),
  );
  ipcMain.handle("daemon:wsl-stop", (_event, distro: string) =>
    stopWslDaemon(String(distro || "")),
  );
  ipcMain.handle(
    "daemon:wsl-validate-local-directory",
    (_event, distro: string, path: string) =>
      validateWslLocalDirectory(String(distro || ""), String(path || "")),
  );

  ipcMain.handle("daemon:add-remote-server", async (_e, serverUrl: string, token: string, workspaceId: string) => {
    const active = await ensureActiveProfile();
    try {
      const res = await fetch(`http://127.0.0.1:${active.port}/remote/add`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ server_url: serverUrl, token, workspace_id: workspaceId }),
        signal: AbortSignal.timeout(30_000),
      });
      if (!res.ok) {
        const body = await res.text().catch(() => "");
        throw new Error(body || `${res.status}`);
      }
      return await res.json();
    } catch (err) {
      console.error("[daemon] add-remote-server failed:", err);
      throw err;
    }
  });

  ipcMain.handle("daemon:remove-remote-server", async (_e, serverUrl: string) => {
    const active = await ensureActiveProfile();
    try {
      await fetch(`http://127.0.0.1:${active.port}/remote/remove`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ server_url: serverUrl }),
        signal: AbortSignal.timeout(10_000),
      });
    } catch (err) {
      console.warn("[daemon] remove-remote-server failed:", err);
    }
  });

  // The host's OS name, available regardless of daemon state. The Runtimes
  // page uses it as a fallback identity for "this machine" when no
  // app-managed daemon is reporting a device name (e.g. the daemon runs
  // out-of-band in WSL2). See desktop-runtimes-page.tsx.
  ipcMain.handle("daemon:get-host-name", () => hostname());
  ipcMain.handle(
    "daemon:sync-token",
    (_event, token: string, userId: string) => syncToken(token, userId),
  );
  ipcMain.handle("daemon:clear-token", () => clearToken());
  ipcMain.handle(
    "daemon:reauthenticate",
    (_event, token: string, userId: string) => reauthenticate(token, userId),
  );
  ipcMain.handle("daemon:is-cli-installed", async () => {
    const bin = await resolveCliBinary();
    return bin !== null;
  });
  ipcMain.handle("daemon:retry-install", async () => {
    cachedCliBinary = undefined;
    cliResolvePromise = null;
    // A retry-install may land a new CLI at a different version; drop the
    // cached version string so the next check re-reads the binary.
    cachedCliBinaryVersion = undefined;
    await bootstrapCli();
  });
  ipcMain.handle("daemon:get-prefs", () => loadPrefs());
  ipcMain.handle(
    "daemon:set-prefs",
    (_event, prefs: Partial<DaemonPrefs>) =>
      loadPrefs().then((cur) => {
        const merged = { ...cur, ...prefs };
        return savePrefs(merged).then(() => merged);
      }),
  );
  ipcMain.handle("daemon:auto-start", async () => {
    const prefs = await loadPrefs();
    if (!prefs.autoStart) return;
    const bin = await resolveCliBinary();
    if (!bin) return;
    const health = await fetchHealth();
    if (health.state === "running") {
      // Daemon is up but may be running an older CLI than the one we just
      // bundled. Restart it so the new binary actually takes effect.
      await ensureRunningDaemonVersionMatches();
      return;
    }
    // Don't start if no token is configured — the renderer will call
    // syncToken after login, which triggers autoStart again.
    const active = await ensureActiveProfile();
    const config = await readProfileConfig(active.name);
    if (typeof config.token !== "string" || config.token.length === 0) {
      return;
    }
    await startDaemon();
  });

  ipcMain.on("daemon:start-log-stream", () => {
    const win = getMainWindow();
    if (win) startLogTail(win);
  });

  ipcMain.on("daemon:stop-log-stream", () => {
    stopLogTail();
  });

  // Reveal the daemon's log file in the user's default editor / Console
  // app. Acts as the escape hatch when the in-app log viewer isn't enough
  // (full history, complex search, copy-to-clipboard at scale).
  ipcMain.handle("daemon:open-log-file", async () => {
    const active = await ensureActiveProfile();
    const logPath = profileLogPath(active.name);
    if (!existsSync(logPath)) {
      return { success: false, error: "Log file not found yet" };
    }
    // shell.openPath returns "" on success, error string on failure.
    const error = await shell.openPath(logPath);
    return error === "" ? { success: true } : { success: false, error };
  });

  // First-run CLI install kicks off here. Status bar shows "Setting up…"
  // until the managed binary is on disk (instant on subsequent launches).
  currentState = "installing_cli";
  sendStatus({ state: "installing_cli" });
  void bootstrapCli();

  let isQuitting = false;
  app.on("before-quit", (event) => {
    if (isQuitting) return;
    stopPolling();
    stopLogTail();

    // preventDefault must be called synchronously — calling it inside an
    // async .then() is too late and the app exits before stopDaemon runs.
    event.preventDefault();
    isQuitting = true;

    loadPrefs()
      .then(async (prefs) => {
        if (prefs.autoStop) {
          try {
            await stopDaemon();
          } catch {
            // Best-effort stop on quit
          }
        }
      })
      .catch(() => {})
      .finally(() => app.quit());
  });
}
