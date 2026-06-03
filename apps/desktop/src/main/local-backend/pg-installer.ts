import { existsSync } from "node:fs";
import { join } from "node:path";
import { app } from "electron";

function binName(name: string): string {
  return process.platform === "win32" ? `${name}.exe` : name;
}

export function bundledPgBinDir(): string {
  return join(
    app.getAppPath(),
    "resources",
    "pgsql",
    "bin",
  ).replace("app.asar", "app.asar.unpacked");
}

export function isBundledPgAvailable(): boolean {
  return existsSync(join(bundledPgBinDir(), binName("pg_ctl")));
}
