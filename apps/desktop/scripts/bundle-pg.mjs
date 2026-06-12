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

import { existsSync } from "node:fs";
import { cp, mkdir, rm } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
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

  // Use curl for the download — Node's fetch gets 403 from EDB on CI runners
  execFileSync("curl", [
    "-fSL",
    "-o", archivePath,
    "-A", "Mozilla/5.0 (compatible; RimeDeck-Build/1.0)",
    downloadUrl,
  ], { stdio: "inherit" });
  console.log(`[bundle-pg] downloaded to ${archivePath}`);

  // Extract to workDir — EDB archive produces a `pgsql/` subdirectory
  console.log("[bundle-pg] extracting...");
  execFileSync("tar", ["-xf", archivePath, "-C", workDir], { stdio: "inherit" });

  const extractedPgsql = join(workDir, "pgsql");
  if (!existsSync(extractedPgsql)) {
    throw new Error("[bundle-pg] expected pgsql/ directory in archive not found");
  }

  // Copy pgsql/ content to resources/pgsql/
  await rm(destDir, { recursive: true, force: true });
  await mkdir(dirname(destDir), { recursive: true });
  await cp(extractedPgsql, destDir, { recursive: true });

  // macOS: rewrite dylib paths from absolute Homebrew/EDB paths to
  // @executable_path/../lib/ so the bundled binaries find their libs
  // inside the app bundle instead of /opt/homebrew/... or /Library/...
  if (targetPlatform === "darwin") {
    const libDir = join(destDir, "lib");
    const libPgDir = join(destDir, "lib", "postgresql");
    const binDir = join(destDir, "bin");

    // Helper: given an absolute dep path, return the rewritten @executable_path
    // reference if the matching file exists in our bundled lib/ or lib/postgresql/.
    function rewrittenDep(dep, relativeTo) {
      const libName = dep.split("/").pop();
      if (existsSync(join(libPgDir, libName))) {
        return relativeTo === "bin"
          ? `@executable_path/../lib/postgresql/${libName}`
          : relativeTo === "lib"
          ? `@loader_path/postgresql/${libName}`
          : `@loader_path/${libName}`;
      }
      if (existsSync(join(libDir, libName))) {
        return relativeTo === "bin"
          ? `@executable_path/../lib/${libName}`
          : relativeTo === "lib"
          ? `@loader_path/${libName}`
          : `@loader_path/../${libName}`;
      }
      return null;
    }

    // Rewrite all binaries in bin/
    const { readdirSync } = await import("node:fs");
    for (const bin of readdirSync(binDir)) {
      const binPath = join(binDir, bin);
      try {
        const otoolOut = execFileSync("otool", ["-L", binPath], { encoding: "utf-8" });
        for (const line of otoolOut.split("\n")) {
          const match = line.trim().match(/^(.+\.dylib)\s/);
          if (!match) continue;
          const dep = match[1];
          if (dep.startsWith("@") || dep.startsWith("/usr/lib") || dep.startsWith("/System")) continue;
          const newDep = rewrittenDep(dep, "bin");
          if (newDep) {
            execFileSync("install_name_tool", ["-change", dep, newDep, binPath], { stdio: "pipe" });
          }
        }
      } catch (err) {
        console.warn(`[bundle-pg] rewrite dylib for ${bin} (non-fatal):`, err.message);
      }
    }

    // Rewrite dylibs in lib/ (top-level)
    for (const libFile of readdirSync(libDir)) {
      if (!libFile.endsWith(".dylib")) continue;
      const libPath = join(libDir, libFile);
      try {
        execFileSync("install_name_tool", [
          "-id", `@executable_path/../lib/${libFile}`, libPath,
        ], { stdio: "pipe" });
        const otoolOut = execFileSync("otool", ["-L", libPath], { encoding: "utf-8" });
        for (const line of otoolOut.split("\n")) {
          const match = line.trim().match(/^(.+\.dylib)\s/);
          if (!match) continue;
          const dep = match[1];
          if (dep.startsWith("@") || dep.startsWith("/usr/lib") || dep.startsWith("/System")) continue;
          const newDep = rewrittenDep(dep, "lib");
          if (newDep) {
            execFileSync("install_name_tool", ["-change", dep, newDep, libPath], { stdio: "pipe" });
          }
        }
      } catch { /* non-fatal */ }
    }

    // Rewrite dylibs in lib/postgresql/
    if (existsSync(libPgDir)) {
      for (const libFile of readdirSync(libPgDir)) {
        if (!libFile.endsWith(".dylib")) continue;
        const libPath = join(libPgDir, libFile);
        try {
          execFileSync("install_name_tool", [
            "-id", `@loader_path/${libFile}`, libPath,
          ], { stdio: "pipe" });
          const otoolOut = execFileSync("otool", ["-L", libPath], { encoding: "utf-8" });
          for (const line of otoolOut.split("\n")) {
            const match = line.trim().match(/^(.+\.dylib)\s/);
            if (!match) continue;
            const dep = match[1];
            if (dep.startsWith("@") || dep.startsWith("/usr/lib") || dep.startsWith("/System")) continue;
            const newDep = rewrittenDep(dep, "libpg");
            if (newDep) {
              execFileSync("install_name_tool", ["-change", dep, newDep, libPath], { stdio: "pipe" });
            }
          }
        } catch { /* non-fatal */ }
      }
    }
  }

  // macOS: ad-hoc codesign PG binaries + libs to avoid Gatekeeper issues
  // (must run AFTER install_name_tool — codesign invalidates on modification)
  if (process.platform === "darwin" && targetPlatform === "darwin") {
    const { readdirSync } = await import("node:fs");
    const signDirs = [
      join(destDir, "bin"),
      join(destDir, "lib"),
      join(destDir, "lib", "postgresql"),
    ];
    for (const dir of signDirs) {
      if (!existsSync(dir)) continue;
      for (const f of readdirSync(dir)) {
        const fp = join(dir, f);
        try {
          execFileSync("codesign", ["-s", "-", "--force", fp], { stdio: "pipe" });
        } catch { /* non-fatal */ }
      }
    }
  }

  console.log(`[bundle-pg] PostgreSQL ${PG_VERSION} bundled at ${destDir}`);
} finally {
  await rm(workDir, { recursive: true, force: true }).catch(() => {});
}
