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
    const bins = ["pg_ctl", "initdb", "createdb", "pg_isready", "postgres", "psql"];
    for (const bin of bins) {
      const binPath = join(binDir, bin);
      if (!existsSync(binPath)) continue;
      try {
        const otoolOut = execFileSync("otool", ["-L", binPath], { encoding: "utf-8" });
        for (const line of otoolOut.split("\n")) {
          const match = line.trim().match(/^(.+\.dylib)\s/);
          if (!match) continue;
          const dep = match[1];
          // Only rewrite absolute paths (not @executable_path, @rpath, /usr/lib)
          if (dep.startsWith("@") || dep.startsWith("/usr/lib") || dep.startsWith("/System")) continue;
          const libName = dep.split("/").pop();
          if (existsSync(join(libDir, libName))) {
            execFileSync("install_name_tool", [
              "-change", dep, `@executable_path/../lib/${libName}`, binPath,
            ], { stdio: "pipe" });
          } else if (existsSync(join(libPgDir, libName))) {
            execFileSync("install_name_tool", [
              "-change", dep, `@executable_path/../lib/postgresql/${libName}`, binPath,
            ], { stdio: "pipe" });
          }
        }
      } catch (err) {
        console.warn(`[bundle-pg] rewrite dylib for ${bin} (non-fatal):`, err.message);
      }
    }
    // Also fix dylibs that reference other dylibs with absolute paths
    try {
      const { readdirSync } = await import("node:fs");
      for (const libFile of readdirSync(libDir)) {
        if (!libFile.endsWith(".dylib")) continue;
        const libPath = join(libDir, libFile);
        try {
          // Fix the install name of the lib itself
          execFileSync("install_name_tool", [
            "-id", `@executable_path/../lib/${libFile}`, libPath,
          ], { stdio: "pipe" });
          // Fix references to other libs
          const otoolOut = execFileSync("otool", ["-L", libPath], { encoding: "utf-8" });
          for (const line of otoolOut.split("\n")) {
            const match = line.trim().match(/^(.+\.dylib)\s/);
            if (!match) continue;
            const dep = match[1];
            if (dep.startsWith("@") || dep.startsWith("/usr/lib") || dep.startsWith("/System")) continue;
            const depName = dep.split("/").pop();
            if (existsSync(join(libDir, depName))) {
              execFileSync("install_name_tool", [
                "-change", dep, `@executable_path/../lib/${depName}`, libPath,
              ], { stdio: "pipe" });
            }
          }
        } catch {
          // Non-fatal per-lib
        }
      }
      // Fix dylibs in lib/postgresql/ that reference other dylibs
      if (existsSync(libPgDir)) {
        for (const libFile of readdirSync(libPgDir)) {
          if (!libFile.endsWith(".dylib")) continue;
          const libPath = join(libPgDir, libFile);
          try {
            execFileSync("install_name_tool", [
              "-id", `@executable_path/../lib/postgresql/${libFile}`, libPath,
            ], { stdio: "pipe" });
            const otoolOut = execFileSync("otool", ["-L", libPath], { encoding: "utf-8" });
            for (const line of otoolOut.split("\n")) {
              const match = line.trim().match(/^(.+\.dylib)\s/);
              if (!match) continue;
              const dep = match[1];
              if (dep.startsWith("@") || dep.startsWith("/usr/lib") || dep.startsWith("/System")) continue;
              const depName = dep.split("/").pop();
              if (existsSync(join(libPgDir, depName))) {
                execFileSync("install_name_tool", [
                  "-change", dep, `@executable_path/../lib/postgresql/${depName}`, libPath,
                ], { stdio: "pipe" });
              } else if (existsSync(join(libDir, depName))) {
                execFileSync("install_name_tool", [
                  "-change", dep, `@executable_path/../lib/${depName}`, libPath,
                ], { stdio: "pipe" });
              }
            }
          } catch {
            // Non-fatal per-lib
          }
        }
      }
    } catch {
      // Non-fatal
    }
  }

  // macOS: ad-hoc codesign PG binaries + libs to avoid Gatekeeper issues
  // (must run AFTER install_name_tool — codesign invalidates on modification)
  if (process.platform === "darwin" && targetPlatform === "darwin") {
    const bins = ["pg_ctl", "initdb", "createdb", "pg_isready", "postgres", "psql"];
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
    // Codesign dylibs too
    try {
      const { readdirSync } = await import("node:fs");
      for (const f of readdirSync(join(destDir, "lib"))) {
        if (!f.endsWith(".dylib")) continue;
        try {
          execFileSync("codesign", ["-s", "-", "--force", join(destDir, "lib", f)], { stdio: "pipe" });
        } catch { /* non-fatal */ }
      }
      // Codesign lib/postgresql/ dylibs
      const pgLibDir = join(destDir, "lib", "postgresql");
      if (existsSync(pgLibDir)) {
        for (const f of readdirSync(pgLibDir)) {
          if (!f.endsWith(".dylib")) continue;
          try {
            execFileSync("codesign", ["-s", "-", "--force", join(pgLibDir, f)], { stdio: "pipe" });
          } catch { /* non-fatal */ }
        }
      }
    } catch { /* non-fatal */ }
  }

  console.log(`[bundle-pg] PostgreSQL ${PG_VERSION} bundled at ${destDir}`);
} finally {
  await rm(workDir, { recursive: true, force: true }).catch(() => {});
}
