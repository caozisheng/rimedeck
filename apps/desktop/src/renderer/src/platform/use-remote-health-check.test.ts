import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useRemoteHealthCheck, type RemoteHealthCallbacks } from "./use-remote-health-check";

function makeCallbacks(overrides?: Partial<RemoteHealthCallbacks>): RemoteHealthCallbacks {
  return {
    onConnectionLost: vi.fn(),
    onConnectionRestored: vi.fn(),
    onAutoDisconnect: vi.fn(),
    ...overrides,
  };
}

describe("useRemoteHealthCheck", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("does nothing when disabled", () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const cb = makeCallbacks();

    renderHook(() =>
      useRemoteHealthCheck("http://remote:18080", false, cb),
    );

    vi.advanceTimersByTime(30_000);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(cb.onConnectionLost).not.toHaveBeenCalled();
    expect(cb.onAutoDisconnect).not.toHaveBeenCalled();
  });

  it("does nothing when apiUrl is null", () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const cb = makeCallbacks();

    renderHook(() => useRemoteHealthCheck(null, true, cb));

    vi.advanceTimersByTime(30_000);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(cb.onConnectionLost).not.toHaveBeenCalled();
  });

  it("pings immediately on mount when enabled", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response("", { status: 200 }));
    const cb = makeCallbacks();

    renderHook(() =>
      useRemoteHealthCheck("http://remote:18080", true, cb),
    );

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy).toHaveBeenCalledWith(
      "http://remote:18080/api/server-info",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("fires onConnectionLost on the first failure", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(
      new TypeError("fetch failed"),
    );
    const cb = makeCallbacks();

    renderHook(() =>
      useRemoteHealthCheck("http://remote:18080", true, cb),
    );

    // Immediate ping → fail #1 → onConnectionLost fires
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(cb.onConnectionLost).toHaveBeenCalledTimes(1);
    expect(cb.onAutoDisconnect).not.toHaveBeenCalled();
  });

  it("fires onConnectionRestored when server comes back", async () => {
    let callCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(() => {
      callCount++;
      if (callCount <= 1) return Promise.reject(new TypeError("fetch failed"));
      return Promise.resolve(new Response("", { status: 200 }));
    });
    const cb = makeCallbacks();

    renderHook(() =>
      useRemoteHealthCheck("http://remote:18080", true, cb),
    );

    // Immediate ping → fail #1
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(cb.onConnectionLost).toHaveBeenCalledTimes(1);

    // +5s → success → onConnectionRestored
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    expect(cb.onConnectionRestored).toHaveBeenCalledTimes(1);
  });

  it("resets failure count on successful response", async () => {
    let callCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(() => {
      callCount++;
      // First two calls fail, then succeed.
      if (callCount <= 2) return Promise.reject(new TypeError("fetch failed"));
      return Promise.resolve(new Response("", { status: 200 }));
    });
    const cb = makeCallbacks();

    renderHook(() =>
      useRemoteHealthCheck("http://remote:18080", true, cb),
    );

    // Immediate ping (fail #1)
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    // 5s later (fail #2)
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    // 5s later (success — resets counter)
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    // 5s later (fail #1 again after reset)
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("fetch failed"));
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });

    expect(cb.onAutoDisconnect).not.toHaveBeenCalled();
  });

  it("calls onAutoDisconnect after 3 consecutive network failures", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(
      new TypeError("fetch failed"),
    );
    const cb = makeCallbacks();

    renderHook(() =>
      useRemoteHealthCheck("http://remote:18080", true, cb),
    );

    // Immediate ping → fail #1
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(cb.onAutoDisconnect).not.toHaveBeenCalled();

    // +5s → fail #2
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    expect(cb.onAutoDisconnect).not.toHaveBeenCalled();

    // +5s → fail #3 → auto-disconnect
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    expect(cb.onAutoDisconnect).toHaveBeenCalledTimes(1);
    expect(cb.onConnectionLost).toHaveBeenCalledTimes(1); // only once
  });

  it("does not disconnect on HTTP errors (server reachable)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("", { status: 500 }),
    );
    const cb = makeCallbacks();

    renderHook(() =>
      useRemoteHealthCheck("http://remote:18080", true, cb),
    );

    // Run well past the threshold.
    for (let i = 0; i < 10; i++) {
      await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    }
    expect(cb.onAutoDisconnect).not.toHaveBeenCalled();
    expect(cb.onConnectionLost).not.toHaveBeenCalled();
  });

  it("stops polling on unmount", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response("", { status: 200 }));
    const cb = makeCallbacks();

    const { unmount } = renderHook(() =>
      useRemoteHealthCheck("http://remote:18080", true, cb),
    );

    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(fetchSpy).toHaveBeenCalledTimes(1);

    unmount();
    fetchSpy.mockClear();

    await act(async () => { await vi.advanceTimersByTimeAsync(30_000); });
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
