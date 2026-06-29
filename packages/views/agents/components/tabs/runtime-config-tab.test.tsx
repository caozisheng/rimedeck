// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { Agent } from "@rimedeck/core/types";
import { I18nProvider } from "@rimedeck/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { RuntimeConfigTab } from "./runtime-config-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  sops: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTab(
  overrides: Partial<Agent> = {},
  runtimeProvider = "openclaw",
  onSave = vi.fn().mockResolvedValue(undefined),
) {
  const agent = { ...baseAgent, ...overrides };
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const result = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <RuntimeConfigTab
          agent={agent}
          runtimeProvider={runtimeProvider}
          onSave={onSave}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...result, onSave };
}

describe("RuntimeConfigTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("saves execution timeout_minutes alongside openclaw config", async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({ runtime_config: {} }, "openclaw");

    fireEvent.change(screen.getByLabelText(/Task timeout/i), {
      target: { value: "45" },
    });
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(onSave).toHaveBeenCalledWith({
      runtime_config: {
        mode: "local",
        execution: { timeout_minutes: 45 },
      },
    });
  });

  it("shows the execution timeout UI for non-openclaw runtimes too", () => {
    renderTab({ runtime_config: {} }, "claude");

    expect(screen.getByLabelText(/Task timeout/i)).toBeInTheDocument();
    expect(screen.queryByText(/Routing mode/i)).not.toBeInTheDocument();
  });
});
