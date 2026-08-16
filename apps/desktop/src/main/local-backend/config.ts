import { readFile, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { homedir } from "node:os";
import { randomBytes } from "node:crypto";
import { isPortAvailable } from "./port-utils";

export interface LocalConfig {
  [key: string]: unknown;
  pgPort: number;
  backendPort: number;
  jwtSecret: string;
  // Versioned keyring the embedded server expects at GITLAB_TRACKER_KEYS.
  // Format: `v1=<base64(32 random bytes)>[,v2=…]`. Auto-provisioned on
  // first run so the desktop user is never asked to paste a raw key
  // before adding a GitLab tracker. Rotation is a future concern —
  // ciphertexts store the version int, so appending `,v2=…` keeps old
  // rows readable while new writes pick up the new key.
  gitlabTrackerKey: string;
  firstRunAt: string;
}

const DEFAULT_PG_PORT = 15432;
const DEFAULT_BACKEND_PORT = 18080;
const MIN_PORT = 10_240;
const MAX_PORT = 65_535;

export function getRimedeckDir(): string {
  return process.env.RIMEDECK_HOME ?? join(homedir(), ".rimedeck");
}

export async function loadOrCreateConfig(): Promise<LocalConfig> {
  const configPath = join(getRimedeckDir(), "config.json");
  try {
    const raw = await readFile(configPath, "utf-8");
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const { config, changed } = await normalizeConfig(parsed);
    if (changed) await saveConfig(config);
    return config;
  } catch {
    return createConfig();
  }
}

function isValidPort(value: unknown): value is number {
  return (
    typeof value === "number" &&
    Number.isInteger(value) &&
    value >= MIN_PORT &&
    value <= MAX_PORT
  );
}

async function findFreeLocalBackendPort(preferred?: number): Promise<number> {
  if (preferred && isValidPort(preferred) && await isPortAvailable(preferred)) {
    return preferred;
  }
  for (let port = MIN_PORT; port <= MAX_PORT; port += 1) {
    if (await isPortAvailable(port)) return port;
  }
  throw new Error("No local backend port available");
}

async function normalizeConfig(
  parsed: Record<string, unknown>,
): Promise<{ config: LocalConfig; changed: boolean }> {
  const config = { ...parsed } as LocalConfig;
  let changed = false;

  if (!isValidPort(config.pgPort)) {
    config.pgPort = await findFreeLocalBackendPort(DEFAULT_PG_PORT);
    changed = true;
  } else if (!(await isPortAvailable(config.pgPort))) {
    config.pgPort = await findFreeLocalBackendPort(DEFAULT_PG_PORT);
    changed = true;
  }

  if (!isValidPort(config.backendPort) || config.backendPort === config.pgPort) {
    config.backendPort = await findFreeLocalBackendPort(DEFAULT_BACKEND_PORT);
    changed = true;
  } else if (!(await isPortAvailable(config.backendPort))) {
    config.backendPort = await findFreeLocalBackendPort(DEFAULT_BACKEND_PORT);
    changed = true;
  }

  if (config.backendPort === config.pgPort) {
    config.backendPort = await findFreeLocalBackendPort();
    changed = true;
  }

  if (typeof config.jwtSecret !== "string" || config.jwtSecret.length === 0) {
    config.jwtSecret = randomBytes(32).toString("hex");
    changed = true;
  }

  if (typeof config.gitlabTrackerKey !== "string" || !isValidGitlabTrackerKey(config.gitlabTrackerKey)) {
    config.gitlabTrackerKey = generateGitlabTrackerKey();
    changed = true;
  }

  if (typeof config.firstRunAt !== "string" || config.firstRunAt.length === 0) {
    config.firstRunAt = new Date().toISOString();
    changed = true;
  }

  return { config, changed };
}

async function createConfig(): Promise<LocalConfig> {
  const config: LocalConfig = {
    pgPort: await findFreeLocalBackendPort(DEFAULT_PG_PORT),
    backendPort: await findFreeLocalBackendPort(DEFAULT_BACKEND_PORT),
    jwtSecret: randomBytes(32).toString("hex"),
    gitlabTrackerKey: generateGitlabTrackerKey(),
    firstRunAt: new Date().toISOString(),
  };
  await saveConfig(config);
  return config;
}

// generateGitlabTrackerKey mints a v1 entry matching the server's
// keyring parser (server/internal/handler/gitlab_tracker.go): 32 random
// bytes, standard base64, prefixed with the version tag.
function generateGitlabTrackerKey(): string {
  return `v1=${randomBytes(32).toString("base64")}`;
}

// isValidGitlabTrackerKey is a defensive shape check so a hand-edited
// config.json can't smuggle in a malformed entry that would make the
// server fail-closed on every request. We accept multi-entry rings
// (comma-separated) because rotation adds `,v2=…` without dropping v1.
function isValidGitlabTrackerKey(raw: string): boolean {
  const entries = raw.split(",").map((s) => s.trim()).filter(Boolean);
  if (entries.length === 0) return false;
  for (const entry of entries) {
    const match = entry.match(/^v(\d+)=([A-Za-z0-9+/=]+)$/);
    if (!match) return false;
    try {
      if (Buffer.from(match[2], "base64").length !== 32) return false;
    } catch {
      return false;
    }
  }
  return true;
}

async function saveConfig(config: LocalConfig): Promise<void> {
  const dir = getRimedeckDir();
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, "config.json"), JSON.stringify(config, null, 2), "utf-8");
}
