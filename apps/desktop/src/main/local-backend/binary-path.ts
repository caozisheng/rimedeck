import { app } from "electron";
import { access } from "node:fs/promises";
import { constants } from "node:fs";
import { join } from "node:path";

export function bundledBinaryPath(name: string): string {
  const binName = process.platform === "win32" ? `${name}.exe` : name;
  return join(app.getAppPath(), "resources", "bin", binName).replace(
    "app.asar",
    "app.asar.unpacked",
  );
}

export async function resolveBinary(name: string): Promise<string> {
  const bundled = bundledBinaryPath(name);
  try {
    await access(bundled, constants.X_OK);
    return bundled;
  } catch {
    // fall through
  }

  // Try PATH
  try {
    await access(bundled, constants.F_OK);
    return bundled;
  } catch {
    // fall through
  }

  throw new Error(
    `[local-backend] Binary '${name}' not found. ` +
      "Ensure the Go backend was built (run bundle-cli.mjs) or the binary is in PATH.",
  );
}
