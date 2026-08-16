import { ipcMain, dialog, BrowserWindow } from "electron";
import { access, stat } from "fs/promises";
import { constants as fsConstants } from "fs";
import { basename, isAbsolute } from "path";
import { execFile } from "child_process";
import { promisify } from "util";

const execFileAsync = promisify(execFile);

export interface GitRemoteInfo {
  url: string;
  host: string;
  provider: "gitlab" | "github" | "unknown";
}

export interface PickDirectoryResult {
  ok: boolean;
  path?: string;
  basename?: string;
  /** Set when ok=false. "cancelled" = user dismissed; otherwise an error blurb. */
  reason?: "cancelled" | "no_window" | "error";
  error?: string;
}

export interface ValidateLocalDirectoryResult {
  ok: boolean;
  /** When ok=false, identifies which check failed so the renderer can render a
   *  specific message without parsing free-form text. */
  reason?:
    | "not_absolute"
    | "not_found"
    | "not_a_directory"
    | "not_readable"
    | "not_writable"
    | "error";
  error?: string;
  gitRemote?: GitRemoteInfo;
}

async function validateLocalDirectory(
  path: string,
): Promise<ValidateLocalDirectoryResult> {
  if (!path || !isAbsolute(path)) {
    return { ok: false, reason: "not_absolute" };
  }
  try {
    const st = await stat(path);
    if (!st.isDirectory()) return { ok: false, reason: "not_a_directory" };
  } catch (err) {
    const code = (err as NodeJS.ErrnoException).code;
    if (code === "ENOENT") return { ok: false, reason: "not_found" };
    return { ok: false, reason: "error", error: errorMessage(err) };
  }
  try {
    await access(path, fsConstants.R_OK);
  } catch {
    return { ok: false, reason: "not_readable" };
  }
  try {
    await access(path, fsConstants.W_OK);
  } catch {
    return { ok: false, reason: "not_writable" };
  }
  const gitRemote = await detectGitRemote(path);
  return gitRemote ? { ok: true, gitRemote } : { ok: true };
}

export async function detectGitRemote(path: string): Promise<GitRemoteInfo | undefined> {
  try {
    const { stdout } = await execFileAsync("git", ["-C", path, "config", "--get", "remote.origin.url"], {
      timeout: 3000,
      maxBuffer: 16 * 1024,
      windowsHide: true,
    });
    const url = stdout.trim();
    let host = "";
    const scp = url.match(/^[^@\s]+@([^:\s]+):/);
    if (scp?.[1]) {
      host = scp[1].toLowerCase();
    } else {
      try { host = new URL(url).hostname.toLowerCase(); } catch { host = ""; }
    }
    if (!host) return { url, host: "", provider: "unknown" };
    // Pure UI hint. The Add-Resource popover confirms the actual
    // provider via the REST validation round-trip, so a false positive
    // here just seeds the wrong tab and the user can flip it. We
    // recognise obvious GitHub / GitLab hosts and fall back to
    // "unknown" for hostnames that give us no signal (a self-hosted
    // GitLab at `git.company.internal` will show up as unknown and
    // the user picks GitLab manually).
    let provider: "gitlab" | "github" | "unknown" = "unknown";
    if (host === "github.com" || host.endsWith(".github.com")) {
      provider = "github";
    } else if (
      host === "gitlab.com" ||
      host === "jihulab.com" ||
      host.includes("gitlab") ||
      host.includes("jihulab")
    ) {
      provider = "gitlab";
    }
    return { url, host, provider };
  } catch {
    return undefined;
  }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function setupLocalDirectory(
  windowGetter: () => BrowserWindow | null,
): void {
  ipcMain.handle(
    "local-directory:pick",
    async (_event, defaultPath?: string): Promise<PickDirectoryResult> => {
      const win = windowGetter();
      if (!win) return { ok: false, reason: "no_window" };
      try {
        const result = await dialog.showOpenDialog(win, {
          // Multiple-selection is intentionally disabled — a project_resource
          // points at a single directory, and the create flow expects one
          // path per click. Multi-add would have to be a separate UX.
          properties: ["openDirectory", "createDirectory"],
          ...(defaultPath ? { defaultPath } : {}),
        });
        if (result.canceled || result.filePaths.length === 0) {
          return { ok: false, reason: "cancelled" };
        }
        const picked = result.filePaths[0];
        if (!picked) return { ok: false, reason: "cancelled" };
        return { ok: true, path: picked, basename: basename(picked) };
      } catch (err) {
        return { ok: false, reason: "error", error: errorMessage(err) };
      }
    },
  );

  ipcMain.handle(
    "local-directory:validate",
    (_event, path: string): Promise<ValidateLocalDirectoryResult> =>
      validateLocalDirectory(path),
  );
}
