#!/usr/bin/env node
// Bump the version across every package.json in the monorepo and create a
// git tag.
//
// Usage:
//   node scripts/bump-version.mjs 0.3.0   # explicit version
//   node scripts/bump-version.mjs patch    # 0.2.0 → 0.2.1
//   node scripts/bump-version.mjs minor    # 0.2.0 → 0.3.0
//   node scripts/bump-version.mjs major    # 0.2.0 → 1.0.0

import { readFileSync, writeFileSync, existsSync, readdirSync } from "node:fs";
import { execSync } from "node:child_process";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function readJSON(path) {
  return JSON.parse(readFileSync(path, "utf-8"));
}

function writeJSON(path, data) {
  writeFileSync(path, JSON.stringify(data, null, 2) + "\n");
}

function bumpSemver(current, level) {
  const [major, minor, patch] = current.split(".").map(Number);
  if (level === "major") return `${major + 1}.0.0`;
  if (level === "minor") return `${major}.${minor + 1}.0`;
  if (level === "patch") return `${major}.${minor}.${patch + 1}`;
  throw new Error(`Unknown bump level: ${level}`);
}

function discoverPackageJsons() {
  const ws = readFileSync(resolve(root, "pnpm-workspace.yaml"), "utf-8");
  const patterns = [];
  for (const line of ws.split("\n")) {
    const m = line.match(/^\s*-\s*"(.+)"/);
    if (m) patterns.push(m[1]);
  }

  const paths = [resolve(root, "package.json")];

  for (const pattern of patterns) {
    const base = pattern.replace(/\/?\*$/, "");
    const baseDir = resolve(root, base);
    if (!existsSync(baseDir)) continue;
    for (const entry of readdirSync(baseDir, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      const pkg = resolve(baseDir, entry.name, "package.json");
      if (existsSync(pkg)) paths.push(pkg);
    }
  }

  const e2ePkg = resolve(root, "e2e", "package.json");
  if (existsSync(e2ePkg)) paths.push(e2ePkg);

  return paths;
}

const arg = process.argv[2];
if (!arg) {
  console.error("Usage: node scripts/bump-version.mjs <version|patch|minor|major>");
  process.exit(1);
}

const rootPkg = readJSON(resolve(root, "package.json"));
const currentVersion = rootPkg.version;

let newVersion;
if (["patch", "minor", "major"].includes(arg)) {
  newVersion = bumpSemver(currentVersion, arg);
} else {
  newVersion = arg.replace(/^v/, "");
  if (!/^\d+\.\d+\.\d+/.test(newVersion)) {
    console.error(`Invalid version: ${arg}`);
    process.exit(1);
  }
}

console.log(`Bumping ${currentVersion} → ${newVersion}\n`);

const packageJsons = discoverPackageJsons();
const updated = [];

for (const pkgPath of packageJsons) {
  const pkg = readJSON(pkgPath);
  if (pkg.version === newVersion) continue;
  pkg.version = newVersion;
  writeJSON(pkgPath, pkg);
  const rel = pkgPath.slice(root.length + 1).replace(/\\/g, "/");
  updated.push(rel);
  console.log(`  updated ${rel}`);
}

if (updated.length === 0) {
  console.log("All package.json files already at target version.");
}

const tag = `v${newVersion}`;
try {
  execSync(`git tag ${tag}`, { cwd: root, stdio: "inherit" });
  console.log(`\n  Created git tag: ${tag}`);
} catch {
  console.warn(`\n  Warning: tag ${tag} already exists or git tag failed.`);
}

console.log(`
Done! Next steps:
  git add -A && git commit -m "chore: bump version to ${newVersion}"
  git push && git push --tags
`);
