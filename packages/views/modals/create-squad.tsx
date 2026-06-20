"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Crown, UserPlus, X } from "lucide-react";
import { api } from "@rimedeck/core/api";
import { useAuthStore } from "@rimedeck/core/auth";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useWorkspacePaths } from "@rimedeck/core/paths";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
} from "@rimedeck/core/workspace/queries";
import { runtimeListOptions } from "@rimedeck/core/runtimes/queries";
import { AGENT_DESCRIPTION_MAX_LENGTH } from "@rimedeck/core/agents";
import { isImeComposing } from "@rimedeck/core/utils";
import type { Agent, MemberWithUser, RuntimeDevice } from "@rimedeck/core/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@rimedeck/ui/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@rimedeck/ui/components/ui/popover";
import { Button } from "@rimedeck/ui/components/ui/button";
import { Input } from "@rimedeck/ui/components/ui/input";
import { Label } from "@rimedeck/ui/components/ui/label";
import { toast } from "sonner";

import { generateRoutingTable, type RoutingTableMember } from "@rimedeck/core/squads";
import { useNavigation } from "../navigation";
import { ActorAvatar } from "../common/actor-avatar";
import { AvatarPicker } from "../agents/components/avatar-picker";
import { CharCounter } from "../agents/components/char-counter";
import { RuntimePicker } from "../agents/components/runtime-picker";
import { ModelDropdown } from "../agents/components/model-dropdown";
import {
  PickerEmpty,
  PickerItem,
  PickerSection,
} from "../issues/components/pickers/property-picker";
import { matchesPinyin } from "../editor/extensions/pinyin-match";
import { useT } from "../i18n";

type SelectedMember = {
  type: "agent" | "member";
  id: string;
  name: string;
};

type LeaderMode = "existing" | "create-manager";

const CHIP_DISPLAY_LIMIT = 3;

export function CreateSquadModal({ onClose }: { onClose: () => void }) {
  const { t } = useT("modals");
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const currentUser = useAuthStore((s) => s.user);
  const currentUserId = currentUser?.id ?? null;

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: wsMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery({
    ...runtimeListOptions(wsId),
  });

  const activeAgents = useMemo(
    () => agents.filter((a: Agent) => !a.archived_at && a.runtime_id),
    [agents],
  );

  // -- Step state --
  const [step, setStep] = useState<1 | 2>(1);

  // -- Step 1: Leader selection --
  const [leaderMode, setLeaderMode] = useState<LeaderMode>("existing");
  // Existing agent selection
  const [leaderId, setLeaderId] = useState("");
  // Create-manager form
  const [managerName, setManagerName] = useState("Agent Manager");
  const [selectedRuntimeId, setSelectedRuntimeId] = useState("");
  const [selectedModel, setSelectedModel] = useState("");

  // -- Step 2: Squad info --
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [avatarUrl, setAvatarUrl] = useState<string | null>(null);
  const [selectedMembers, setSelectedMembers] = useState<SelectedMember[]>([]);
  const [creating, setCreating] = useState(false);

  // Default to create-manager when user has no agents.
  useEffect(() => {
    if (activeAgents.length === 0) setLeaderMode("create-manager");
  }, [activeAgents.length]);

  const handleLeaderChange = (id: string) => {
    setLeaderId(id);
    if (id) {
      setSelectedMembers((prev) =>
        prev.filter((m) => !(m.type === "agent" && m.id === id)),
      );
    }
  };

  const selectedRuntime = runtimes.find((r) => r.id === selectedRuntimeId) ?? null;

  const canProceedToStep2 =
    leaderMode === "existing"
      ? !!leaderId
      : !!managerName.trim() && !!selectedRuntimeId;

  const canSubmit = !!name.trim() && !creating;

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setCreating(true);
    try {
      let finalLeaderId = leaderId;

      if (leaderMode === "create-manager") {
        const result = await api.createAgentFromTemplate({
          template_slug: "agent-manager",
          name: managerName.trim(),
          runtime_id: selectedRuntimeId,
          model: selectedModel || undefined,
          visibility: "workspace",
        });
        finalLeaderId = result.agent.id;
        queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      }

      const squad = await api.createSquad({
        name: name.trim(),
        description: description.trim() || undefined,
        leader_id: finalLeaderId,
        avatar_url: avatarUrl ?? undefined,
      });
      queryClient.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });

      if (selectedMembers.length > 0) {
        await Promise.allSettled(
          selectedMembers.map(async (m) => {
            try {
              await api.addSquadMember(squad.id, {
                member_type: m.type,
                member_id: m.id,
              });
            } catch (err) {
              toast.warning(
                t(($) => $.create_squad.toast_member_add_failed, {
                  name: m.name,
                  error:
                    err instanceof Error ? err.message : "unknown error",
                }),
              );
            }
          }),
        );
        queryClient.invalidateQueries({
          queryKey: [...workspaceKeys.squads(wsId), squad.id, "members"],
        });
      }

      // Auto-generate routing table when creating via Agent Manager template.
      if (leaderMode === "create-manager" && selectedMembers.length > 0) {
        try {
          const rtMembers: RoutingTableMember[] = selectedMembers.map((m) => ({
            name: m.name,
            memberType: m.type,
            role: "",
          }));
          const routingTable = generateRoutingTable(rtMembers);
          await api.updateSquad(squad.id, { instructions: routingTable });
        } catch {
          // Non-fatal — squad is created, routing table can be added manually.
        }
      }

      onClose();
      toast.success(t(($) => $.create_squad.toast_created));
      router.push(wsPaths.squadDetail(squad.id));
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : leaderMode === "create-manager"
            ? t(($) => $.create_squad.toast_manager_failed)
            : t(($) => $.create_squad.toast_failed),
      );
      setCreating(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="p-0 gap-0 flex flex-col overflow-hidden !top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2 !w-full !max-w-2xl !h-[85vh]">
        <DialogHeader className="border-b px-5 py-3 space-y-0">
          <DialogTitle className="text-base font-semibold">
            {step === 1
              ? t(($) => $.create_squad.step1_title)
              : t(($) => $.create_squad.title)}
          </DialogTitle>
          <DialogDescription className="mt-1 text-xs">
            {step === 1
              ? t(($) => $.create_squad.step1_description)
              : t(($) => $.create_squad.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto p-5">
          {step === 1 ? (
            <StepLeader
              leaderMode={leaderMode}
              onModeChange={setLeaderMode}
              activeAgents={activeAgents}
              currentUserId={currentUserId}
              leaderId={leaderId}
              onLeaderChange={handleLeaderChange}
              managerName={managerName}
              onManagerNameChange={setManagerName}
              runtimes={runtimes}
              runtimesLoading={runtimesLoading}
              wsMembers={wsMembers}
              selectedRuntimeId={selectedRuntimeId}
              onRuntimeSelect={setSelectedRuntimeId}
              selectedRuntime={selectedRuntime}
              selectedModel={selectedModel}
              onModelChange={setSelectedModel}
            />
          ) : (
            <StepSquadInfo
              name={name}
              onNameChange={setName}
              description={description}
              onDescriptionChange={setDescription}
              avatarUrl={avatarUrl}
              onAvatarChange={setAvatarUrl}
              activeAgents={activeAgents}
              wsMembers={wsMembers}
              currentUserId={currentUserId}
              leaderId={leaderMode === "existing" ? leaderId : ""}
              selectedMembers={selectedMembers}
              onMembersChange={setSelectedMembers}
              onSubmit={handleSubmit}
            />
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
          {step === 1 ? (
            <>
              <Button variant="ghost" onClick={onClose}>
                {t(($) => $.create_squad.cancel)}
              </Button>
              <Button
                onClick={() => setStep(2)}
                disabled={!canProceedToStep2}
              >
                {t(($) => $.create_squad.next_step)}
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" onClick={() => setStep(1)} disabled={creating}>
                {t(($) => $.create_squad.prev_step)}
              </Button>
              <Button onClick={() => void handleSubmit()} disabled={!canSubmit}>
                {creating
                  ? t(($) => $.create_squad.submitting)
                  : t(($) => $.create_squad.submit)}
              </Button>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Step 1: Choose or create a Leader
// ---------------------------------------------------------------------------
function StepLeader({
  leaderMode,
  onModeChange,
  activeAgents,
  currentUserId,
  leaderId,
  onLeaderChange,
  managerName,
  onManagerNameChange,
  runtimes,
  runtimesLoading,
  wsMembers,
  selectedRuntimeId,
  onRuntimeSelect,
  selectedRuntime,
  selectedModel,
  onModelChange,
}: {
  leaderMode: LeaderMode;
  onModeChange: (mode: LeaderMode) => void;
  activeAgents: Agent[];
  currentUserId: string | null;
  leaderId: string;
  onLeaderChange: (id: string) => void;
  managerName: string;
  onManagerNameChange: (name: string) => void;
  runtimes: RuntimeDevice[];
  runtimesLoading: boolean;
  wsMembers: MemberWithUser[];
  selectedRuntimeId: string;
  onRuntimeSelect: (id: string) => void;
  selectedRuntime: RuntimeDevice | null;
  selectedModel: string;
  onModelChange: (model: string) => void;
}) {
  const { t } = useT("modals");

  return (
    <div className="space-y-4">
      {/* Radio: Select existing agent */}
      <label
        className={`flex items-start gap-3 rounded-lg border p-4 cursor-pointer transition-colors ${
          leaderMode === "existing"
            ? "border-foreground bg-accent/30"
            : "border-border hover:bg-accent/10"
        }`}
      >
        <input
          type="radio"
          name="leader-mode"
          checked={leaderMode === "existing"}
          onChange={() => onModeChange("existing")}
          className="mt-0.5 accent-foreground"
        />
        <div className="flex-1 min-w-0">
          <div className="text-sm font-medium">
            {t(($) => $.create_squad.mode_existing)}
          </div>
          {leaderMode === "existing" && (
            <div className="mt-3">
              <LeaderPicker
                agents={activeAgents}
                currentUserId={currentUserId}
                value={leaderId}
                onChange={onLeaderChange}
              />
            </div>
          )}
        </div>
      </label>

      {/* Radio: Create Agent Manager */}
      <label
        className={`flex items-start gap-3 rounded-lg border p-4 cursor-pointer transition-colors ${
          leaderMode === "create-manager"
            ? "border-foreground bg-accent/30"
            : "border-border hover:bg-accent/10"
        }`}
      >
        <input
          type="radio"
          name="leader-mode"
          checked={leaderMode === "create-manager"}
          onChange={() => onModeChange("create-manager")}
          className="mt-0.5 accent-foreground"
        />
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 text-sm font-medium">
            <Crown className="h-4 w-4 text-amber-500" />
            {t(($) => $.create_squad.mode_create_manager)}
          </div>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {t(($) => $.create_squad.mode_create_manager_hint)}
          </p>
          {leaderMode === "create-manager" && (
            <div className="mt-3 space-y-3">
              <div>
                <Label className="text-xs text-muted-foreground">
                  {t(($) => $.create_squad.manager_name_label)}
                </Label>
                <Input
                  type="text"
                  value={managerName}
                  onChange={(e) => onManagerNameChange(e.target.value)}
                  placeholder={t(($) => $.create_squad.manager_name_placeholder)}
                  className="mt-1"
                />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">
                  {t(($) => $.create_squad.runtime_label)}
                </Label>
                <div className="mt-1">
                  <RuntimePicker
                    runtimes={runtimes}
                    runtimesLoading={runtimesLoading}
                    members={wsMembers}
                    currentUserId={currentUserId}
                    selectedRuntimeId={selectedRuntimeId}
                    onSelect={onRuntimeSelect}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">
                  {t(($) => $.create_squad.model_label)}
                </Label>
                <div className="mt-1">
                  <ModelDropdown
                    runtimeId={selectedRuntimeId || null}
                    runtimeOnline={selectedRuntime?.status === "online"}
                    value={selectedModel}
                    onChange={onModelChange}
                    disabled={!selectedRuntimeId}
                  />
                </div>
              </div>
            </div>
          )}
        </div>
      </label>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step 2: Squad info (name, description, avatar, members)
// ---------------------------------------------------------------------------
function StepSquadInfo({
  name,
  onNameChange,
  description,
  onDescriptionChange,
  avatarUrl,
  onAvatarChange,
  activeAgents,
  wsMembers,
  currentUserId,
  leaderId,
  selectedMembers,
  onMembersChange,
  onSubmit,
}: {
  name: string;
  onNameChange: (v: string) => void;
  description: string;
  onDescriptionChange: (v: string) => void;
  avatarUrl: string | null;
  onAvatarChange: (v: string | null) => void;
  activeAgents: Agent[];
  wsMembers: MemberWithUser[];
  currentUserId: string | null;
  leaderId: string;
  selectedMembers: SelectedMember[];
  onMembersChange: (v: SelectedMember[]) => void;
  onSubmit: () => void;
}) {
  const { t } = useT("modals");

  return (
    <div className="space-y-4 min-w-0">
      <div className="flex items-start gap-4">
        <AvatarPicker value={avatarUrl} onChange={onAvatarChange} size={64} />
        <div className="flex-1 min-w-0 space-y-3">
          <div>
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.create_squad.name_label)}
            </Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder={t(($) => $.create_squad.name_placeholder)}
              className="mt-1"
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === "Enter") void onSubmit();
              }}
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.create_squad.description_label)}
            </Label>
            <Input
              type="text"
              value={description}
              onChange={(e) => onDescriptionChange(e.target.value)}
              placeholder={t(($) => $.create_squad.description_placeholder)}
              maxLength={AGENT_DESCRIPTION_MAX_LENGTH}
              className="mt-1"
            />
            <div className="mt-1">
              <CharCounter
                length={[...description].length}
                max={AGENT_DESCRIPTION_MAX_LENGTH}
              />
            </div>
          </div>
        </div>
      </div>

      <AdditionalMembersPicker
        agents={activeAgents}
        members={wsMembers}
        currentUserId={currentUserId}
        leaderId={leaderId}
        value={selectedMembers}
        onChange={onMembersChange}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// LeaderPicker — single-select agent picker
// ---------------------------------------------------------------------------
function LeaderPicker({
  agents,
  currentUserId,
  value,
  onChange,
}: {
  agents: Agent[];
  currentUserId: string | null;
  value: string;
  onChange: (id: string) => void;
}) {
  const { t } = useT("modals");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const myAgents = useMemo(
    () => (currentUserId ? agents.filter((a) => a.owner_id === currentUserId) : []),
    [agents, currentUserId],
  );
  const otherAgents = useMemo(
    () =>
      currentUserId
        ? agents.filter((a) => a.owner_id !== currentUserId)
        : agents,
    [agents, currentUserId],
  );

  const q = filter.trim().toLowerCase();
  const matches = (a: Agent) =>
    !q || a.name.toLowerCase().includes(q) || matchesPinyin(a.name, q);
  const filteredMine = myAgents.filter(matches);
  const filteredOthers = otherAgents.filter(matches);

  const selected = agents.find((a) => a.id === value) ?? null;
  const noAgents = agents.length === 0;

  if (noAgents) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-dashed bg-muted/30 px-3 py-2.5 text-sm text-muted-foreground">
        {t(($) => $.create_squad.no_agents)}
      </div>
    );
  }

  return (
    <Popover
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
    >
      <PopoverTrigger className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 text-left text-sm transition-colors hover:bg-muted">
        {selected ? (
          <ActorAvatar actorType="agent" actorId={selected.id} size={20} showStatusDot />
        ) : (
          <UserPlus className="h-4 w-4 shrink-0 text-muted-foreground" />
        )}
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">
            {selected?.name ?? t(($) => $.create_squad.leader_placeholder)}
          </div>
          {selected?.description && (
            <div className="truncate text-xs text-muted-foreground">
              {selected.description}
            </div>
          )}
        </div>
        <ChevronDown
          className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
            open ? "rotate-180" : ""
          }`}
        />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[var(--anchor-width)] p-0">
        <div className="border-b px-2 py-1.5">
          <input
            autoFocus
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder={t(($) => $.create_squad.picker_search_placeholder)}
            className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          />
        </div>
        <div className="max-h-72 overflow-y-auto p-1">
          {filteredMine.length > 0 && (
            <PickerSection label={t(($) => $.create_squad.group_my_agents)}>
              {filteredMine.map((a) => (
                <PickerItem
                  key={a.id}
                  selected={value === a.id}
                  onClick={() => {
                    onChange(a.id);
                    setOpen(false);
                    setFilter("");
                  }}
                >
                  <ActorAvatar actorType="agent" actorId={a.id} size={18} showStatusDot />
                  <span className="truncate">{a.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
          {filteredOthers.length > 0 && (
            <PickerSection label={t(($) => $.create_squad.group_workspace_agents)}>
              {filteredOthers.map((a) => (
                <PickerItem
                  key={a.id}
                  selected={value === a.id}
                  onClick={() => {
                    onChange(a.id);
                    setOpen(false);
                    setFilter("");
                  }}
                >
                  <ActorAvatar actorType="agent" actorId={a.id} size={18} showStatusDot />
                  <span className="truncate">{a.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
          {filteredMine.length === 0 && filteredOthers.length === 0 && (
            <PickerEmpty />
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

// ---------------------------------------------------------------------------
// AdditionalMembersPicker — multi-select agents + workspace members
// ---------------------------------------------------------------------------
function AdditionalMembersPicker({
  agents,
  members,
  currentUserId,
  leaderId,
  value,
  onChange,
}: {
  agents: Agent[];
  members: MemberWithUser[];
  currentUserId: string | null;
  leaderId: string;
  value: SelectedMember[];
  onChange: (next: SelectedMember[]) => void;
}) {
  const { t } = useT("modals");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const isSelected = (type: "agent" | "member", id: string) =>
    value.some((m) => m.type === type && m.id === id);

  const toggle = (m: SelectedMember) => {
    if (isSelected(m.type, m.id)) {
      onChange(value.filter((x) => !(x.type === m.type && x.id === m.id)));
    } else {
      onChange([...value, m]);
    }
  };

  const remove = (m: SelectedMember) => {
    onChange(value.filter((x) => !(x.type === m.type && x.id === m.id)));
  };

  const myAgents = useMemo(
    () =>
      currentUserId
        ? agents.filter((a) => a.owner_id === currentUserId && a.id !== leaderId)
        : [],
    [agents, currentUserId, leaderId],
  );
  const otherAgents = useMemo(
    () =>
      agents.filter(
        (a) =>
          a.id !== leaderId &&
          (currentUserId ? a.owner_id !== currentUserId : true),
      ),
    [agents, currentUserId, leaderId],
  );

  const q = filter.trim().toLowerCase();
  const agentMatches = (a: Agent) =>
    !q || a.name.toLowerCase().includes(q) || matchesPinyin(a.name, q);
  const memberMatches = (m: MemberWithUser) =>
    !q || m.name.toLowerCase().includes(q) || matchesPinyin(m.name, q);

  const filteredMine = myAgents.filter(agentMatches);
  const filteredOthers = otherAgents.filter(agentMatches);
  const filteredMembers = members.filter(memberMatches);
  const anyResults =
    filteredMine.length + filteredOthers.length + filteredMembers.length > 0;

  return (
    <div>
      <Label className="text-xs text-muted-foreground">
        {t(($) => $.create_squad.members_label)}{" "}
        <span className="text-muted-foreground/60">
          {t(($) => $.create_squad.members_optional)}
        </span>
      </Label>
      <p className="mt-0.5 mb-1.5 text-xs text-muted-foreground">
        {t(($) => $.create_squad.members_hint)}
      </p>

      <Popover
        open={open}
        onOpenChange={(v) => {
          setOpen(v);
          if (!v) setFilter("");
        }}
      >
        <PopoverTrigger
          render={
            <div
              role="combobox"
              aria-haspopup="listbox"
              aria-expanded={open}
              aria-controls="squad-member-listbox"
              tabIndex={0}
              className="flex w-full min-w-0 cursor-pointer items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 text-left text-sm transition-colors hover:bg-muted focus:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
            >
              {value.length === 0 ? (
                <>
                  <UserPlus className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate text-muted-foreground">
                    {t(($) => $.create_squad.members_placeholder)}
                  </span>
                </>
              ) : (
                <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
                  {value.slice(0, CHIP_DISPLAY_LIMIT).map((m) => (
                    <MemberChip
                      key={`${m.type}:${m.id}`}
                      m={m}
                      onRemove={() => remove(m)}
                    />
                  ))}
                  {value.length > CHIP_DISPLAY_LIMIT && (
                    <span className="rounded-md bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground tabular-nums">
                      {t(($) => $.create_squad.members_more_count, {
                        count: value.length - CHIP_DISPLAY_LIMIT,
                      })}
                    </span>
                  )}
                </div>
              )}
              <ChevronDown
                className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
                  open ? "rotate-180" : ""
                }`}
              />
            </div>
          }
        />
        <PopoverContent align="start" className="w-[var(--anchor-width)] p-0">
          <div className="border-b px-2 py-1.5">
            <input
              autoFocus
              type="text"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder={t(($) => $.create_squad.picker_search_placeholder)}
              className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
            />
          </div>
          <div id="squad-member-listbox" role="listbox" className="max-h-72 overflow-y-auto p-1">
            {filteredMine.length > 0 && (
              <PickerSection label={t(($) => $.create_squad.group_my_agents)}>
                {filteredMine.map((a) => (
                  <PickerItem
                    key={a.id}
                    selected={isSelected("agent", a.id)}
                    onClick={() =>
                      toggle({ type: "agent", id: a.id, name: a.name })
                    }
                  >
                    <ActorAvatar actorType="agent" actorId={a.id} size={18} showStatusDot />
                    <span className="truncate">{a.name}</span>
                  </PickerItem>
                ))}
              </PickerSection>
            )}
            {filteredOthers.length > 0 && (
              <PickerSection label={t(($) => $.create_squad.group_workspace_agents)}>
                {filteredOthers.map((a) => (
                  <PickerItem
                    key={a.id}
                    selected={isSelected("agent", a.id)}
                    onClick={() =>
                      toggle({ type: "agent", id: a.id, name: a.name })
                    }
                  >
                    <ActorAvatar actorType="agent" actorId={a.id} size={18} showStatusDot />
                    <span className="truncate">{a.name}</span>
                  </PickerItem>
                ))}
              </PickerSection>
            )}
            {filteredMembers.length > 0 && (
              <PickerSection label={t(($) => $.create_squad.group_members)}>
                {filteredMembers.map((m) => (
                  <PickerItem
                    key={m.user_id}
                    selected={isSelected("member", m.user_id)}
                    onClick={() =>
                      toggle({ type: "member", id: m.user_id, name: m.name })
                    }
                  >
                    <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
                    <span className="truncate">{m.name}</span>
                  </PickerItem>
                ))}
              </PickerSection>
            )}
            {!anyResults && <PickerEmpty />}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}

function MemberChip({
  m,
  onRemove,
}: {
  m: SelectedMember;
  onRemove: () => void;
}) {
  const { t } = useT("modals");
  return (
    <span className="inline-flex items-center gap-1 rounded-full border bg-background px-1.5 py-0.5 text-xs">
      <ActorAvatar actorType={m.type} actorId={m.id} size={14} />
      <span className="max-w-[120px] truncate">{m.name}</span>
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          onRemove();
        }}
        aria-label={t(($) => $.create_squad.members_remove_aria, { name: m.name })}
        className="rounded-full text-muted-foreground hover:text-foreground"
      >
        <X className="h-3 w-3" />
      </button>
    </span>
  );
}
