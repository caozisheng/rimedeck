import React from "react";
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

const { deleteMutate } = vi.hoisted(() => ({ deleteMutate: vi.fn() }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey ?? [];
    if (key.includes("detail")) {
      return {
        data: {
          id: "project-1",
          title: "Project One",
          icon: "📁",
          description: null,
          status: "planned",
          priority: "none",
          lead_type: null,
          lead_id: null,
          issue_count: 0,
          done_count: 0,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
        isLoading: false,
      };
    }
    return { data: [], isLoading: false };
  },
}));

vi.mock("@rimedeck/core/projects/queries", () => ({
  projectDetailOptions: () => ({ queryKey: ["projects", "detail"] }),
}));
vi.mock("@rimedeck/core/projects/mutations", () => ({
  useUpdateProject: () => ({ mutate: vi.fn() }),
  useDeleteProject: () => ({ mutateAsync: deleteMutate, isPending: false }),
}));
vi.mock("@rimedeck/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@rimedeck/core/paths", () => ({
  useWorkspacePaths: () => ({ projects: () => "/workspace/projects" }),
}));
vi.mock("@rimedeck/core/auth", () => ({
  useAuthStore: (selector: (state: { user: null }) => unknown) => selector({ user: null }),
}));
vi.mock("@rimedeck/core/pins", () => ({
  pinListOptions: () => ({ queryKey: ["pins"] }),
  useCreatePin: () => ({ mutate: vi.fn() }),
  useDeletePin: () => ({ mutate: vi.fn() }),
}));
vi.mock("@rimedeck/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));
vi.mock("@rimedeck/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "" }),
}));
vi.mock("@rimedeck/core/agents", () => ({ agentTaskSnapshotOptions: () => ({ queryKey: ["tasks"] }) }));
vi.mock("@rimedeck/core/issues/queries", () => ({
  myIssueAssigneeGroupsOptions: () => ({ queryKey: ["issue-groups"] }),
  myIssueListOptions: () => ({ queryKey: ["issues"] }),
  projectGanttIssuesOptions: () => ({ queryKey: ["gantt"] }),
  childIssueProgressOptions: () => ({ queryKey: ["progress"] }),
}));
vi.mock("@rimedeck/core/issues/mutations", () => ({ useUpdateIssue: () => ({ mutate: vi.fn() }) }));
vi.mock("@rimedeck/core/modals", () => ({ useModalStore: { getState: () => ({}) } }));
vi.mock("@rimedeck/core/projects/config", () => ({
  PROJECT_STATUS_ORDER: ["planned"],
  PROJECT_STATUS_CONFIG: { planned: { dotColor: "" } },
  PROJECT_PRIORITY_ORDER: ["none"],
}));
vi.mock("@rimedeck/core/issues/config", () => ({ BOARD_STATUSES: [] }));
vi.mock("@rimedeck/core/issues/stores/view-store", () => ({ createIssueViewStore: () => ({}) }));
vi.mock("@rimedeck/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useViewStore: () => ({}),
}));
vi.mock("../labels", () => ({
  useProjectStatusLabels: () => ({ planned: "Planned" }),
  useProjectPriorityLabels: () => ({ none: "None" }),
  useFormatRelativeDate: () => () => "today",
}));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../editor", () => ({
  TitleEditor: () => null,
  ContentEditor: () => null,
}));
vi.mock("../../issues/components/priority-icon", () => ({ PriorityIcon: () => null }));
vi.mock("./project-resources-section", () => ({ ProjectResourcesSection: () => null }));
vi.mock("./gitlab-tracker-list", () => ({ ProjectGitlabTrackerSection: () => null }));
vi.mock("../../issues/components/issues-header", () => ({ IssuesHeader: () => null }));
vi.mock("../../issues/components/board-view", () => ({ BoardView: () => null }));
vi.mock("../../issues/components/list-view", () => ({ ListView: () => null }));
vi.mock("../../issues/components/gantt-view", () => ({ GanttView: () => null }));
vi.mock("../../issues/components/swimlane-view", () => ({ SwimLaneView: () => null }));
vi.mock("../../issues/components/analytics-view", () => ({ AnalyticsView: () => null }));
vi.mock("../../issues/components/calendar-view", () => ({ CalendarView: () => null }));
vi.mock("../../issues/components/dag/dag-view", () => ({ DagView: () => null }));
vi.mock("../../issues/components/batch-action-toolbar", () => ({ BatchActionToolbar: () => null }));
vi.mock("@rimedeck/ui/hooks/use-mobile", () => ({ useIsMobile: () => false }));
vi.mock("@rimedeck/ui/lib/clipboard", () => ({ copyText: vi.fn() }));
vi.mock("@rimedeck/ui/components/common/emoji-picker", () => ({ EmojiPicker: () => null }));
vi.mock("@rimedeck/ui/components/ui/skeleton", () => ({ Skeleton: () => null }));
vi.mock("@rimedeck/ui/components/ui/button", () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => <button {...props}>{children}</button>,
}));
vi.mock("@rimedeck/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  ResizablePanel: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  ResizableHandle: () => null,
}));
vi.mock("@rimedeck/ui/components/ui/sheet", () => ({ Sheet: () => null, SheetContent: () => null }));
vi.mock("@rimedeck/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactElement }) => render,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => <button onClick={onClick}>{children}</button>,
  DropdownMenuSeparator: () => null,
}));
vi.mock("@rimedeck/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactElement }) => render,
  PopoverContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@rimedeck/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactElement }) => render,
  TooltipContent: () => null,
}));
vi.mock("../../layout/breadcrumb-header", () => ({
  BreadcrumbHeader: ({ actions }: { actions: React.ReactNode }) => <>{actions}</>,
}));
vi.mock("@rimedeck/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ children, open }: { children: React.ReactNode; open: boolean }) => open ? <div>{children}</div> : null,
  AlertDialogAction: ({ children, onClick, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => <button onClick={onClick} {...props}>{children}</button>,
  AlertDialogCancel: ({ children, onClick, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => <button onClick={onClick} {...props}>{children}</button>,
  AlertDialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { ProjectDetail } from "./project-detail";

describe("ProjectDetail deletion", () => {
  it("closes the confirmation dialog and submits deletion once", async () => {
    renderWithI18n(<ProjectDetail projectId="project-1" />);

    await userEvent.click(screen.getByRole("button", { name: "Delete project" }));
    expect(screen.getByText("This will delete the project. Issues will not be deleted but will be unlinked.")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Delete" }));

    expect(deleteMutate).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("This will delete the project. Issues will not be deleted but will be unlinked.")).not.toBeInTheDocument();
  });
});
