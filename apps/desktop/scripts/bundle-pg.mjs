#!/usr/bin/env node
// Downloads pre-built PostgreSQL binaries from EDB and places them into
// apps/desktop/resources/pgsql/ so electron-builder bundles them into
// the packaged app. The asarUnpack: resources/** rule in
// electron-builder.yml extracts them to real files at runtime.
//
// Usage:
//   node scripts/bundle-pg.mjs [--target-platform <darwin|linux|win32>] [--target-arch <x64|arm64>]
//
// When no flags are given, builds for the host platform/arch.
// Graceful: if the archive is already present at the expected path,
// the download is skipped (idempotent for CI caching).

import { createWriteStream, existsSync } from "node:fs";
import { mkdir, rm } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { pipeline } from "node:stream/promises";
import { Readable } from "node:stream";
import { tmpdir } from "node:os";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");
const destDir = join(repoRoot, "apps", "desktop", "resources", "pgsql");

const PG_VERSION = "17.5-1";

function flagValue(argv, flag) {
  const i = argv.indexOf(flag);
  return i === -1 ? undefined : argv[i + 1];
}

const targetPlatform = flagValue(process.argv.slice(2), "--target-platform") ?? process.platform;
const targetArch = flagValue(process.argv.slice(2), "--target-arch") ?? process.arch;

function platformDescriptor(platform, arch) {
  switch (platform) {
    case "darwin":
      return { urlFragment: arch === "arm64" ? "osx-arm64" : "osx", ext: "tar.gz" };
    case "win32":
      return { urlFragment: "windows-x64-binaries", ext: "zip" };
    case "linux":
      return { urlFragment: "linux-x64-binaries", ext: "tar.gz" };
    default:
      throw new Error(`[bundle-pg] unsupported platform: ${platform}`);
  }
}

const { urlFragment, ext } = platformDescriptor(targetPlatform, targetArch);
const downloadUrl = `https://get.enterprisedb.com/postgresql/postgresql-${PG_VERSION}-${urlFragment}.${ext}`;

// The EDB archive extracts to a `pgsql/` directory containing bin/, lib/, share/, etc.
// We place the entire `pgsql/` tree into resources/pgsql/.
const pgCtlName = targetPlatform === "win32" ? "pg_ctl.exe" : "pg_ctl";
const pgCtlExpected = join(destDir, "bin", pgCtlName);

if (existsSync(pgCtlExpected) && !process.argv.includes("--force")) {
  console.log(`[bundle-pg] PostgreSQL already bundled at ${destDir} — skipping download.`);
  process.exit(0);
}

console.log(`[bundle-pg] downloading PostgreSQL ${PG_VERSION} for ${targetPlatform}/${targetArch}`);
console.log(`[bundle-pg] url: ${downloadUrl}`);

const workDir = join(tmpdir(), `rimedeck-bundle-pg-${Date.now()}`);
await mkdir(workDir, { recursive: true });

try {
  const archivePath = join(workDir, `postgresql.${ext}`);

  // Download
  const res = await fetch(downloadUrl, {
    redirect: "follow",
    headers: { "User-Agent": "RimeDeck-Desktop-Build/1.0" },
  });
  if (!res.ok || !res.body) {
    throw new Error(`download failed: ${res.status} ${res.statusText}`);
  }
  await mkdir(dirname(archivePath), { recursive: true });
  const nodeStream = Readable.fromWeb(res.body);
  await pipeline(nodeStream, createWriteStream(archivePath));
  console.log(`[bundle-pg] downloaded to ${archivePath}`);

  // Extract to workDir — EDB archive produces a `pgsql/` subdirectory
  console.log("[bundle-pg] extracting...");
  execFileSync("tar", ["-xf", archivePath, "-C", workDir], { stdio: "inherit" });

  const extractedPgsql = join(workDir, "pgsql");
  if (!existsSync(extractedPgsql)) {
    throw new Error("[bundle-pg] expected pgsql/ directory in archive not found");
  }

  // Move pgsql/ content to resources/pgsql/
  await rm(destDir, { recursive: true, force: true });
  await mkdir(dirname(destDir), { recursive: true });

  // Use platform-native move for cross-device compatibility
  if (process.platform === "win32") {
    execFileSync("cmd", ["/c", "move", extractedPgsql, destDir], { stdio: "inherit" });
  } else {
    execFileSync("mv", [extractedPgsql, destDir], { stdio: "inherit" });
  }

  // macOS: ad-hoc codesign PG binaries to avoid Gatekeeper issues
  if (process.platform === "darwin" && targetPlatform === "darwin") {
    const bins = ["pg_ctl", "initdb", "createdb", "pg_isready", "postgres"];
    for (const bin of bins) {
      const binPath = join(destDir, "bin", bin);
      if (existsSync(binPath)) {
        try {
          execFileSync("codesign", ["-s", "-", "--force", binPath], { stdio: "pipe" });
        } catch {
          // Non-fatal
        }
      }
    }
  }

  console.log(`[bundle-pg] PostgreSQL ${PG_VERSION} bundled at ${destDir}`);
} finally {
  await rm(workDir, { recursive: true, force: true }).catch(() => {});
}
