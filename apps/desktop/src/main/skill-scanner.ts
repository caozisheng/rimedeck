import { ipcMain, dialog, BrowserWindow } from "electron";
import { readdir, readFile, stat } from "fs/promises";
import { join, basename, extname } from "path";
import AdmZip from "adm-zip";

// ---------------------------------------------------------------------------
// Types shared with the renderer (via preload)
// ---------------------------------------------------------------------------

export interface ScannedSkillEntry {
  /** Unique key — folder path or "zip:entryDir" */
  key: string;
  /** Skill name extracted from SKILL.md frontmatter, or directory basename */
  name: string;
  /** Description from frontmatter */
  description: string;
  /** Relative directory path inside the source */
  dirPath: string;
  /** Number of supporting files (excluding SKILL.md itself) */
  fileCount: number;
}

export interface SkillBundleContent {
  /** SKILL.md body */
  content: string;
  /** Name from frontmatter */
  name: string;
  /** Description from frontmatter */
  description: string;
  /** Supporting files */
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
// Frontmatter parser (mirrors server/internal/skill/frontmatter.go)
// ---------------------------------------------------------------------------

const FRONTMATTER_RE = /^---\r?\n([\s\S]*?\r?\n)---\r?\n?/;

function parseFrontmatter(content: string): { name: string; description: string } {
  const match = FRONTMATTER_RE.exec(content);
  if (!match) return { name: "", description: "" };

  // Simple YAML key: value extraction — avoids adding a YAML dep to main.
  // Only extracts top-level scalar `name:` and `description:`.
  const yaml = match[1]!;
  let name = "";
  let description = "";
  for (const line of yaml.split(/\r?\n/)) {
    const m = /^(name|description)\s*:\s*(.*)$/.exec(line);
    if (!m) continue;
    let val = m[2]!.trim();
    // Strip surrounding quotes
    if (
      (val.startsWith('"') && val.endsWith('"')) ||
      (val.startsWith("'") && val.endsWith("'"))
    ) {
      val = val.slice(1, -1);
    }
    if (m[1] === "name") name = val;
    else description = val;
  }
  return { name, description };
}

// ---------------------------------------------------------------------------
// Binary-file filter (mirrors server/internal/handler/skill.go)
// ---------------------------------------------------------------------------

const BINARY_EXTENSIONS = new Set([
  ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp", ".avif",
  ".svg", ".mp4", ".mov", ".avi", ".mkv", ".mp3", ".wav", ".ogg",
  ".flac", ".pdf", ".zip", ".tar", ".gz", ".tgz", ".rar", ".7z",
  ".exe", ".dll", ".so", ".dylib", ".woff", ".woff2", ".ttf", ".otf",
  ".eot", ".class", ".pyc", ".o", ".a", ".wasm",
]);

function isLikelyBinary(filePath: string): boolean {
  return BINARY_EXTENSIONS.has(extname(filePath).toLowerCase());
}

// Max sizes per bundle (mirroring server constants)
const MAX_IMPORT_FILE_SIZE = 512 * 1024; // 512 KB per file
const MAX_IMPORT_BUNDLE_FILES = 50;
const MAX_IMPORT_BUNDLE_BYTES = 2 * 1024 * 1024; // 2 MB total

// ---------------------------------------------------------------------------
// Folder scanning
// ---------------------------------------------------------------------------

async function scanFolder(dirPath: string): Promise<ScannedSkillEntry[]> {
  const entries = await readdir(dirPath, { withFileTypes: true });
  const results: ScannedSkillEntry[] = [];

  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const subDir = join(dirPath, entry.name);
    const skillMdPath = join(subDir, "SKILL.md");
    try {
      const st = await stat(skillMdPath);
      if (!st.isFile()) continue;
    } catch {
      continue;
    }

    try {
      const content = await readFile(skillMdPath, "utf-8");
      const fm = parseFrontmatter(content);
      // Count supporting files (1 level deep, non-binary)
      const subEntries = await readdir(subDir, { withFileTypes: true });
      const fileCount = subEntries.filter(
        (e) => e.isFile() && e.name !== "SKILL.md" && !isLikelyBinary(e.name),
      ).length;

      results.push({
        key: subDir,
        name: fm.name || entry.name,
        description: fm.description || "",
        dirPath: entry.name,
        fileCount,
      });
    } catch {
      // Skip unreadable skills
    }
  }
  return results;
}

async function readFolderBundle(dirPath: string): Promise<SkillBundleContent> {
  const skillMdPath = join(dirPath, "SKILL.md");
  const content = await readFile(skillMdPath, "utf-8");
  const fm = parseFrontmatter(content);

  const entries = await readdir(dirPath, { withFileTypes: true });
  const files: { path: string; content: string }[] = [];
  let totalBytes = 0;

  for (const entry of entries) {
    if (!entry.isFile() || entry.name === "SKILL.md") continue;
    if (isLikelyBinary(entry.name)) continue;
    if (files.length >= MAX_IMPORT_BUNDLE_FILES) break;

    const filePath = join(dirPath, entry.name);
    try {
      const st = await stat(filePath);
      if (st.size > MAX_IMPORT_FILE_SIZE) continue;
      const fileContent = await readFile(filePath, "utf-8");
      totalBytes += fileContent.length;
      if (totalBytes > MAX_IMPORT_BUNDLE_BYTES) break;
      files.push({ path: entry.name, content: fileContent });
    } catch {
      // Skip unreadable files
    }
  }

  return {
    content,
    name: fm.name || basename(dirPath),
    description: fm.description || "",
    files,
  };
}

// ---------------------------------------------------------------------------
// ZIP scanning (using adm-zip — pure JS, no native deps)
// ---------------------------------------------------------------------------

function scanZip(filePath: string): ScannedSkillEntry[] {
  const zip = new AdmZip(filePath);
  const zipEntries = zip.getEntries();

  // Find all directories that contain a SKILL.md at their root
  const skillDirs = new Map<string, { content: string; fileCount: number }>();

  for (const entry of zipEntries) {
    if (entry.isDirectory) continue;
    const fileName = entry.entryName.replace(/\\/g, "/");
    const parts = fileName.split("/");
    const name = parts[parts.length - 1]!;
    const dir = parts.slice(0, -1).join("/");

    if (name === "SKILL.md") {
      const existing = skillDirs.get(dir);
      const content = entry.getData().toString("utf-8");
      if (existing) {
        existing.content = content;
      } else {
        skillDirs.set(dir, { content, fileCount: 0 });
      }
    }
  }

  // Count supporting files per skill directory
  for (const entry of zipEntries) {
    if (entry.isDirectory) continue;
    const fileName = entry.entryName.replace(/\\/g, "/");
    const parts = fileName.split("/");
    const name = parts[parts.length - 1]!;
    const dir = parts.slice(0, -1).join("/");

    if (name !== "SKILL.md" && !isLikelyBinary(name) && skillDirs.has(dir)) {
      // Only count direct children, not nested subdirectories
      const dirInfo = skillDirs.get(dir)!;
      const relPath = fileName.slice(dir ? dir.length + 1 : 0);
      if (!relPath.includes("/")) {
        dirInfo.fileCount++;
      }
    }
  }

  const results: ScannedSkillEntry[] = [];
  for (const [dir, info] of skillDirs) {
    const fm = parseFrontmatter(info.content);
    const dirBasename = dir.split("/").filter(Boolean).pop() || dir || "root";
    results.push({
      key: `zip:${dir}`,
      name: fm.name || dirBasename,
      description: fm.description || "",
      dirPath: dir,
      fileCount: info.fileCount,
    });
  }
  return results;
}

function readZipBundle(zipPath: string, dirPath: string): SkillBundleContent {
  const zip = new AdmZip(zipPath);
  const zipEntries = zip.getEntries();
  const prefix = dirPath ? dirPath + "/" : "";

  let content = "";
  let fm = { name: "", description: "" };
  const files: { path: string; content: string }[] = [];
  let totalBytes = 0;

  for (const entry of zipEntries) {
    if (entry.isDirectory) continue;
    const fileName = entry.entryName.replace(/\\/g, "/");
    if (fileName === prefix + "SKILL.md") {
      content = entry.getData().toString("utf-8");
      fm = parseFrontmatter(content);
      break;
    }
  }

  for (const entry of zipEntries) {
    if (entry.isDirectory) continue;
    const fileName = entry.entryName.replace(/\\/g, "/");
    if (!fileName.startsWith(prefix)) continue;
    const relPath = fileName.slice(prefix.length);
    if (!relPath || relPath === "SKILL.md" || relPath.includes("/")) continue;
    if (isLikelyBinary(relPath)) continue;
    if (files.length >= MAX_IMPORT_BUNDLE_FILES) break;
    if (entry.header.size > MAX_IMPORT_FILE_SIZE) continue;

    try {
      const fileContent = entry.getData().toString("utf-8");
      totalBytes += fileContent.length;
      if (totalBytes > MAX_IMPORT_BUNDLE_BYTES) break;
      files.push({ path: relPath, content: fileContent });
    } catch {
      // Skip unreadable
    }
  }

  const dirBasename = dirPath.split("/").filter(Boolean).pop() || dirPath || "root";
  return {
    content,
    name: fm.name || dirBasename,
    description: fm.description || "",
    files,
  };
}

// ---------------------------------------------------------------------------
// IPC registration
// ---------------------------------------------------------------------------

export function setupSkillScanner(windowGetter: () => BrowserWindow | null): void {
  ipcMain.handle(
    "skill-scan:pick-folder",
    async (_event, defaultPath?: string): Promise<ScanResult> => {
      const win = windowGetter();
      if (!win) return { ok: false, reason: "no_window" };

      try {
        const result = await dialog.showOpenDialog(win, {
          properties: ["openDirectory"],
          ...(defaultPath ? { defaultPath } : {}),
        });
        if (result.canceled || result.filePaths.length === 0) {
          return { ok: false, reason: "cancelled" };
        }
        const picked = result.filePaths[0]!;
        const skills = await scanFolder(picked);
        return { ok: true, source: picked, skills };
      } catch (err) {
        return {
          ok: false,
          reason: "error",
          error: err instanceof Error ? err.message : String(err),
        };
      }
    },
  );

  ipcMain.handle("skill-scan:pick-zip", async (_event): Promise<ScanResult> => {
    const win = windowGetter();
    if (!win) return { ok: false, reason: "no_window" };

    try {
      const result = await dialog.showOpenDialog(win, {
        properties: ["openFile"],
        filters: [{ name: "ZIP Archives", extensions: ["zip"] }],
      });
      if (result.canceled || result.filePaths.length === 0) {
        return { ok: false, reason: "cancelled" };
      }
      const picked = result.filePaths[0]!;
      const skills = scanZip(picked);
      return { ok: true, source: picked, skills };
    } catch (err) {
      return {
        ok: false,
        reason: "error",
        error: err instanceof Error ? err.message : String(err),
      };
    }
  });

  ipcMain.handle(
    "skill-scan:read-bundle",
    async (_event, source: string, key: string): Promise<ReadBundleResult> => {
      try {
        if (key.startsWith("zip:")) {
          // source = zip file path, key = "zip:<dirPath>"
          const dirPath = key.slice(4);
          const bundle = readZipBundle(source, dirPath);
          return { ok: true, bundle };
        }
        // key = folder path
        const bundle = await readFolderBundle(key);
        return { ok: true, bundle };
      } catch (err) {
        return {
          ok: false,
          error: err instanceof Error ? err.message : String(err),
        };
      }
    },
  );
}
