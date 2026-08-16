import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Issue } from "@rimedeck/core/types";
import { IssueSourceBadge } from "./issue-source-badge";

const baseIssue = {
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
  created_at: "2026-08-15T00:00:00Z",
  updated_at: "2026-08-15T00:00:00Z",
  source_type: "local",
  sync_state: "local",
} satisfies Issue;

describe("IssueSourceBadge", () => {
  it("renders a neutral Local badge without sync state", () => {
    render(<IssueSourceBadge issue={baseIssue} />);
    expect(screen.getByText("Local")).toBeInTheDocument();
    expect(screen.queryByText("Pending sync")).not.toBeInTheDocument();
  });

  it("links a GitLab issue and renders pending sync state", () => {
    render(<IssueSourceBadge issue={{ ...baseIssue, source_type: "gitlab", sync_state: "pending", external: { provider: "gitlab", tracker_connection_id: "tracker-1", iid: 12, url: "https://gitlab.example/group/project/-/issues/12", author_name: null } }} />);
    expect(screen.getByRole("link", { name: "Open GitLab issue #12" })).toHaveAttribute("href", "https://gitlab.example/group/project/-/issues/12");
    expect(screen.getByText("#12")).toBeInTheDocument();
    expect(screen.getByText("Pending sync")).toBeInTheDocument();
  });

  it("shows failed and detached states", () => {
    const { rerender } = render(<IssueSourceBadge issue={{ ...baseIssue, source_type: "gitlab", sync_state: "failed" }} />);
    expect(screen.getByText("Sync failed")).toBeInTheDocument();
    rerender(<IssueSourceBadge issue={{ ...baseIssue, source_type: "detached", sync_state: "detached" }} />);
    expect(screen.getAllByText("Detached").length).toBeGreaterThan(0);
  });
});
