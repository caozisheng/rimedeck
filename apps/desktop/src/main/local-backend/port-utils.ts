import { createServer, type AddressInfo } from "node:net";

export function findFreePort(preferred?: number): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.on("error", (err: NodeJS.ErrnoException) => {
      if (preferred && err.code === "EADDRINUSE") {
        findFreePort().then(resolve, reject);
        return;
      }
      reject(err);
    });
    server.listen(preferred ?? 0, "127.0.0.1", () => {
      const { port } = server.address() as AddressInfo;
      server.close(() => resolve(port));
    });
  });
}

export function isPortAvailable(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const server = createServer();
    server.on("error", () => resolve(false));
    server.listen(port, "127.0.0.1", () => {
      server.close(() => resolve(true));
    });
  });
}
