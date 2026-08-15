#!/usr/bin/env node
// Drop and recreate a database on the local PostgreSQL instance.
// Called by `make db-reset`.
//
// Usage: node scripts/db-reset.mjs [env-file] <db-name>

import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");

const envFile = process.argv[2]?.endsWith(".env") ? process.argv[2] : ".env";
const dbName = process.argv[2]?.endsWith(".env")
  ? process.argv[3]
  : process.argv[2];

if (!dbName) {
  console.error("Usage: node scripts/db-reset.mjs [env-file] <db-name>");
  process.exit(1);
}

// Parse env for POSTGRES_PORT
const envPath = resolve(repoRoot, envFile);
let dbPort = 5432;
if (existsSync(envPath)) {
  for (const line of readFileSync(envPath, "utf8").split(/\r?\n/)) {
    const m = line.match(/^\s*POSTGRES_PORT\s*=\s*(\d+)/);
    if (m) dbPort = Number(m[1]);
  }
}

// Locate psql
const EXE = process.platform === "win32" ? ".exe" : "";
const bundledBin = join(
  repoRoot,
  "apps",
  "desktop",
  "resources",
  "pgsql",
  "bin"
);
const psql = existsSync(join(bundledBin, `psql${EXE}`))
  ? join(bundledBin, `psql${EXE}`)
  : `psql${EXE}`;

// Drop + recreate
const r = spawnSync(
  psql,
  [
    "-h",
    "localhost",
    "-p",
    String(dbPort),
    "-d",
    "postgres",
    "-v",
    "ON_ERROR_STOP=1",
    "-c",
    `DROP DATABASE IF EXISTS "${dbName}" WITH (FORCE);`,
    "-c",
    `CREATE DATABASE "${dbName}";`,
  ],
  { stdio: "inherit" }
);

process.exit(r.status ?? 1);
