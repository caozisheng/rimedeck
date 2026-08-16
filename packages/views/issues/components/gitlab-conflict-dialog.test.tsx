import { describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import type { Issue } from "@rimedeck/core/types";

const retryMock = vi.fn(() => Promise.resolve({ reset_count: 1 }));
const detachMock = vi.fn(() => Promise.resolve({}));
const discardMock = vi.fn(() => Promise.resolve({}));

vi.mock("@rimedeck/core/issues/mutations", () => ({
  useDetachIssueTracker: () => ({ mutateAsync: detachMock }),
  useDiscardIssuePending: () => ({ mutateAsync: discardMock }),
}));

vi.mock("@rimedeck/core", () => ({
  useRetryGitlabTracker: () => ({ mutateAsync: retryMock, isPending: false }),
}));

vi.mock("@rimedeck/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { GitlabConflictDialog } from "./gitlab-conflict-dialog";

const failedIssue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "MUL-1",
  title: "Issue",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: "project-1",
  position: 1,
  start_date: null,
  due_date: null,
  metadata: {},
  created_at: "2026-08-16T00:00:00Z",
  updated_at: "2026-08-16T00:00:00Z",
  source_type: "gitlab",
  sync_state: "failed",
  tracker_connection_id: "tracker-1",
};

describe("GitlabConflictDialog", () => {
  it("fires the retry mutation with the tracker id", async () => {
    renderWithI18n(<GitlabConflictDialog issue={failedIssue} projectId="project-1" open onOpenChange={() => {}} />);
    await userEvent.click(await screen.findByText("Retry"));
    await waitFor(() => expect(retryMock).toHaveBeenCalledWith("tracker-1"));
  });

  it("fires the detach mutation with the issue id", async () => {
    renderWithI18n(<GitlabConflictDialog issue={failedIssue} projectId="project-1" open onOpenChange={() => {}} />);
    await userEvent.click(await screen.findByText("Convert to local issue"));
    await waitFor(() => expect(detachMock).toHaveBeenCalledWith("issue-1"));
  });

  it("fires the discard mutation with the issue id", async () => {
    renderWithI18n(<GitlabConflictDialog issue={failedIssue} projectId="project-1" open onOpenChange={() => {}} />);
    await userEvent.click(await screen.findByText("Discard local changes"));
    await waitFor(() => expect(discardMock).toHaveBeenCalledWith("issue-1"));
  });
});
