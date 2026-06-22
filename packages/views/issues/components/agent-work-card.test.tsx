// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask, TaskMessagePayload } from "@rimedeck/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  taskMessagesOptions: vi.fn(),
  messagesByTaskId: new Map<string, TaskMessagePayload[]>(),
}));

vi.mock("@rimedeck/core/chat/queries", () => ({
  taskMessagesOptions: mockState.taskMessagesOptions,
}));

vi.mock("@rimedeck/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) =>
      type === "agent" && id === "agent-1" ? "Claude Agent" : id,
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

import { AgentWorkCard } from "./agent-work-card";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "00000000-0000-0000-0000-000000000001",
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
    trigger_summary: "Please inspect this issue",
    ...overrides,
  };
}

function renderCard(task = makeTask(), messages: TaskMessagePayload[] = []) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData(["task-messages", task.id], messages);
  mockState.messagesByTaskId.set(task.id, messages);

  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AgentWorkCard task={task} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockState.messagesByTaskId.clear();
  mockState.taskMessagesOptions.mockImplementation((taskId: string) => ({
    queryKey: ["task-messages", taskId],
    queryFn: () => Promise.resolve(mockState.messagesByTaskId.get(taskId) ?? []),
    enabled: true,
    staleTime: Infinity,
  }));
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-06-08T08:05:04Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("AgentWorkCard", () => {
  it("renders an expanded live work card with transcript events", () => {
    renderCard(makeTask(), [
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 1,
        type: "thinking",
        content: "Tracing the issue timeline",
      },
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 2,
        type: "tool_use",
        tool: "Read",
        input: { file_path: "packages/views/issues/components/issue-detail.tsx" },
      },
    ]);

    expect(screen.getAllByText("Working").length).toBeGreaterThan(0);
    expect(screen.getByText("5m 04s")).toBeInTheDocument();
    expect(screen.getByText("Tracing the issue timeline")).toBeInTheDocument();
    expect(screen.getByText("Read")).toBeInTheDocument();
  });
});
