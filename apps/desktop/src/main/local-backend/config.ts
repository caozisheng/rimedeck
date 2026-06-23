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
    firstRunAt: new Date().toISOString(),
  };
  await saveConfig(config);
  return config;
}

async function saveConfig(config: LocalConfig): Promise<void> {
  const dir = getRimedeckDir();
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, "config.json"), JSON.stringify(config, null, 2), "utf-8");
}
