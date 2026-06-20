/**
 * One-time migration of localStorage keys from the old `multica_*` prefix
 * to the new `rimedeck_*` prefix.
 *
 * Old keys are intentionally left in place for backward compatibility with
 * older client versions that may still be running.
 */

const MIGRATION_FLAG = "rimedeck_storage_migrated";

/** All bare key names that were renamed (without workspace suffix). */
const RENAMED_KEYS = [
  "token",
  "transcript_view",
  "agents_view",
  "navigation",
  "issue_draft",
  "comment_drafts",
  "comment_collapse",
  "recent_issues",
  "recent_contexts",
  "feedback_draft",
  "quick_create",
  "create_mode",
  "issues_scope",
  "issues_view",
  "my_issues_view",
  "actor_issues_view",
  "project_draft",
  "projects_view",
  "squads_view",
  "runtime_custom_pricing",
];

/**
 * Migrate all `multica_*` localStorage keys to `rimedeck_*`.
 *
 * - Idempotent: skips if the migration flag is already set.
 * - Copies (not moves) values: old keys are preserved for backward compat.
 * - Handles workspace-scoped keys (`multica_<name>:<workspace>` →
 *   `rimedeck_<name>:<workspace>`).
 */
export function migrateLocalStorageKeys(): void {
  if (typeof localStorage === "undefined") return;

  if (localStorage.getItem(MIGRATION_FLAG)) return;

  // Collect all current keys up-front to avoid issues with iterating a
  // mutating storage during setItem.
  const allKeys: string[] = [];
  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i);
    if (k !== null) allKeys.push(k);
  }

  for (const suffix of RENAMED_KEYS) {
    const oldBare = `multica_${suffix}`;
    const newBare = `rimedeck_${suffix}`;

    // Bare key (non-workspace-scoped stores).
    const bareVal = localStorage.getItem(oldBare);
    if (bareVal !== null && localStorage.getItem(newBare) === null) {
      localStorage.setItem(newBare, bareVal);
    }

    // Workspace-scoped keys: `multica_<name>:<workspace>`.
    const prefix = `${oldBare}:`;
    for (const key of allKeys) {
      if (key.startsWith(prefix)) {
        const workspace = key.slice(prefix.length);
        const newKey = `${newBare}:${workspace}`;
        if (localStorage.getItem(newKey) === null) {
          const val = localStorage.getItem(key);
          if (val !== null) {
            localStorage.setItem(newKey, val);
          }
        }
      }
    }
  }

  localStorage.setItem(MIGRATION_FLAG, "1");
}
