import { afterEach, describe, expect, it } from "vitest";
import { mkdtemp, rm } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { execFile } from "child_process";
import { promisify } from "util";
import { detectGitRemote } from "./local-directory";

const execFileAsync = promisify(execFile);
const dirs: string[] = [];

afterEach(async () => {
  await Promise.all(dirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })));
});

async function repoWithRemote(remote: string): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "rimedeck-git-remote-"));
  dirs.push(dir);
  await execFileAsync("git", ["init", dir]);
  await execFileAsync("git", ["-C", dir, "remote", "add", "origin", remote]);
  return dir;
}

describe("detectGitRemote", () => {
  it("classifies gitlab.com remotes", async () => {
    const gitlab = await repoWithRemote("git@gitlab.com:group/project.git");
    await expect(detectGitRemote(gitlab)).resolves.toMatchObject({ host: "gitlab.com", provider: "gitlab" });
  });

  it("classifies jihulab.com remotes", async () => {
    const jihulab = await repoWithRemote("https://jihulab.com/team/app.git");
    await expect(detectGitRemote(jihulab)).resolves.toMatchObject({ host: "jihulab.com", provider: "gitlab" });
  });

  it("classifies github.com remotes", async () => {
    const github = await repoWithRemote("https://github.com/org/repo.git");
    await expect(detectGitRemote(github)).resolves.toMatchObject({ host: "github.com", provider: "github" });
  });

  it("infers GitLab from a self-hosted hostname without operator config", async () => {
    const selfHosted = await repoWithRemote("ssh://git@gitlab.example.com/group/project.git");
    await expect(detectGitRemote(selfHosted)).resolves.toMatchObject({
      host: "gitlab.example.com",
      provider: "gitlab",
    });
  });

  it("returns unknown for hosts that give no signal", async () => {
    const custom = await repoWithRemote("https://git.company.internal/team/app.git");
    await expect(detectGitRemote(custom)).resolves.toMatchObject({
      host: "git.company.internal",
      provider: "unknown",
    });
  });

  it("returns undefined for repos without an origin remote", async () => {
    const empty = await mkdtemp(join(tmpdir(), "rimedeck-git-empty-"));
    dirs.push(empty);
    await execFileAsync("git", ["init", empty]);
    await expect(detectGitRemote(empty)).resolves.toBeUndefined();
  });
});
