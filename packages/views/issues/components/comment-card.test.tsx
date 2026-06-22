import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentTask, TaskMessagePayload, TimelineEntry } from "@rimedeck/core/types";
import { renderWithI18n } from "../../test/i18n";

const { getAttachmentTextContentMock, taskMessagesOptionsMock, taskMessagesById } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
  taskMessagesOptionsMock: vi.fn(),
  taskMessagesById: new Map<string, TaskMessagePayload[]>(),
}));

vi.mock("@rimedeck/core/api", () => ({
  api: {
    getAttachmentTextContent: getAttachmentTextContentMock,
    getAttachment: vi.fn(),
  },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("@rimedeck/core/chat/queries", () => ({
  taskMessagesOptions: taskMessagesOptionsMock,
}));

vi.mock("@rimedeck/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) =>
      type === "agent" && id === "agent-1" ? "Embedded Engineer" : id,
  }),
}));

vi.mock("@rimedeck/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

// HtmlAttachmentPreview (kind="html" dispatch from AttachmentBlock) reads
// useNavigation() + useWorkspaceSlug() for the Open-in-new-tab button.
// Mock both so the standalone-attachment-routes-to-iframe test does not
// need the surrounding NavigationProvider / WorkspaceSlugProvider tree.
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    openInNewTab: vi.fn(),
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("@rimedeck/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@rimedeck/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
  };
});

import { AttachmentList, CommentCard } from "./comment-card";

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function renderCardWithProviders(ui: ReactElement, taskMessages: TaskMessagePayload[] = []) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  for (const message of taskMessages) {
    const existing = taskMessagesById.get(message.task_id) ?? [];
    taskMessagesById.set(message.task_id, [...existing, message]);
  }
  for (const [taskId, messages] of taskMessagesById) {
    qc.setQueryData(["task-messages", taskId], messages);
  }
  return renderWithI18n(
    <QueryClientProvider client={qc}>{ui}</QueryClientProvider>,
  );
}

function comment(id: string, parentId: string | null, content: string): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "member-1",
    parent_id: parentId,
    content,
    created_at: "2026-06-22T00:00:00Z",
    updated_at: "2026-06-22T00:00:00Z",
    comment_type: "comment",
  };
}

function task(): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-22T00:00:01Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-22T00:00:01Z",
    trigger_comment_id: "root-1",
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  taskMessagesById.clear();
  taskMessagesOptionsMock.mockImplementation((taskId: string) => ({
    queryKey: ["task-messages", taskId],
    queryFn: () => Promise.resolve(taskMessagesById.get(taskId) ?? []),
    enabled: true,
    staleTime: Infinity,
  }));
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("AttachmentList —standalone HTML attachment routes through AttachmentBlock", () => {
  // Regression pin for comment-card.tsx:152. This is the entry point
  // MUL-2330 originally regressed on: standalone HTML attachments (not
  // referenced inline in the markdown body) MUST render through
  // <AttachmentBlock> so the html+attachmentId dispatch fires. Reverting to
  // <AttachmentCard> here re-introduces the "report.html shows as a bare
  // file card row instead of the rendered chart" bug.
  it("renders an iframe (no file-card chrome) for a standalone HTML attachment", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart</p>",
      originalContentType: "text/html",
    });
    const attachment = {
      id: "att-1",
      url: "/uploads/report.html",
      filename: "report.html",
      content_type: "text/html",
      size_bytes: 0,
    } as any;

    renderWithQuery(<AttachmentList attachments={[attachment]} content="" />);

    const frame = await waitFor(() => {
      const f = document.querySelector("iframe") as HTMLIFrameElement | null;
      expect(f).toBeTruthy();
      return f!;
    });
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("srcdoc")).toContain("<p>chart</p>");
    // AttachmentCard chrome would render the filename as visible <p> text;
    // HtmlAttachmentPreview replaces the row entirely.
    expect(screen.queryByText("report.html")).toBeNull();
  });
});

describe("CommentCard pending agent replies", () => {
  it("renders a running comment-triggered task as an in-thread agent reply placeholder", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-06-22T00:00:08Z"));

    renderCardWithProviders(
      <CommentCard
        issueId="issue-1"
        entry={comment("root-1", null, "Please check the embedded camera bug")}
        replies={[]}
        currentUserId="member-1"
        onReply={vi.fn()}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
        pendingAgentReplies={[
          {
            task: task(),
            triggerCommentId: "root-1",
            rootCommentId: "root-1",
          },
        ]}
      />,
      [
        {
          task_id: "task-1",
          issue_id: "issue-1",
          seq: 1,
          type: "tool_use",
          tool: "Bash",
          input: { command: "pio test" },
        },
      ],
    );

    expect(screen.getByText("Please check the embedded camera bug")).toBeInTheDocument();
    expect(screen.getByTestId("pending-agent-reply")).toBeInTheDocument();
    expect(screen.getByText("Embedded Engineer")).toBeInTheDocument();
    expect(screen.getAllByText("Working").length).toBeGreaterThan(0);
    expect(screen.getByText("Bash")).toBeInTheDocument();
    expect(screen.getByText("pio test")).toBeInTheDocument();
  });
});
