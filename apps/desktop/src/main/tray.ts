import { app, BrowserWindow, Menu, nativeImage, Tray } from "electron";

// ---- Main-process locale strings ----------------------------------------
// The renderer's i18next bundle is unavailable in the main process, so we
// keep a minimal map for the tray menu and close dialog — the only native
// UI that the main process owns. Keyed by SupportedLocale values.

interface TrayStrings {
  showApp: string;
  quit: string;
  closeDialogTitle: string;
  closeDialogMessage: string;
  closeDialogMinimize: string;
  closeDialogQuit: string;
}

const TRAY_STRINGS: Record<string, TrayStrings> = {
  en: {
    showApp: "Show RimeDeck",
    quit: "Quit",
    closeDialogTitle: "RimeDeck",
    closeDialogMessage:
      "Do you want to minimize RimeDeck to the system tray, or quit?",
    closeDialogMinimize: "Minimize to Tray",
    closeDialogQuit: "Quit",
  },
  "zh-Hans": {
    showApp: "显示 RimeDeck",
    quit: "退出",
    closeDialogTitle: "RimeDeck",
    closeDialogMessage: "你想将 RimeDeck 最小化到系统托盘，还是退出？",
    closeDialogMinimize: "最小化到托盘",
    closeDialogQuit: "退出",
  },
  ko: {
    showApp: "RimeDeck 보기",
    quit: "종료",
    closeDialogTitle: "RimeDeck",
    closeDialogMessage:
      "RimeDeck를 시스템 트레이로 최소화하시겠습니까, 아니면 종료하시겠습니까?",
    closeDialogMinimize: "트레이로 최소화",
    closeDialogQuit: "종료",
  },
  ja: {
    showApp: "RimeDeck を表示",
    quit: "終了",
    closeDialogTitle: "RimeDeck",
    closeDialogMessage:
      "RimeDeck をシステムトレイに最小化しますか、それとも終了しますか？",
    closeDialogMinimize: "トレイに最小化",
    closeDialogQuit: "終了",
  },
};

/**
 * Resolve a BCP 47 locale string (e.g. "zh-CN", "en-US", "ja") to the
 * closest SupportedLocale key in TRAY_STRINGS. Falls back to "en".
 */
function resolveLocale(raw: string): string {
  const lower = raw.toLowerCase();
  // Exact match (e.g. "en", "ko", "ja")
  if (TRAY_STRINGS[raw]) return raw;
  // zh-Hans / zh-CN / zh-SG → zh-Hans; zh-TW / zh-Hant → en (no zh-Hant)
  if (lower.startsWith("zh")) {
    if (lower.includes("hant") || lower.includes("tw") || lower.includes("hk")) {
      return "en";
    }
    return "zh-Hans";
  }
  // Strip region: "en-US" → "en", "ja-JP" → "ja", "ko-KR" → "ko"
  const base = lower.split("-")[0]!;
  if (TRAY_STRINGS[base]) return base;
  return "en";
}

export function getTrayStrings(systemLocale: string): TrayStrings {
  return TRAY_STRINGS[resolveLocale(systemLocale)] ?? TRAY_STRINGS["en"]!;
}

// ---- Tray ---------------------------------------------------------------

let tray: Tray | null = null;

/**
 * Create the system tray icon with a context menu.
 * Call once from app.whenReady(). Safe to call multiple times — subsequent
 * calls are no-ops.
 */
export function createTray(
  iconPath: string,
  getMainWindow: () => BrowserWindow | null,
  systemLocale: string,
): Tray {
  if (tray) return tray;

  const strings = getTrayStrings(systemLocale);

  const icon = nativeImage.createFromPath(iconPath);
  // Resize so it fits well in the Windows system tray (16×16 recommended).
  const trayIcon = icon.resize({ width: 16, height: 16 });

  tray = new Tray(trayIcon);
  tray.setToolTip("RimeDeck");

  const contextMenu = Menu.buildFromTemplate([
    {
      label: strings.showApp,
      click: () => showWindow(getMainWindow()),
    },
    { type: "separator" },
    {
      label: strings.quit,
      click: () => {
        // Mark as intentional quit so the close handler doesn't intercept.
        setForceQuit(true);
        app.quit();
      },
    },
  ]);

  tray.setContextMenu(contextMenu);

  // Double-click on the tray icon restores the window (Windows convention).
  tray.on("double-click", () => showWindow(getMainWindow()));

  return tray;
}

function showWindow(window: BrowserWindow | null): void {
  if (!window) return;
  if (window.isMinimized()) window.restore();
  window.show();
  window.focus();
}

/**
 * Destroy the tray icon. Called during app shutdown so the ghost icon
 * doesn't linger on Windows.
 */
export function destroyTray(): void {
  if (tray) {
    tray.destroy();
    tray = null;
  }
}

// ---- Force-quit flag ----------------------------------------------------
// When true, the close handler should NOT intercept — let the window close
// and the app quit normally.

let forceQuit = false;

export function isForceQuit(): boolean {
  return forceQuit;
}

export function setForceQuit(value: boolean): void {
  forceQuit = value;
}
