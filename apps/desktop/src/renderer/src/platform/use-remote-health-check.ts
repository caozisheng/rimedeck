import { useEffect, useRef } from "react";

/**
 * Periodically pings the remote server while the app is connected to one.
 * After `MAX_CONSECUTIVE_FAILURES` network-level failures (fetch throws —
 * server genuinely unreachable, not HTTP 4xx/5xx which means it's alive),
 * automatically disconnects and reloads to restore the local workspace.
 *
 * Only active when `enabled` is true (remote mode + user logged in).
 */

const POLL_INTERVAL_MS = 5_000;
const MAX_CONSECUTIVE_FAILURES = 3;
const REQUEST_TIMEOUT_MS = 4_000;

export interface RemoteHealthCallbacks {
  /** Fires once when the first network failure is detected. */
  onConnectionLost: () => void;
  /** Fires when a response is received after a period of failures. */
  onConnectionRestored: () => void;
  /** Fires after MAX_CONSECUTIVE_FAILURES — auto-fallback to local. */
  onAutoDisconnect: () => void;
}

export function useRemoteHealthCheck(
  apiUrl: string | null,
  enabled: boolean,
  callbacks: RemoteHealthCallbacks,
): void {
  const failCountRef = useRef(0);
  const disconnectingRef = useRef(false);
  const lostFiredRef = useRef(false);

  useEffect(() => {
    if (!enabled || !apiUrl) return;
    failCountRef.current = 0;
    disconnectingRef.current = false;
    lostFiredRef.current = false;

    async function ping() {
      if (disconnectingRef.current) return;
      try {
        const ac = new AbortController();
        const timer = setTimeout(() => ac.abort(), REQUEST_TIMEOUT_MS);
        // Use /api/server-info — public endpoint, lightweight, no auth needed.
        await fetch(`${apiUrl}/api/server-info`, { signal: ac.signal });
        clearTimeout(timer);
        // Any response (even 4xx/5xx) means server is reachable — reset.
        if (lostFiredRef.current) {
          lostFiredRef.current = false;
          callbacks.onConnectionRestored();
        }
        failCountRef.current = 0;
      } catch {
        // Network error or timeout — server unreachable.
        failCountRef.current += 1;
        if (!lostFiredRef.current) {
          lostFiredRef.current = true;
          callbacks.onConnectionLost();
        }
        if (failCountRef.current >= MAX_CONSECUTIVE_FAILURES && !disconnectingRef.current) {
          disconnectingRef.current = true;
          console.warn(
            `[remote-health] ${MAX_CONSECUTIVE_FAILURES} consecutive failures reaching ${apiUrl}, falling back to local workspace`,
          );
          callbacks.onAutoDisconnect();
          return;
        }
      }
    }

    // First ping immediately so we catch an already-dead server fast.
    void ping();
    const id = setInterval(ping, POLL_INTERVAL_MS);

    return () => {
      clearInterval(id);
    };
  }, [apiUrl, enabled, callbacks]);
}
