import { existsSync, realpathSync } from "node:fs";
import { join } from "node:path";
import { app } from "electron";

function binName(name: string): string {
  return process.platform === "win32" ? `${name}.exe` : name;
}

/**
 * Resolve the app path, working around macOS App Translocation.
 *
 * When Gatekeeper translocates an unsigned/un-quarantine-cleared app, it
 * mounts a read-only DMG under `/private/var/folders/.../AppTranslocation/`.
 * `app.getAppPath()` returns the translocated path, and `app.asar.unpacked`
 * resources (like the bundled PostgreSQL) may not be fully available there.
 *
 * `realpathSync` resolves the symlink/mount back to the original location
 * (e.g. `/Applications/RimeDeck.app/...`), which always has the full
 * unpacked tree. On non-macOS or non-translocated installs this is a no-op.
 */
function resolvedAppPath(): string {
  const raw = app.getAppPath();
  if (process.platform !== "darwin") return raw;
  try {
    return realpathSync(raw);
  } catch {
    return raw;
  }
}

export function bundledPgBinDir(): string {
  return join(
    resolvedAppPath(),
    "resources",
    "pgsql",
    "bin",
  ).replace("app.asar", "app.asar.unpacked");
}

export function isBundledPgAvailable(): boolean {
  return existsSync(join(bundledPgBinDir(), binName("pg_ctl")));
}
