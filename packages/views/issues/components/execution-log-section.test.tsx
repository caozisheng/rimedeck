// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@rimedeck/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  taskMessagesOptions: vi.fn(),
  messagesByTaskId: new Map<string, any[]>(),
}));

vi.mock("@rimedeck/core/chat/queries", () => ({
  taskMessagesOptions: mockState.taskMessagesOptions,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: ({ title }: { title?: string }) => (
    <button type="button">{title ?? "Transcript"}</button>
  ),
  TaskTimelinePreview: ({ taskId }: { taskId: string }) => {
    const messages = mockState.messagesByTaskId.get(taskId) ?? [];
    if (messages.length === 0) return null;
    return (
      <div data-testid="task-timeline-preview">
        {messages.map((message: any) => (
          <div key={message.seq}>
            <span>{message.tool ?? message.type}</span>
            <span>{message.content ?? message.input?.file_path ?? ""}</span>
          </div>
        ))}
      </div>
    );
  },
}));

vi.mock("./terminate-task-confirm-dialog", () => ({
  TerminateTaskConfirmDialog: () => null,
}));

import { ActiveTaskRow } from "./execution-log-section";
import type { TaskMessagePayload } from "@rimedeck/core/types";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    trigger_summary: "Started from comment",
    ...overrides,
  };
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockState.messagesByTaskId.clear();
  mockState.taskMessagesOptions.mockImplementation((taskId: string) => ({
    queryKey: ["task-messages", taskId],
    queryFn: () => Promise.resolve([]),
    enabled: true,
    staleTime: Infinity,
  }));
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-06-08T08:05:04Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ActiveTaskRow", () => {
  function renderRow(task = makeTask(), messages: TaskMessagePayload[] = []) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(["task-messages", task.id], messages);
    mockState.messagesByTaskId.set(task.id, messages);
    return renderWithI18n(
      <QueryClientProvider client={qc}>
        <ActiveTaskRow task={task} issueId="issue-1" />
      </QueryClientProvider>,
    );
  }

  it("renders running status as elapsed time only", () => {
    renderRow();

    expect(screen.getByText("5m 04s")).toBeInTheDocument();
    expect(screen.queryByText(/events?/i)).not.toBeInTheDocument();
    expect(screen.getByText("Started from comment")).toBeInTheDocument();
    expect(screen.getByText("View transcript")).toBeInTheDocument();
    expect(screen.queryByTestId("task-timeline-preview")).not.toBeInTheDocument();
  });

  it("shows a live inline task timeline while the task is running", () => {
    renderRow(makeTask(), [
      {
        task_id: "task-1",
        issue_id: "issue-1",
        seq: 1,
        type: "thinking",
        content: "Inspecting the failing issue path",
      },
      {
        task_id: "task-1",
        issue_id: "issue-1",
        seq: 2,
        type: "tool_use",
        tool: "Read",
        input: { file_path: "packages/views/issues/components/issue-detail.tsx" },
      },
      {
        task_id: "task-1",
        issue_id: "issue-1",
        seq: 3,
        type: "text",
        content: "I found the issue.",
      },
    ]);

    expect(screen.getByText("Inspecting the failing issue path")).toBeInTheDocument();
    expect(screen.getByText("Read")).toBeInTheDocument();
    expect(screen.getByText("I found the issue.")).toBeInTheDocument();
  });
});
