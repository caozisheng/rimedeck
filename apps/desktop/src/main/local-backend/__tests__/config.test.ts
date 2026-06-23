import { describe, expect, it, beforeEach, afterEach } from "vitest";
import { mkdtemp, readFile, rm, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

let tempDir: string;
let rimedeckDir: string;

beforeEach(async () => {
  tempDir = await mkdtemp(join(tmpdir(), "rimedeck-test-"));
  rimedeckDir = join(tempDir, ".rimedeck");
  process.env.RIMEDECK_HOME = rimedeckDir;
});

afterEach(async () => {
  delete process.env.RIMEDECK_HOME;
  await rm(tempDir, { recursive: true, force: true });
});

describe("loadOrCreateConfig", () => {
  it("creates config on first run", async () => {
    const { loadOrCreateConfig } = await import("../config");
    const config = await loadOrCreateConfig();
    expect(config.pgPort).toBeGreaterThan(0);
    expect(config.backendPort).toBeGreaterThan(0);
    expect(config.pgPort).not.toBe(config.backendPort);
    expect(config.jwtSecret).toHaveLength(64);
    expect(config.firstRunAt).toBeTruthy();

    const raw = await readFile(join(rimedeckDir, "config.json"), "utf-8");
    const persisted = JSON.parse(raw);
    expect(persisted.pgPort).toBe(config.pgPort);
  });

  it("loads existing config", async () => {
    await mkdir(rimedeckDir, { recursive: true });
    const existing = {
      pgPort: 25432,
      backendPort: 28080,
      jwtSecret: "a".repeat(64),
      firstRunAt: "2025-01-01T00:00:00.000Z",
    };
    await writeFile(
      join(rimedeckDir, "config.json"),
      JSON.stringify(existing),
      "utf-8",
    );

    const { loadOrCreateConfig } = await import("../config");
    const config = await loadOrCreateConfig();
    expect(config.jwtSecret).toBe(existing.jwtSecret);
    expect(config.firstRunAt).toBe(existing.firstRunAt);
  });

  it("moves persisted local backend ports out of the reserved range", async () => {
    await mkdir(rimedeckDir, { recursive: true });
    await writeFile(
      join(rimedeckDir, "config.json"),
      JSON.stringify({
        pgPort: 1814,
        backendPort: 18080,
        jwtSecret: "a".repeat(64),
        firstRunAt: "2025-01-01T00:00:00.000Z",
      }),
      "utf-8",
    );

    const { loadOrCreateConfig } = await import("../config");
    const config = await loadOrCreateConfig();

    expect(config.pgPort).toBeGreaterThanOrEqual(1024 * 10);
    expect(config.backendPort).toBeGreaterThanOrEqual(1024 * 10);
    expect(config.pgPort).not.toBe(1814);
  });

  it("fills missing local backend fields without dropping existing config keys", async () => {
    await mkdir(rimedeckDir, { recursive: true });
    await writeFile(
      join(rimedeckDir, "config.json"),
      JSON.stringify({ workspace_id: "11111111-1111-1111-1111-111111111111" }),
      "utf-8",
    );

    const { loadOrCreateConfig } = await import("../config");
    const config = await loadOrCreateConfig();
    expect(config.pgPort).toBeGreaterThan(0);
    expect(config.backendPort).toBeGreaterThan(0);
    expect(config.pgPort).not.toBe(config.backendPort);
    expect(config.jwtSecret).toHaveLength(64);
    expect(config.firstRunAt).toBeTruthy();
    expect(config.workspace_id).toBe("11111111-1111-1111-1111-111111111111");

    const raw = await readFile(join(rimedeckDir, "config.json"), "utf-8");
    const persisted = JSON.parse(raw);
    expect(persisted.workspace_id).toBe("11111111-1111-1111-1111-111111111111");
    expect(persisted.pgPort).toBe(config.pgPort);
    expect(persisted.backendPort).toBe(config.backendPort);
  });
});
