#!/usr/bin/env node
// Ensure a local PostgreSQL (native, no Docker) is running and the target
// database exists.
//
// RimeDeck fork policy: zero Docker. Dev PostgreSQL comes from the same
// pre-built binaries the desktop app bundles (apps/desktop/resources/pgsql,
// fetched by apps/desktop/scripts/bundle-pg.mjs), falling back to a system
// PostgreSQL on PATH. The data directory lives under .rimedeck-dev/ in the
// repo (gitignored); main checkout and worktrees share one instance and
// isolate per-database, like the old shared docker container did.
//
// Usage: node scripts/ensure-postgres.mjs [env-file]
//
// Env (from the env file, same vars the Makefile exports):
//   POSTGRES_DB / POSTGRES_USER / POSTGRES_PORT / DATABASE_URL

import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { readFileSync } from "node:fs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");

// ---------- Load env file ----------
const envFile = process.argv[2] ?? ".env";
const envPath = resolve(repoRoot, envFile);
if (!existsSync(envPath)) {
  console.error(`Missing env file: ${envFile}`);
  console.error("Create .env from .env.example, or run 'make worktree-env' and use .env.worktree.");
  process.exit(1);
}

// Minimal .env parser: KEY=VALUE lines, ignores comments/quotes like dotenv.
for (const line of readFileSync(envPath, "utf8").split(/\r?\n/)) {
  const m = line.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/);
  if (!m) continue;
  let val = m[2];
  if (
    (val.startsWith('"') && val.endsWith('"')) ||
    (val.startsWith("'") && val.endsWith("'"))
  ) {
    val = val.slice(1, -1);
  }
  if (!(m[1] in process.env)) process.env[m[1]] = val;
}

const POSTGRES_DB = process.env.POSTGRES_DB ?? "multica";
const POSTGRES_PORT = Number(process.env.POSTGRES_PORT ?? 5432);
const DATABASE_URL = process.env.DATABASE_URL ?? "";

// ---------- Parse DATABASE_URL (remote detection) ----------
let dbHost = "";
let dbPort = POSTGRES_PORT;
let dbName = POSTGRES_DB;
if (DATABASE_URL) {
  try {
    const u = new URL(DATABASE_URL);
    dbHost = u.hostname;
    if (u.port) dbPort = Number(u.port);
    if (u.pathname && u.pathname.length > 1) dbName = u.pathname.slice(1).split("/")[0];
  } catch {
    // fall through with defaults
  }
}

const isRemote =
  dbHost !== "" && !["localhost", "127.0.0.1", "::1"].includes(dbHost);

// ---------- Locate postgres binaries ----------
const EXE = process.platform === "win32" ? ".exe" : "";
const bundledBin = join(repoRoot, "apps", "desktop", "resources", "pgsql", "bin");

function resolveFromDir(dir) {
  if (!existsSync(join(dir, `pg_isready${EXE}`))) return null;
  const p = {};
  for (const b of ["pg_ctl", "initdb", "createdb", "pg_isready", "psql"]) {
    p[b] = join(dir, `${b}${EXE}`);
  }
  return p;
}

const bins =
  resolveFromDir(bundledBin) ??
  resolveFromDir("/usr/local/opt/postgresql@17/bin") ??
  resolveFromDir("/opt/homebrew/opt/postgresql@17/bin");

if (!bins) {
  console.error("✗ No local PostgreSQL found.");
  console.error("  Run: node apps/desktop/scripts/bundle-pg.mjs   (downloads the bundled PG 17)");
  console.error("  or install PostgreSQL on PATH.");
  process.exit(1);
}

function run(bin, args, opts = {}) {
  return new Promise((resolveP, rejectP) => {
    const child = spawn(bin, args, {
      stdio: opts.capture ? ["ignore", "pipe", "pipe"] : "inherit",
      env: { ...process.env, LC_ALL: "C" },
      ...opts.spawn,
    });
    let out = "";
    if (opts.capture) {
      child.stdout.on("data", (d) => (out += d));
      child.stderr.on("data", (d) => (out += d));
    }
    child.on("error", rejectP);
    child.on("close", (code) => {
      if (code === 0 || opts.okCodes?.includes(code)) resolveP(out);
      else rejectP(new Error(`${bin} exited ${code}`));
    });
  });
}

function isUp() {
  const r = spawnSync(bins.pg_isready, ["-h", "localhost", "-p", String(dbPort)], {
    encoding: "utf8",
  });
  return r.status === 0;
}

async function main() {
  if (isRemote) {
    console.log(`==> Remote database detected (host: ${dbHost}). Skipping local PostgreSQL.`);
    const deadline = Date.now() + 60_000;
    while (!isUp() && Date.now() < deadline) {
      await new Promise((r) => setTimeout(r, 1000));
    }
    console.log(`✓ PostgreSQL ready (remote: ${dbHost}:${dbPort}). Database: ${dbName}`);
    return;
  }

  const baseDir = join(repoRoot, ".rimedeck-dev");
  const dataDir = join(baseDir, "pg-data");
  const logDir = join(baseDir, "pg-log");
  mkdirSync(logDir, { recursive: true });

  if (!isUp()) {
    if (!existsSync(join(dataDir, "PG_VERSION"))) {
      console.log(`==> Initializing local PostgreSQL data directory at ${dataDir}...`);
      mkdirSync(dataDir, { recursive: true });
      const shareDir = join(bundledBin, "..", "share");
      const args = [
        "--auth=trust",
        "--encoding=UTF8",
        "--locale=C",
        "-D",
        dataDir,
      ];
      if (existsSync(shareDir)) args.splice(2, 0, "-L", shareDir);
      await run(bins.initdb, args, { capture: true });
    }
    console.log(`==> Starting local PostgreSQL on port ${dbPort}...`);
    await run(bins.pg_ctl, [
      "start",
      "-D",
      dataDir,
      "-w",
      "-t",
      "30",
      "-l",
      join(logDir, "postgresql.log"),
      "-o",
      `-p ${dbPort} -c listen_addresses=localhost`,
    ]);
  }

  console.log("==> Waiting for PostgreSQL to be ready...");
  const deadline = Date.now() + 30_000;
  while (!isUp() && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 1000));
  }
  if (!isUp()) {
    console.error("✗ PostgreSQL did not become ready within 30s.");
    console.error(`  Check ${join(logDir, "postgresql.log")}`);
    process.exit(1);
  }

  console.log(`==> Ensuring database '${dbName}' exists...`);
  const exists = await run(
    bins.psql,
    ["-h", "localhost", "-p", String(dbPort), "-d", "postgres", "-Atqc",
      `SELECT 1 FROM pg_database WHERE datname = '${dbName}'`],
    { capture: true },
  ).catch(() => "");

  if (exists.trim() !== "1") {
    await run(bins.createdb, ["-h", "localhost", "-p", String(dbPort), dbName]);
  }

  console.log("==> Ensuring app role and database ownership...");
  const appUser = process.env.POSTGRES_USER ?? "multica";
  const appPass = process.env.POSTGRES_PASSWORD ?? "multica";
  await run(
    bins.psql,
    [
      "-h", "localhost", "-p", String(dbPort), "-d", "postgres",
      "-v", "ON_ERROR_STOP=1",
      "-c",
      `DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='${appUser}') THEN CREATE ROLE ${appUser} LOGIN PASSWORD '${appPass}'; END IF; END $$;`,
      "-c", `ALTER DATABASE "${dbName}" OWNER TO ${appUser};`,
    ],
    { capture: true },
  ).catch((e) => {
    console.warn(`  (role/owner provisioning note: ${e.message})`);
  });

  console.log(`✓ PostgreSQL ready (local native, port ${dbPort}). Database: ${dbName}`);
}

main().catch((err) => {
  console.error(`✗ ${err.message}`);
  process.exit(1);
});
