import { describe, expect, it } from "vitest";
import { createServer } from "node:net";
import { findFreePort, isPortAvailable } from "../port-utils";

describe("findFreePort", () => {
  it("returns a valid port number", async () => {
    const port = await findFreePort();
    expect(port).toBeGreaterThan(0);
    expect(port).toBeLessThanOrEqual(65535);
  });

  it("returns the preferred port when available", async () => {
    const preferred = await findFreePort();
    const port = await findFreePort(preferred);
    expect(port).toBe(preferred);
  });

  it("falls back to a random port when preferred is occupied", async () => {
    const occupied = await findFreePort();
    const server = createServer();
    await new Promise<void>((resolve) =>
      server.listen(occupied, "127.0.0.1", resolve),
    );
    try {
      const port = await findFreePort(occupied);
      expect(port).not.toBe(occupied);
      expect(port).toBeGreaterThan(0);
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });
});

describe("isPortAvailable", () => {
  it("returns true for an unused port", async () => {
    const port = await findFreePort();
    expect(await isPortAvailable(port)).toBe(true);
  });

  it("returns false for an occupied port", async () => {
    const port = await findFreePort();
    const server = createServer();
    await new Promise<void>((resolve) =>
      server.listen(port, "127.0.0.1", resolve),
    );
    try {
      expect(await isPortAvailable(port)).toBe(false);
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });
});
