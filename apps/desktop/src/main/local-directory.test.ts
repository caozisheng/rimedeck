import { afterEach, describe, expect, it, vi } from "vitest";
import { mkdtemp, rm } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { execFile } from "child_process";
import { promisify } from "util";
import { detectGitRemote } from "./local-directory";

const execFileAsync = promisify(execFile);
const dirs: string[] = [];

afterEach(async () => {
  vi.unstubAllEnvs();
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
  it("classifies gitlab.com and GitHub remotes", async () => {
    const gitlab = await repoWithRemote("git@gitlab.com:group/project.git");
    const github = await repoWithRemote("https://github.com/org/repo.git");
    await expect(detectGitRemote(gitlab)).resolves.toMatchObject({ host: "gitlab.com", provider: "gitlab" });
    await expect(detectGitRemote(github)).resolves.toMatchObject({ host: "github.com", provider: "github" });
  });

  it("classifies configured self-hosted GitLab and ignores missing origins", async () => {
    vi.stubEnv("GITLAB_ALLOWED_HOSTS", "gitlab.example.com");
    const selfHosted = await repoWithRemote("ssh://git@gitlab.example.com/group/project.git");
    const empty = await mkdtemp(join(tmpdir(), "rimedeck-git-empty-"));
    dirs.push(empty);
    await execFileAsync("git", ["init", empty]);
    await expect(detectGitRemote(selfHosted)).resolves.toMatchObject({ host: "gitlab.example.com", provider: "gitlab" });
    await expect(detectGitRemote(empty)).resolves.toBeUndefined();
  });
});
