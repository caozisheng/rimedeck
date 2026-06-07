import { spawn, type ChildProcess } from "node:child_process";
import { createWriteStream, type WriteStream } from "node:fs";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { homedir } from "node:os";
import { resolveBinary } from "./binary-path";
import type { LocalConfig } from "./config";

const HEALTH_POLL_MS = 2_000;
const STARTUP_TIMEOUT_MS = 30_000;
const SHUTDOWN_WAIT_MS = 5_000;

let serverProcess: ChildProcess | null = null;
let logStream: WriteStream | null = null;
let healthTimer: ReturnType<typeof setInterval> | null = null;

export async function startBackend(
  config: LocalConfig,
  databaseUrl: string,
): Promise<{ apiUrl: string; wsUrl: string }> {
  const bin = await resolveBinary("multica-server");
  const port = config.backendPort;
  const logDir = join(homedir(), ".rimedeck");
  await mkdir(logDir, { recursive: true });
  const logPath = join(logDir, "backend.log");
  logStream = createWriteStream(logPath, { flags: "a" });

  const uploadDir = join(homedir(), ".rimedeck", "uploads");
  await mkdir(uploadDir, { recursive: true });

  const publicUrl = `http://127.0.0.1:${port}`;
  const env: NodeJS.ProcessEnv = {
    ...process.env,
    DATABASE_URL: databaseUrl,
    PORT: String(port),
    JWT_SECRET: config.jwtSecret,
    CORS_ALLOWED_ORIGINS: `*`,
    LOCAL_UPLOAD_DIR: uploadDir,
    MULTICA_DEV_VERIFICATION_CODE: "000000",
    APP_ENV: "local",
    ALLOW_SIGNUP: "true",
    MULTICA_PUBLIC_URL: publicUrl,
    MULTICA_APP_URL: publicUrl,
  };

  console.log(`[local-backend] Starting API server on port ${port}...`);
  serverProcess = spawn(bin, [], { env, stdio: ["ignore", "pipe", "pipe"] });

  serverProcess.stdout?.pipe(logStream);
  serverProcess.stderr?.pipe(logStream);

  serverProcess.on("exit", (code) => {
    console.log(`[local-backend] Server exited with code ${code}`);
    serverProcess = null;
  });

  await waitForHealth(port);

  const apiUrl = `http://127.0.0.1:${port}`;
  const wsUrl = `ws://127.0.0.1:${port}/ws`;
  console.log(`[local-backend] API server ready: ${apiUrl}`);
  return { apiUrl, wsUrl };
}

async function waitForHealth(port: number): Promise<void> {
  const deadline = Date.now() + STARTUP_TIMEOUT_MS;
  while (Date.now() < deadline) {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 2_000);
      const res = await fetch(`http://127.0.0.1:${port}/health`, {
        signal: controller.signal,
      });
      clearTimeout(timer);
      if (res.ok) return;
    } catch {
      // Not ready yet
    }
    await new Promise((r) => setTimeout(r, HEALTH_POLL_MS));
  }
  throw new Error(
    `[local-backend] API server failed to become healthy within ${STARTUP_TIMEOUT_MS / 1000}s`,
  );
}

export async function stopBackend(): Promise<void> {
  if (healthTimer) {
    clearInterval(healthTimer);
    healthTimer = null;
  }
  if (!serverProcess) return;
  console.log("[local-backend] Stopping API server...");

  const proc = serverProcess;
  serverProcess = null;

  const exited = new Promise<void>((resolve) => {
    proc.on("exit", () => resolve());
    setTimeout(() => {
      proc.kill("SIGKILL");
      resolve();
    }, SHUTDOWN_WAIT_MS);
  });

  if (process.platform === "win32") {
    proc.kill();
  } else {
    proc.kill("SIGTERM");
  }

  await exited;

  if (logStream) {
    logStream.end();
    logStream = null;
  }
}
