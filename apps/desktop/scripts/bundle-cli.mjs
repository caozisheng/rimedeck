#!/usr/bin/env node
// Builds Go binaries from server/cmd/* and copies them into
// apps/desktop/resources/bin/ so electron-vite (dev) and electron-
// builder (prod) pick them up.
//
// Three binaries:
//   multica         — CLI + daemon (server/cmd/multica)
//   multica-server  — HTTP API server (server/cmd/server)
//   multica-migrate — DB migration runner (server/cmd/migrate)
//
// ldflags mirror `make build` so `multica --version` reports a meaningful
// version / commit / date.
//
// Graceful: if `go` is not installed (e.g. frontend-only contributor), we
// skip the build and fall through to auto-install at runtime. A genuine
// Go compile error is fatal — you want that to block dev, not hide.

import { access, chmod, copyFile, cp, mkdir, rm } from "node:fs/promises";
import { constants } from "node:fs";
import { execFileSync, execSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");
const serverDir = join(repoRoot, "server");

const PLATFORM_TO_GOOS = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const SUPPORTED_ARCHS = new Set(["x64", "arm64"]);

function runtimePlatformFromArgs(argv) {
  const flagIndex = argv.indexOf("--target-platform");
  if (flagIndex === -1) return process.platform;
  return argv[flagIndex + 1] ?? "";
}

function runtimeArchFromArgs(argv) {
  const flagIndex = argv.indexOf("--target-arch");
  if (flagIndex === -1) return process.arch;
  return argv[flagIndex + 1] ?? "";
}

function normalizeRuntimePlatform(platform) {
  if (platform in PLATFORM_TO_GOOS) return platform;
  throw new Error(
    `[bundle-cli] unsupported target platform: ${platform}. ` +
      "Use darwin, linux, or win32.",
  );
}

function normalizeRuntimeArch(arch) {
  if (SUPPORTED_ARCHS.has(arch)) return arch;
  throw new Error(
    `[bundle-cli] unsupported target architecture: ${arch}. ` +
      "Use x64 or arm64.",
  );
}

const BINARIES = [
  { cmd: "./cmd/multica",  outputName: "multica" },
  { cmd: "./cmd/server",   outputName: "multica-server" },
  { cmd: "./cmd/migrate",  outputName: "multica-migrate" },
];
const WSL_CLI_TARGETS = [
  { goarch: "amd64", dir: "linux-amd64" },
  { goarch: "arm64", dir: "linux-arm64" },
];

function binaryNameForPlatform(name, platform) {
  return platform === "win32" ? `${name}.exe` : name;
}

const targetPlatform = normalizeRuntimePlatform(
  runtimePlatformFromArgs(process.argv.slice(2)),
);
const targetArch = normalizeRuntimeArch(runtimeArchFromArgs(process.argv.slice(2)));
const goos = PLATFORM_TO_GOOS[targetPlatform];
const goarch = targetArch === "x64" ? "amd64" : targetArch;
const destDir = join(repoRoot, "apps", "desktop", "resources", "bin");

function sh(cmd) {
  try {
    return execSync(cmd, { encoding: "utf-8" }).trim();
  } catch {
    return "";
  }
}

function hasGo() {
  try {
    execSync("go version", { stdio: "pipe" });
    return true;
  } catch {
    return false;
  }
}

async function exists(p) {
  try {
    await access(p, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

if (hasGo()) {
  const version = sh("git describe --tags --match 'v*' --always --dirty") || "dev";
  const commit = sh("git rev-parse --short HEAD") || "unknown";
  const date = new Date().toISOString().replace(/\.\d+Z$/, "Z");
  const ldflags = `-X main.version=${version} -X main.commit=${commit} -X main.date=${date}`;

  await mkdir(join(serverDir, "bin", `${goos}-${goarch}`), { recursive: true });

  for (const { cmd, outputName } of BINARIES) {
    const binName = binaryNameForPlatform(outputName, targetPlatform);
    const srcBinary = join(serverDir, "bin", `${goos}-${goarch}`, binName);
    console.log(
      `[bundle-cli] go build ${cmd} → ${srcBinary} (${goos}/${goarch}, version=${version} commit=${commit})`,
    );
    execFileSync(
      "go",
      ["build", "-ldflags", ldflags, "-o", srcBinary, cmd],
      {
        cwd: serverDir,
        stdio: "inherit",
        env: {
          ...process.env,
          CGO_ENABLED: "0",
          GOOS: goos,
          GOARCH: goarch,
        },
      },
    );
  }

  if (targetPlatform === "win32") {
    for (const { goarch: wslGoarch } of WSL_CLI_TARGETS) {
      const srcBinary = join(serverDir, "bin", `linux-${wslGoarch}`, "multica");
      await mkdir(dirname(srcBinary), { recursive: true });
      console.log(
        `[bundle-cli] go build ./cmd/multica → ${srcBinary} (linux/${wslGoarch}, version=${version} commit=${commit})`,
      );
      execFileSync(
        "go",
        ["build", "-ldflags", ldflags, "-o", srcBinary, "./cmd/multica"],
        {
          cwd: serverDir,
          stdio: "inherit",
          env: {
            ...process.env,
            CGO_ENABLED: "0",
            GOOS: "linux",
            GOARCH: wslGoarch,
          },
        },
      );
    }
  }
} else {
  console.warn(
    "[bundle-cli] `go` not found in PATH — skipping build. " +
      "Desktop will use whatever is already in resources/bin/, or fall back " +
      "to auto-installing the latest release at runtime.",
  );
}

await rm(destDir, { recursive: true, force: true });
await mkdir(destDir, { recursive: true });

let bundledAny = false;
for (const { outputName } of BINARIES) {
  const binName = binaryNameForPlatform(outputName, targetPlatform);
  const srcBinary = join(serverDir, "bin", `${goos}-${goarch}`, binName);
  if (!(await exists(srcBinary))) {
    console.warn(`[bundle-cli] ${srcBinary} not present — skipping.`);
    continue;
  }
  const destBinary = join(destDir, binName);
  await copyFile(srcBinary, destBinary);
  await chmod(destBinary, 0o755);

  if (process.platform === "darwin") {
    try {
      execSync(`codesign -s - --force ${JSON.stringify(destBinary)}`, {
        stdio: "pipe",
      });
    } catch {
      // Non-fatal.
    }
  }

  console.log(`[bundle-cli] bundled ${srcBinary} → ${destBinary}`);
  bundledAny = true;
}

if (!bundledAny) {
  console.warn(
    "[bundle-cli] No binaries found — Desktop will fall back to " +
      "auto-installing at runtime.",
  );
  await rm(destDir, { recursive: true, force: true });
}

if (targetPlatform === "win32") {
  for (const { goarch: wslGoarch, dir } of WSL_CLI_TARGETS) {
    const srcBinary = join(serverDir, "bin", `linux-${wslGoarch}`, "multica");
    if (!(await exists(srcBinary))) {
      console.warn(`[bundle-cli] ${srcBinary} not present — skipping WSL CLI.`);
      continue;
    }
    const destBinary = join(destDir, "wsl", dir, "multica");
    await mkdir(dirname(destBinary), { recursive: true });
    await copyFile(srcBinary, destBinary);
    await chmod(destBinary, 0o755);
    console.log(`[bundle-cli] bundled WSL CLI ${srcBinary} → ${destBinary}`);
  }
}

// Bundle migration SQL files alongside the binaries so multica-migrate
// can find them at runtime (it searches relative to os.Executable()).
const migrationsSource = join(serverDir, "migrations");
const migrationsDest = join(destDir, "migrations");
if (await exists(migrationsSource)) {
  await rm(migrationsDest, { recursive: true, force: true });
  await cp(migrationsSource, migrationsDest, { recursive: true });
  console.log(`[bundle-cli] bundled migrations → ${migrationsDest}`);
}
