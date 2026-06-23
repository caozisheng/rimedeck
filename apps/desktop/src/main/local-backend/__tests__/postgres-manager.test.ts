import { describe, expect, it } from "vitest";
import { pgIsReadyArgs } from "../postgres-manager";

describe("postgres-manager", () => {
  it("checks readiness against the built-in postgres database", () => {
    expect(pgIsReadyArgs(1814)).toEqual([
      "-h",
      "127.0.0.1",
      "-p",
      "1814",
      "-d",
      "postgres",
      "-U",
      "postgres",
    ]);
  });
});
