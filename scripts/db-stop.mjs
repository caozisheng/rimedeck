#!/usr/bin/env node
// Stop the shared local PostgreSQL instance (see scripts/ensure-postgres.mjs).
// Called by `make db-down`.

import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { join, resolve } from "node:path";

const repoRoot = resolve(import.meta.dirname, "..");
const EXE = process.platform === "win32" ? ".exe" : "";
const bundledBin = join(
  repoRoot,
  "apps",
  "desktop",
  "resources",
  "pgsql",
  "bin"
);
const ctl = existsSync(join(bundledBin, `pg_ctl${EXE}`))
  ? join(bundledBin, `pg_ctl${EXE}`)
  : `pg_ctl${EXE}`;
const dataDir = join(repoRoot, ".rimedeck-dev", "pg-data");

if (!existsSync(join(dataDir, "PG_VERSION"))) {
  console.log("✓ Local PostgreSQL not initialized.");
  process.exit(0);
}

const r = spawnSync(ctl, ["stop", "-D", dataDir, "-m", "fast"], {
  stdio: "inherit",
});
process.exit(r.status ?? 0);
