import { readFile, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { homedir } from "node:os";
import { randomBytes } from "node:crypto";
import { findFreePort, isPortAvailable } from "./port-utils";

export interface LocalConfig {
  pgPort: number;
  backendPort: number;
  jwtSecret: string;
  firstRunAt: string;
}

const DEFAULT_PG_PORT = 15432;
const DEFAULT_BACKEND_PORT = 18080;

export function getRimedeckDir(): string {
  return process.env.RIMEDECK_HOME ?? join(homedir(), ".rimedeck");
}

export async function loadOrCreateConfig(): Promise<LocalConfig> {
  const configPath = join(getRimedeckDir(), "config.json");
  try {
    const raw = await readFile(configPath, "utf-8");
    const config: LocalConfig = JSON.parse(raw);
    const pgOk = await isPortAvailable(config.pgPort);
    const backendOk = await isPortAvailable(config.backendPort);
    if (pgOk && backendOk) return config;

    if (!pgOk) config.pgPort = await findFreePort(DEFAULT_PG_PORT);
    if (!backendOk) config.backendPort = await findFreePort(DEFAULT_BACKEND_PORT);
    await saveConfig(config);
    return config;
  } catch {
    return createConfig();
  }
}

async function createConfig(): Promise<LocalConfig> {
  const config: LocalConfig = {
    pgPort: await findFreePort(DEFAULT_PG_PORT),
    backendPort: await findFreePort(DEFAULT_BACKEND_PORT),
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
