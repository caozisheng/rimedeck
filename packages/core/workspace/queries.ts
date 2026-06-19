import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { Agent, Squad, Workspace } from "../types";

export const workspaceKeys = {
  all: (wsId: string) => ["workspaces", wsId] as const,
  list: () => ["workspaces", "list"] as const,
  members: (wsId: string) => ["workspaces", wsId, "members"] as const,
  invitations: (wsId: string) => ["workspaces", wsId, "invitations"] as const,
  myInvitations: () => ["invitations", "mine"] as const,
  agents: (wsId: string) => ["workspaces", wsId, "agents"] as const,
  squads: (wsId: string) => ["workspaces", wsId, "squads"] as const,
  // Per-squad member status. Lives under the workspace key tree so
  // workspace switches naturally drop the cache, and so a broad
  // `["workspaces", wsId, "squads"]` invalidation covers it.
  squadMemberStatus: (wsId: string, squadId: string) =>
    ["workspaces", wsId, "squads", squadId, "members-status"] as const,
  skills: (wsId: string) => ["workspaces", wsId, "skills"] as const,
  workflows: (wsId: string) => ["workspaces", wsId, "workflows"] as const,
  workflowTemplates: (wsId: string) => ["workspaces", wsId, "workflows", "templates"] as const,
  assigneeFrequency: (wsId: string) => ["workspaces", wsId, "assignee-frequency"] as const,
};

export function workspaceListOptions() {
  return queryOptions({
    queryKey: workspaceKeys.list(),
    queryFn: () => api.listWorkspaces(),
  });
}

/** Resolves the workspace whose slug matches, from the cached workspace list. */
export function workspaceBySlugOptions(slug: string) {
  return queryOptions({
    ...workspaceListOptions(),
    select: (list: Workspace[]) => list.find((w) => w.slug === slug) ?? null,
  });
}

export function memberListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.members(wsId),
    queryFn: () => api.listMembers(wsId),
  });
}

export function agentListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.agents(wsId),
    queryFn: () =>
      api.listAgents({ workspace_id: wsId, include_archived: true }),
  });
}

export function squadListOptions(wsId: string) {
  return queryOptions<Squad[]>({
    queryKey: workspaceKeys.squads(wsId),
    queryFn: () => api.listSquads(),
    enabled: !!wsId,
  });
}

// Per-squad members status snapshot. The freshness signal is the WS task /
// agent / runtime invalidation wired in use-realtime-sync (which broadly
// invalidates `["workspaces", wsId, "squads"]`); the staleTime is a
// tab-focus safety net.
export function squadMemberStatusOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: workspaceKeys.squadMemberStatus(wsId, squadId),
    queryFn: () => api.getSquadMemberStatus(squadId),
    enabled: !!wsId && !!squadId,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function skillListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.skills(wsId),
    queryFn: () => api.listSkills(),
  });
}

export function skillDetailOptions(wsId: string, skillId: string) {
  return queryOptions({
    queryKey: [...workspaceKeys.skills(wsId), skillId] as const,
    queryFn: () => api.getSkill(skillId),
    enabled: !!skillId,
  });
}

export function workflowListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.workflows(wsId),
    queryFn: () => api.listWorkflows(),
  });
}

export function workflowDetailOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: [...workspaceKeys.workflows(wsId), workflowId] as const,
    queryFn: () => api.getWorkflow(workflowId),
    enabled: !!workflowId,
  });
}

export function workflowStatsOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: [...workspaceKeys.workflows(wsId), workflowId, "stats"] as const,
    queryFn: () => api.getWorkflowStats(workflowId),
    enabled: !!workflowId,
  });
}

export function workflowTemplateListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.workflowTemplates(wsId),
    queryFn: () => api.listWorkflowTemplates(),
  });
}

export const workflowRunKeys = {
  all: (wsId: string) => ["workflows", wsId, "runs"] as const,
  list: (wsId: string, workflowId: string) =>
    [...workflowRunKeys.all(wsId), "list", workflowId] as const,
  detail: (wsId: string, workflowId: string, runId: string) =>
    [...workflowRunKeys.all(wsId), "detail", workflowId, runId] as const,
};

export function workflowRunListOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: workflowRunKeys.list(wsId, workflowId),
    queryFn: () => api.listWorkflowRuns(workflowId),
    enabled: !!workflowId,
  });
}

export function workflowRunDetailOptions(
  wsId: string,
  workflowId: string,
  runId: string,
  opts?: { refetchInterval?: number },
) {
  return queryOptions({
    queryKey: workflowRunKeys.detail(wsId, workflowId, runId),
    queryFn: () => api.getWorkflowRun(workflowId, runId),
    enabled: !!runId,
    refetchInterval: opts?.refetchInterval,
  });
}

/**
 * Builds a `Map<skillId, Agent[]>` from the cached agent list. The server
 * already returns each agent with its full skill list inline, so no extra
 * request is needed — "which agents use skill X" is pure client-side fold.
 *
 * Exposed as a plain helper rather than a `queryOptions` with `select` so
 * the Map's identity is stable across unrelated agent-cache rerenders —
 * callers wrap this in `useMemo(..., [agents])` and only re-fold when the
 * agent array identity actually changes. Previously this was `{ select }`,
 * which returned a new Map every subscription tick and triggered cascading
 * re-renders on every `agent:updated` WS event.
 */
export function selectSkillAssignments(
  agents: Agent[] | undefined,
): Map<string, Agent[]> {
  const map = new Map<string, Agent[]>();
  if (!agents) return map;
  for (const a of agents) {
    if (a.archived_at) continue;
    for (const s of a.skills ?? []) {
      const existing = map.get(s.id);
      if (existing) existing.push(a);
      else map.set(s.id, [a]);
    }
  }
  return map;
}

/**
 * Builds a `workflowId → Agent[]` lookup from the `workflows` array
 * embedded in each agent, mirroring `selectSkillAssignments`.
 */
export function selectWorkflowAssignments(
  agents: Agent[] | undefined,
): Map<string, Agent[]> {
  const map = new Map<string, Agent[]>();
  if (!agents) return map;
  for (const a of agents) {
    if (a.archived_at) continue;
    for (const w of a.workflows ?? []) {
      const existing = map.get(w.id);
      if (existing) existing.push(a);
      else map.set(w.id, [a]);
    }
  }
  return map;
}

export function invitationListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.invitations(wsId),
    queryFn: () => api.listWorkspaceInvitations(wsId),
  });
}

export function myInvitationListOptions() {
  return queryOptions({
    queryKey: workspaceKeys.myInvitations(),
    queryFn: () => api.listMyInvitations(),
  });
}

export function assigneeFrequencyOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.assigneeFrequency(wsId),
    queryFn: () => api.getAssigneeFrequency(),
  });
}
