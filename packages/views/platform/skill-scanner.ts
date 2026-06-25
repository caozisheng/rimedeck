// Desktop-only helpers for scanning folders / ZIP files for SKILL.md bundles.
//
// Mirrors the pattern in local-directory.ts: typed interface for the preload
// surface, a reader that returns undefined on web, and exported async wrappers
// that degrade gracefully.

// Re-export the IPC-level types so consumers don't need to reach into main/.
export interface ScannedSkillEntry {
  key: string;
  name: string;
  description: string;
  dirPath: string;
  fileCount: number;
}

export interface SkillBundleContent {
  content: string;
  name: string;
  description: string;
  files: { path: string; content: string }[];
}

export interface ScanResult {
  ok: boolean;
  source?: string;
  skills?: ScannedSkillEntry[];
  error?: string;
  reason?: "cancelled" | "no_window" | "not_found" | "error";
}

export interface ReadBundleResult {
  ok: boolean;
  bundle?: SkillBundleContent;
  error?: string;
}

// ---------------------------------------------------------------------------
// Preload surface contract
// ---------------------------------------------------------------------------

interface DesktopSkillScannerAPI {
  scanSkillFolder: (defaultPath?: string) => Promise<ScanResult>;
  scanSkillZip: () => Promise<ScanResult>;
  readSkillBundle: (source: string, key: string) => Promise<ReadBundleResult>;
}

function readSkillScannerAPI(): DesktopSkillScannerAPI | undefined {
  if (typeof window === "undefined") return undefined;
  if (!("desktopAPI" in window)) return undefined;
  const desktop: unknown = window.desktopAPI;
  if (
    !desktop ||
    typeof desktop !== "object" ||
    !("scanSkillFolder" in desktop) ||
    !("scanSkillZip" in desktop) ||
    !("readSkillBundle" in desktop)
  ) {
    return undefined;
  }
  // All three methods verified present via `in` narrowing above.
  // The final cast is safe: we've confirmed the shape at runtime.
  const checked: DesktopSkillScannerAPI = {
    scanSkillFolder: desktop.scanSkillFolder as DesktopSkillScannerAPI["scanSkillFolder"],
    scanSkillZip: desktop.scanSkillZip as DesktopSkillScannerAPI["scanSkillZip"],
    readSkillBundle: desktop.readSkillBundle as DesktopSkillScannerAPI["readSkillBundle"],
  };
  return checked;
}

/** Whether the desktop skill scanner IPC is available. */
export function hasSkillScanner(): boolean {
  return readSkillScannerAPI() !== undefined;
}

export async function scanSkillFolder(
  defaultPath?: string,
): Promise<ScanResult> {
  const api = readSkillScannerAPI();
  if (!api) return { ok: false, reason: "error", error: "Not available in this environment" };
  return api.scanSkillFolder(defaultPath);
}

export async function scanSkillZip(): Promise<ScanResult> {
  const api = readSkillScannerAPI();
  if (!api) return { ok: false, reason: "error", error: "Not available in this environment" };
  return api.scanSkillZip();
}

export async function readSkillBundle(
  source: string,
  key: string,
): Promise<ReadBundleResult> {
  const api = readSkillScannerAPI();
  if (!api) return { ok: false, error: "Not available in this environment" };
  return api.readSkillBundle(source, key);
}
