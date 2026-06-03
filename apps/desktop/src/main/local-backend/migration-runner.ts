import { execFile } from "node:child_process";
import { resolveBinary } from "./binary-path";

const MIGRATE_TIMEOUT_MS = 120_000;

export async function runMigrations(databaseUrl: string): Promise<void> {
  const bin = await resolveBinary("multica-migrate");
  console.log("[local-backend] Running database migrations...");

  return new Promise((resolve, reject) => {
    execFile(
      bin,
      ["up"],
      {
        timeout: MIGRATE_TIMEOUT_MS,
        env: { ...process.env, DATABASE_URL: databaseUrl },
      },
      (err, stdout, stderr) => {
        if (stdout) console.log("[local-backend] migrate:", stdout.trim());
        if (stderr) console.error("[local-backend] migrate:", stderr.trim());
        if (err) {
          reject(
            new Error(`Migration failed: ${err.message}\n${stderr || ""}`),
          );
        } else {
          console.log("[local-backend] Migrations complete.");
          resolve();
        }
      },
    );
  });
}
