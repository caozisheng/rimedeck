import { loadOrCreateConfig } from "./config";
import { startPostgres, stopPostgres } from "./postgres-manager";
import { runMigrations } from "./migration-runner";
import { startBackend, stopBackend } from "./backend-manager";

let started = false;

export async function setupLocalBackend(): Promise<{
  apiUrl: string;
  wsUrl: string;
}> {
  console.log("[local-backend] Starting local backend stack...");

  const config = await loadOrCreateConfig();
  console.log(
    `[local-backend] Config: pgPort=${config.pgPort}, backendPort=${config.backendPort}`,
  );

  let databaseUrl: string;
  try {
    databaseUrl = await startPostgres(config.pgPort);
  } catch (err) {
    console.error("[local-backend] PostgreSQL startup failed:", err);
    throw err;
  }

  try {
    await runMigrations(databaseUrl);
  } catch (err) {
    console.error("[local-backend] Migrations failed:", err);
    await stopPostgres();
    throw err;
  }

  let result: { apiUrl: string; wsUrl: string };
  try {
    result = await startBackend(config, databaseUrl);
  } catch (err) {
    console.error("[local-backend] Backend startup failed:", err);
    await stopPostgres();
    throw err;
  }

  started = true;
  console.log("[local-backend] All services running.");
  return result;
}

export async function shutdownLocalBackend(): Promise<void> {
  if (!started) return;
  console.log("[local-backend] Shutting down local backend stack...");
  await stopBackend();
  await stopPostgres();
  started = false;
  console.log("[local-backend] All services stopped.");
}
