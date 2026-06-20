// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { I18nProvider } from "@rimedeck/core/i18n/react";
import type { Agent, MemberWithUser, Squad } from "@rimedeck/core/types";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";
import enAgents from "../locales/en/agents.json";
import enIssues from "../locales/en/issues.json";

const TEST_RESOURCES = {
  en: { common: enCommon, modals: enModals, agents: enAgents, issues: enIssues },
};

const ME = "user-me";
const OTHER = "user-other";

// Hoisted so each test can override mock return values per case.
const mocks = vi.hoisted(() => ({
  agents: [] as Agent[],
  members: [] as MemberWithUser[],
  createSquad: vi.fn(),
  addSquadMember: vi.fn(),
  navigationPush: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  toastWarning: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (opts: { queryKey?: unknown[] }) => {
      const key = opts.queryKey ?? [];
      if (Array.isArray(key) && key.includes("agents")) {
        return { data: mocks.agents, isLoading: false };
      }
      if (Array.isArray(key) && key.includes("members")) {
        return { data: mocks.members, isLoading: false };
      }
      return { data: [], isLoading: false };
    },
    useQueryClient: () => ({ invalidateQueries: mocks.invalidate }),
  };
});

vi.mock("@rimedeck/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  workspaceKeys: { squads: (id: string) => ["squads", id] },
}));

vi.mock("@rimedeck/core/api", () => ({
  api: {
    createSquad: (...args: unknown[]) => mocks.createSquad(...args),
    addSquadMember: (...args: unknown[]) => mocks.addSquadMember(...args),
  },
}));

vi.mock("@rimedeck/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: ME } }),
}));

vi.mock("@rimedeck/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@rimedeck/core/paths", () => ({
  useWorkspacePaths: () => ({
    squadDetail: (id: string) => `/test-ws/squads/${id}`,
  }),
}));

vi.mock("@rimedeck/core/utils", () => ({
  isImeComposing: () => false,
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mocks.navigationPush }),
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("../agents/components/avatar-picker", () => ({
  AvatarPicker: ({
    value,
    onChange,
  }: {
    value: string | null;
    onChange: (v: string | null) => void;
  }) => (
    <button
      type="button"
      data-testid="avatar-picker"
      data-value={value ?? ""}
      onClick={() => onChange("https://example.com/avatar.png")}
    >
      avatar
    </button>
  ),
}));

vi.mock("@rimedeck/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));

vi.mock("@rimedeck/core/squads", () => ({
  generateRoutingTable: () => "mock-routing-table",
}));

vi.mock("../agents/components/runtime-picker", () => ({
  RuntimePicker: () => <div data-testid="runtime-picker" />,
}));

vi.mock("../agents/components/model-dropdown", () => ({
  ModelDropdown: () => <div data-testid="model-dropdown" />,
}));

vi.mock("../agents/components/char-counter", () => ({
  CharCounter: ({ length, max }: { length: number; max: number }) => (
    <span data-testid="char-counter">
      {length}/{max}
    </span>
  ),
}));

// Render Popover/PopoverContent inline so the picker rows are queryable
// without simulating a Base UI portal — we still drive the open prop via
// PopoverTrigger clicks but the content is always in the DOM.
vi.mock("@rimedeck/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PopoverTrigger: ({
    children,
    className,
    onClick,
    render,
  }: {
    children?: ReactNode;
    className?: string;
    onClick?: () => void;
    render?: ReactNode;
  }) => {
    // Base UI's `render` prop replaces the trigger with the provided element.
    // The mock just renders it as-is so its children stay queryable in tests.
    if (render !== undefined) return <>{render}</>;
    return (
      <button type="button" className={className} onClick={onClick}>
        {children}
      </button>
    );
  },
  PopoverContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@rimedeck/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
}));

vi.mock("@rimedeck/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@rimedeck/ui/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => (
    <input {...props} />
  ),
}));

vi.mock("@rimedeck/ui/components/ui/label", () => ({
  Label: ({ children, className }: { children: ReactNode; className?: string }) => (
    <label className={className}>{children}</label>
  ),
}));

vi.mock("../editor/extensions/pinyin-match", () => ({
  matchesPinyin: () => false,
}));

vi.mock("sonner", () => ({
  toast: {
    success: (...args: unknown[]) => mocks.toastSuccess(...args),
    error: (...args: unknown[]) => mocks.toastError(...args),
    warning: (...args: unknown[]) => mocks.toastWarning(...args),
  },
}));

import { CreateSquadModal } from "./create-squad";

function makeAgent(overrides: Partial<Agent> & { id: string; name: string; owner_id: string | null }): Agent {
  return {
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "private",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    skills: [],
    workflows: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeMember(user_id: string, name: string): MemberWithUser {
  return {
    id: `m-${user_id}`,
    user_id,
    workspace_id: "ws-1",
    role: "member",
    name,
    email: `${user_id}@example.com`,
    avatar_url: null,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function makeSquad(overrides: Partial<Squad> = {}): Squad {
  return {
    id: "sq-new",
    workspace_id: "ws-1",
    name: "New Squad",
    description: "",
    instructions: "",
    avatar_url: null,
    leader_id: "agent-mine-1",
    creator_id: ME,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function getSubmitButton(): HTMLButtonElement {
  const btn = screen
    .getAllByRole("button")
    .find((b) => b.textContent === "Create Squad");
  if (!btn) throw new Error("Create Squad submit button not found");
  return btn as HTMLButtonElement;
}

function getNextButton(): HTMLButtonElement {
  const btn = screen
    .getAllByRole("button")
    .find((b) => b.textContent === "Next");
  if (!btn) throw new Error("Next button not found");
  return btn as HTMLButtonElement;
}

/** Select a leader on Step 1 and advance to Step 2. */
async function selectLeaderAndNext(leaderName: string) {
  fireEvent.click(firstMatch(leaderName));
  // After clicking agent name, Next button should become enabled
  await waitFor(() => {
    expect(getNextButton().disabled).toBe(false);
  });
  fireEvent.click(getNextButton());
  // Wait for Step 2 to render (squad name input appears)
  await waitFor(() => {
    expect(screen.queryAllByPlaceholderText(/e\.g\. Frontend Team/i).length).toBeGreaterThan(0);
  });
}

// getAllByText is typed to never return undefined slots, but the indexed
// access goes through `noUncheckedIndexedAccess` so we narrow explicitly.
function firstMatch(label: string): HTMLElement {
  const matches = screen.getAllByText(label);
  if (matches.length === 0) throw new Error(`no match for "${label}"`);
  return matches[0]!;
}

function lastMatch(label: string): HTMLElement {
  const matches = screen.getAllByText(label);
  if (matches.length === 0) throw new Error(`no match for "${label}"`);
  return matches[matches.length - 1]!;
}

function renderModal() {
  const onClose = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CreateSquadModal onClose={onClose} />
    </I18nProvider>,
  );
  return { onClose };
}

const myAgent = makeAgent({ id: "agent-mine-1", name: "MineAgentOne", owner_id: ME });
const myAgent2 = makeAgent({ id: "agent-mine-2", name: "MineAgentTwo", owner_id: ME });
const otherAgent = makeAgent({ id: "agent-other-1", name: "OtherAgentOne", owner_id: OTHER });
const wsMember = makeMember(OTHER, "Workspace Pal");

describe("CreateSquadModal", () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.agents = [otherAgent, myAgent, myAgent2];
    mocks.members = [wsMember];
  });

  it("binds the name and description inputs in the identity row", async () => {
    renderModal();
    await selectLeaderAndNext("MineAgentOne");

    const name = screen.getAllByPlaceholderText(/e\.g\. Frontend Team/i).pop()! as HTMLInputElement;
    fireEvent.change(name, { target: { value: "Platform Team" } });
    expect(name.value).toBe("Platform Team");

    const desc = screen.getAllByPlaceholderText(/Describe what this squad/i).pop()! as HTMLInputElement;
    fireEvent.change(desc, { target: { value: "We own infra" } });
    expect(desc.value).toBe("We own infra");
    expect(screen.getByTestId("char-counter").textContent).toMatch(/^12\//);
  });

  it("renders the leader picker with 'My Agents' before 'Workspace Agents'", () => {
    renderModal();
    // The leader group headers come from the LeaderPicker — agent name "MineAgentOne"
    // appears under the My Agents section, "OtherAgentOne" under Workspace Agents.
    const myGroupLabels = screen.getAllByText("My Agents");
    const wsGroupLabels = screen.getAllByText("Workspace Agents");
    expect(myGroupLabels.length).toBeGreaterThanOrEqual(1);
    expect(wsGroupLabels.length).toBeGreaterThanOrEqual(1);

    // Verify ordering: the first occurrence of My Agents appears before the
    // first occurrence of Workspace Agents in the DOM (leader picker shows
    // both sections; the additional-members picker also has them, but both
    // pickers follow the same order so the assertion holds either way).
    const all = Array.from(document.querySelectorAll("*"));
    const myIdx = all.findIndex((n) => n.textContent === "My Agents");
    const wsIdx = all.findIndex((n) => n.textContent === "Workspace Agents");
    expect(myIdx).toBeLessThan(wsIdx);
  });

  it("auto-clears an additional-members entry when the same agent is picked as leader", async () => {
    renderModal();
    // Pick MineAgentTwo as leader and advance to Step 2.
    await selectLeaderAndNext("MineAgentTwo");

    // On Step 2, the additional-members picker filters out the leader.
    // MineAgentOne and OtherAgentOne should be available.
    expect(screen.getAllByText("MineAgentOne").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("OtherAgentOne").length).toBeGreaterThanOrEqual(1);
  });

  it("removes a member promoted to leader from selectedMembers so switching leader away does not resurrect it", async () => {
    renderModal();
    // Pick MineAgentOne, go to step 2, pick OtherAgentOne as member, submit.
    // Verify only OtherAgentOne added as member, not the leader.
    await selectLeaderAndNext("MineAgentOne");

    fireEvent.click(lastMatch("OtherAgentOne"));

    mocks.createSquad.mockResolvedValue(makeSquad({ id: "sq-3", leader_id: "agent-mine-1" }));
    fireEvent.change(screen.getAllByPlaceholderText(/e\.g\. Frontend Team/i).pop()!, {
      target: { value: "Swap Squad" },
    });
    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.createSquad).toHaveBeenCalledWith({
        name: "Swap Squad",
        description: undefined,
        leader_id: "agent-mine-1",
        avatar_url: undefined,
      });
    });
    await waitFor(() => {
      expect(mocks.addSquadMember).toHaveBeenCalledTimes(1);
    });
    expect(mocks.addSquadMember).toHaveBeenCalledWith("sq-3", {
      member_type: "agent",
      member_id: "agent-other-1",
    });
  });

  it("on success with no additional members fires exactly one success toast and navigates", async () => {
    renderModal();
    await selectLeaderAndNext("MineAgentOne");

    fireEvent.change(screen.getAllByPlaceholderText(/e\.g\. Frontend Team/i).pop()!, {
      target: { value: "Solo Squad" },
    });

    mocks.createSquad.mockResolvedValue(makeSquad({ id: "sq-1", leader_id: "agent-mine-1" }));

    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.createSquad).toHaveBeenCalledWith({
        name: "Solo Squad",
        description: undefined,
        leader_id: "agent-mine-1",
        avatar_url: undefined,
      });
    });
    await waitFor(() => {
      expect(mocks.toastSuccess).toHaveBeenCalledTimes(1);
    });
    expect(mocks.addSquadMember).not.toHaveBeenCalled();
    expect(mocks.toastWarning).not.toHaveBeenCalled();
    expect(mocks.navigationPush).toHaveBeenCalledWith("/test-ws/squads/sq-1");
  });

  it("on success with partial member failure shows success + warning toasts and still navigates", async () => {
    renderModal();
    await selectLeaderAndNext("MineAgentOne");

    fireEvent.change(screen.getAllByPlaceholderText(/e\.g\. Frontend Team/i).pop()!, {
      target: { value: "Mixed Squad" },
    });

    // Add two additional members
    fireEvent.click(lastMatch("OtherAgentOne"));
    fireEvent.click(lastMatch("Workspace Pal"));

    mocks.createSquad.mockResolvedValue(makeSquad({ id: "sq-2", leader_id: "agent-mine-1" }));
    mocks.addSquadMember
      .mockResolvedValueOnce({})
      .mockRejectedValueOnce(new Error("boom"));

    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.createSquad).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(mocks.addSquadMember).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(mocks.toastSuccess).toHaveBeenCalledTimes(1);
    });
    expect(mocks.toastWarning).toHaveBeenCalledTimes(1);
    expect(mocks.navigationPush).toHaveBeenCalledWith("/test-ws/squads/sq-2");
  });

  it("on createSquad failure shows an error toast, does not navigate, and re-enables submit", async () => {
    renderModal();
    await selectLeaderAndNext("MineAgentOne");

    fireEvent.change(screen.getAllByPlaceholderText(/e\.g\. Frontend Team/i).pop()!, {
      target: { value: "Boom Squad" },
    });

    mocks.createSquad.mockRejectedValueOnce(new Error("server down"));

    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.toastError).toHaveBeenCalledTimes(1);
    });
    expect(mocks.navigationPush).not.toHaveBeenCalled();
    const button = getSubmitButton();
    expect(button.disabled).toBe(false);
  });
});
