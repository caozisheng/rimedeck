// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { TaskMessagePayload } from "@rimedeck/core/types";
import { TaskTimelinePreview } from "./task-timeline-preview";

vi.mock("@rimedeck/core/api", () => ({
  api: {
    listTaskMessages: vi.fn().mockResolvedValue([]),
  },
}));

describe("TaskTimelinePreview", () => {
  it("renders recent task messages from the shared task-message cache", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const messages: TaskMessagePayload[] = [
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 1,
        type: "thinking",
        content: "Checking the issue flow",
      },
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 2,
        type: "tool_use",
        tool: "Read",
        input: { file_path: "packages/views/issues/components/issue-detail.tsx" },
      },
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 3,
        type: "text",
        content: "Done.",
      },
    ];
    qc.setQueryData(["task-messages", "00000000-0000-0000-0000-000000000001"], messages);

    render(
      <QueryClientProvider client={qc}>
        <TaskTimelinePreview taskId="00000000-0000-0000-0000-000000000001" />
      </QueryClientProvider>,
    );

    expect(screen.getByText("Checking the issue flow")).toBeInTheDocument();
    expect(screen.getByText("Read")).toBeInTheDocument();
    expect(screen.getByText("packages/views/issues/components/issue-detail.tsx")).toBeInTheDocument();
    expect(screen.getByText("Done.")).toBeInTheDocument();
  });

  it("lets long transcript rows expand from preview to full text", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const longThinking = [
      "Checking the issue workflow before editing.",
      "Reading the comment thread and task messages.",
      "Preparing the smallest safe change.",
    ].join("\n");
    const messages: TaskMessagePayload[] = [
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 1,
        type: "thinking",
        content: longThinking,
      },
    ];
    qc.setQueryData(["task-messages", "00000000-0000-0000-0000-000000000001"], messages);

    render(
      <QueryClientProvider client={qc}>
        <TaskTimelinePreview taskId="00000000-0000-0000-0000-000000000001" />
      </QueryClientProvider>,
    );

    expect(screen.getByText(/Reading the comment thread/)).toBeInTheDocument();
    expect(screen.getByTestId("task-timeline-row-summary")).toHaveClass("line-clamp-2");

    fireEvent.click(screen.getByRole("button", { name: "Expand Thinking" }));

    expect(screen.queryByTestId("task-timeline-row-summary")).not.toBeInTheDocument();
    expect(screen.getByTestId("task-timeline-row-detail")).not.toHaveClass("line-clamp-2");
    expect(screen.getByText(/Reading the comment thread/)).toBeInTheDocument();
    expect(screen.getByText(/Preparing the smallest safe change/)).toBeInTheDocument();
  });

  it("renders transcript content on its own full-width line", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const messages: TaskMessagePayload[] = [
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 1,
        type: "tool_use",
        tool: "Bash",
        input: {
          command: [
            "cat <<'COMMENT' | multica issue comment add c3e7cd61-3409-4133-9b2c-0308aa64187f --parent 3ec0d436-6d82-4bd0-a990-0f44961d9e0b --content-stdin",
            "hello",
            "COMMENT",
          ].join("\n"),
        },
      },
    ];
    qc.setQueryData(["task-messages", "00000000-0000-0000-0000-000000000001"], messages);

    render(
      <QueryClientProvider client={qc}>
        <TaskTimelinePreview taskId="00000000-0000-0000-0000-000000000001" />
      </QueryClientProvider>,
    );

    const summary = screen.getByTestId("task-timeline-row-summary");
    expect(summary).toHaveClass("ml-5", "line-clamp-2", "whitespace-pre-wrap", "break-words");
    expect(summary).not.toHaveClass("whitespace-nowrap", "text-ellipsis");

    fireEvent.click(screen.getByRole("button", { name: "Expand Bash" }));

    expect(screen.getByTestId("task-timeline-row-detail")).toHaveClass("ml-5");
    expect(screen.getByText(/COMMENT/)).toBeInTheDocument();
  });

  it("omits daemon public progress rows from the preview", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const messages: TaskMessagePayload[] = [
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 1,
        type: "progress",
        content: "Running Bash: curl wttr.in/Shenzhen",
      },
    ];
    qc.setQueryData(["task-messages", "00000000-0000-0000-0000-000000000001"], messages);

    render(
      <QueryClientProvider client={qc}>
        <TaskTimelinePreview taskId="00000000-0000-0000-0000-000000000001" />
      </QueryClientProvider>,
    );

    expect(screen.queryByTestId("task-timeline-preview")).not.toBeInTheDocument();
    expect(screen.queryByText("Progress")).not.toBeInTheDocument();
    expect(screen.queryByText("Thinking")).not.toBeInTheDocument();
    expect(screen.queryByText("Running Bash: curl wttr.in/Shenzhen")).not.toBeInTheDocument();
  });

  it("renders an empty fallback when no visible transcript rows exist", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(["task-messages", "00000000-0000-0000-0000-000000000001"], []);

    render(
      <QueryClientProvider client={qc}>
        <TaskTimelinePreview
          taskId="00000000-0000-0000-0000-000000000001"
          emptyFallback={<div>Waiting for the first events...</div>}
        />
      </QueryClientProvider>,
    );

    expect(screen.getByText("Waiting for the first events...")).toBeInTheDocument();
  });

  it("shows Codex exec_command events with the user-facing Bash labels", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const messages: TaskMessagePayload[] = [
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 1,
        type: "tool_use",
        tool: "exec_command",
        input: { command: "pwd" },
      },
      {
        task_id: "00000000-0000-0000-0000-000000000001",
        issue_id: "issue-1",
        seq: 2,
        type: "tool_result",
        tool: "exec_command",
        output: "C:\\Users\\miles\\Documents\\GitHub\\rimedeck",
      },
    ];
    qc.setQueryData(["task-messages", "00000000-0000-0000-0000-000000000001"], messages);

    render(
      <QueryClientProvider client={qc}>
        <TaskTimelinePreview taskId="00000000-0000-0000-0000-000000000001" />
      </QueryClientProvider>,
    );

    expect(screen.getByText("Bash")).toBeInTheDocument();
    expect(screen.getByText("Bash result")).toBeInTheDocument();
    expect(screen.queryByText("exec_command")).not.toBeInTheDocument();
    expect(screen.queryByText("exec_command result")).not.toBeInTheDocument();
  });
});
