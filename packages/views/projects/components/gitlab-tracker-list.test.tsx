import { afterEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

const trackerRow = {
  id: "tracker-1",
  instance_url: "https://gitlab.example.com",
  path_with_namespace: "group/project",
  web_url: "https://gitlab.example.com/group/project",
  state: "active" as const,
  webhook_state: "unavailable" as const,
  last_pull_at: "2026-08-16T00:00:00Z",
  pending_outbox_count: 0,
  failed_outbox_count: 2,
  token_configured: true,
  can_manage: true,
};

let listData: { data: unknown; isPending: boolean } = { data: [trackerRow], isPending: false };
const syncMock = vi.fn(() => Promise.resolve());
const retryMock = vi.fn(() => Promise.resolve({ reset_count: 2 }));
const disableMock = vi.fn(() => Promise.resolve());

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => listData,
}));

vi.mock("@rimedeck/core", () => ({
  projectGitlabTrackersOptions: () => ({ queryKey: ["trackers"] }),
  useSyncGitlabTracker: () => ({ mutateAsync: syncMock, isPending: false }),
  useRetryGitlabTracker: () => ({ mutateAsync: retryMock, isPending: false }),
  useRotateGitlabTrackerToken: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useDisableGitlabTracker: () => ({ mutateAsync: disableMock, isPending: false }),
  useDeleteGitlabTrackerMirrors: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@rimedeck/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(() => {
  vi.clearAllMocks();
  listData = { data: [trackerRow], isPending: false };
});

import { ProjectGitlabTrackerSection } from "./gitlab-tracker-list";

describe("ProjectGitlabTrackerSection", () => {
  it("shows tracker path and failed count when data present", () => {
    renderWithI18n(<ProjectGitlabTrackerSection projectId="p-1" />);
    expect(screen.getByText("group/project")).toBeInTheDocument();
    expect(screen.getByText(/2 failed/)).toBeInTheDocument();
  });

  it("renders nothing when the tracker list is empty", () => {
    listData = { data: [], isPending: false };
    const { container } = renderWithI18n(<ProjectGitlabTrackerSection projectId="p-1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("hides manage actions when can_manage is false", () => {
    listData = { data: [{ ...trackerRow, can_manage: false }], isPending: false };
    renderWithI18n(<ProjectGitlabTrackerSection projectId="p-1" />);
    expect(screen.queryByRole("menuitem")).not.toBeInTheDocument();
  });

  it("triggers sync mutation from the actions menu", async () => {
    renderWithI18n(<ProjectGitlabTrackerSection projectId="p-1" />);
    await userEvent.click(screen.getAllByRole("button").pop()!);
    await userEvent.click(await screen.findByText("Sync now"));
    expect(syncMock).toHaveBeenCalledWith("tracker-1");
  });
});
