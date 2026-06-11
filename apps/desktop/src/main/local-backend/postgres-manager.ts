import { execFile } from "node:child_process";
import { access, writeFile, mkdir } from "node:fs/promises";
import { constants } from "node:fs";
import { join } from "node:path";
import { getRimedeckDir } from "./config";
import { bundledPgBinDir, isBundledPgAvailable } from "./pg-installer";

const PG_BINS = ["pg_ctl", "initdb", "createdb", "pg_isready", "psql"] as const;
const STARTUP_TIMEOUT_MS = 30_000;
const HEALTH_POLL_MS = 1_000;

interface PgPaths {
  pg_ctl: string;
  initdb: string;
  createdb: string;
  pg_isready: string;
  psql: string;
}

let pgPaths: PgPaths | null = null;
let dataDir: string | null = null;

function execAsync(
  bin: string,
  args: string[],
  opts?: { timeout?: number; env?: NodeJS.ProcessEnv },
): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    execFile(
      bin,
      args,
      { timeout: opts?.timeout ?? 60_000, env: opts?.env },
      (err, stdout, stderr) => {
        if (err) reject(Object.assign(err, { stdout, stderr }));
        else resolve({ stdout, stderr });
      },
    );
  });
}

function binName(name: string): string {
  return process.platform === "win32" ? `${name}.exe` : name;
}

async function which(name: string): Promise<string | null> {
  const cmd = process.platform === "win32" ? "where.exe" : "which";
  try {
    const { stdout } = await execAsync(cmd, [name], { timeout: 5_000 });
    const path = stdout.trim().split("\n")[0]?.trim();
    return path || null;
  } catch {
    return null;
  }
}

async function tryResolvePgFromDir(dir: string): Promise<PgPaths | null> {
  const resolved: Record<string, string> = {};
  for (const bin of PG_BINS) {
    const p = join(dir, binName(bin));
    if (await pathExists(p)) resolved[bin] = p;
    else return null;
  }
  return resolved as unknown as PgPaths;
}

async function tryResolveFromPath(): Promise<PgPaths | null> {
  const resolved: Record<string, string> = {};
  for (const bin of PG_BINS) {
    const path = await which(bin);
    if (path) resolved[bin] = path;
    else return null;
  }
  return resolved as unknown as PgPaths;
}

async function resolvePgBinaries(): Promise<PgPaths> {
  // 1. Check bundled PG shipped inside the app package
  if (isBundledPgAvailable()) {
    const bundled = await tryResolvePgFromDir(bundledPgBinDir());
    if (bundled) return bundled;
  }

  // 2. Check system PATH
  const system = await tryResolveFromPath();
  if (system) return system;

  throw new Error(
    "PostgreSQL not found. The bundled PostgreSQL is missing and no system PostgreSQL was found in PATH.\n" +
      "Please reinstall RimeDeck or install PostgreSQL manually:\n" +
      "  macOS: brew install postgresql@17\n" +
      "  Windows: https://www.postgresql.org/download/windows/\n" +
      "  Linux: sudo apt install postgresql",
  );
}

async function pathExists(p: string): Promise<boolean> {
  try {
    await access(p, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

export async function startPostgres(pgPort: number): Promise<string> {
  pgPaths = await resolvePgBinaries();
  const baseDir = join(getRimedeckDir(), "pg");
  dataDir = join(baseDir, "data");
  const logDir = join(baseDir, "log");

  await mkdir(logDir, { recursive: true });

  if (!(await pathExists(join(dataDir, "PG_VERSION")))) {
    console.log("[local-backend] Initializing PostgreSQL data directory...");
    await mkdir(dataDir, { recursive: true });
    await execAsync(pgPaths.initdb, [
      "--auth=trust",
      "--encoding=UTF8",
      "--locale=C",
      "-D",
      dataDir,
    ]);
  }

  const confOverrides = [
    `listen_addresses = '127.0.0.1'`,
    `port = ${pgPort}`,
    `max_connections = 20`,
    `shared_buffers = 128MB`,
    `log_destination = 'stderr'`,
    `logging_collector = off`,
  ].join("\n");

  await writeFile(
    join(dataDir, "postgresql.auto.conf"),
    confOverrides,
    "utf-8",
  );

  // Stop stale instance if running
  try {
    await execAsync(pgPaths.pg_ctl, ["status", "-D", dataDir], {
      timeout: 5_000,
    });
    console.log("[local-backend] Stopping stale PostgreSQL instance...");
    await execAsync(pgPaths.pg_ctl, ["stop", "-D", dataDir, "-m", "fast"], {
      timeout: 15_000,
    });
  } catch {
    // Not running — expected path
  }

  console.log(`[local-backend] Starting PostgreSQL on port ${pgPort}...`);
  await execAsync(
    pgPaths.pg_ctl,
    ["start", "-D", dataDir, "-w", "-t", "30", "-l", join(logDir, "postgresql.log")],
    {
      env: { ...process.env, LC_ALL: "C" },
    },
  );

  // Wait for readiness
  let pgReady = false;
  const deadline = Date.now() + STARTUP_TIMEOUT_MS;
  while (Date.now() < deadline) {
    try {
      await execAsync(pgPaths.pg_isready, [
        "-h",
        "127.0.0.1",
        "-p",
        String(pgPort),
      ]);
      pgReady = true;
      break;
    } catch {
      await new Promise((r) => setTimeout(r, HEALTH_POLL_MS));
    }
  }
  if (!pgReady) {
    throw new Error(
      `PostgreSQL failed to become ready within ${STARTUP_TIMEOUT_MS / 1000}s`,
    );
  }

  // Create database (idempotent)
  try {
    await execAsync(pgPaths.createdb, [
      "-h",
      "127.0.0.1",
      "-p",
      String(pgPort),
      "multica",
    ]);
    console.log("[local-backend] Created database 'multica'.");
  } catch (err: unknown) {
    const msg = (err as { stderr?: string }).stderr ?? "";
    if (!msg.includes("already exists")) throw err;
  }

  // Enable pgcrypto extension (required by upstream migrations for gen_random_uuid)
  await execAsync(pgPaths.psql, [
    "-h",
    "127.0.0.1",
    "-p",
    String(pgPort),
    "-d",
    "multica",
    "-c",
    "CREATE EXTENSION IF NOT EXISTS pgcrypto",
  ]);

  const connStr = `postgres://localhost:${pgPort}/multica?sslmode=disable`;
  console.log(`[local-backend] PostgreSQL ready: ${connStr}`);
  return connStr;
}

export async function stopPostgres(): Promise<void> {
  if (!pgPaths || !dataDir) return;
  console.log("[local-backend] Stopping PostgreSQL...");
  try {
    await execAsync(pgPaths.pg_ctl, ["stop", "-D", dataDir, "-m", "fast"], {
      timeout: 15_000,
    });
  } catch {
    // Already stopped
  }
  pgPaths = null;
  dataDir = null;
}
