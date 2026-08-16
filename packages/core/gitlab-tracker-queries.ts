import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import type {
  CreateGitlabTrackerRequest,
  ValidateGitlabTrackerRequest,
} from "./types";

export const gitlabTrackerKeys = {
  all: (wsId: string) => ["gitlab-trackers", wsId] as const,
  project: (wsId: string, projectId: string) =>
    [...gitlabTrackerKeys.all(wsId), projectId] as const,
};

export function projectGitlabTrackersOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: gitlabTrackerKeys.project(wsId, projectId),
    queryFn: () => api.listProjectGitlabTrackers(projectId),
    select: (data) => data.trackers,
    enabled: !!projectId,
  });
}

// Validation is a mutation by design: PATs must never enter the React Query
// cache or devtools. The token exists only in this call's local variables.
export function useValidateGitlabTracker() {
  return useMutation({
    mutationFn: (data: ValidateGitlabTrackerRequest) => api.validateGitlabTracker(data),
  });
}

export function useCreateProjectGitlabTracker(wsId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateGitlabTrackerRequest) => api.createProjectGitlabTracker(projectId, data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: gitlabTrackerKeys.project(wsId, projectId) }),
  });
}

export function useSyncGitlabTracker(wsId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackerId: string) => api.syncGitlabTracker(projectId, trackerId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: gitlabTrackerKeys.project(wsId, projectId) }),
  });
}

export function useRetryGitlabTracker(wsId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackerId: string) => api.retryGitlabTracker(projectId, trackerId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: gitlabTrackerKeys.project(wsId, projectId) }),
  });
}

export function useRotateGitlabTrackerToken(wsId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ trackerId, accessToken }: { trackerId: string; accessToken: string }) => api.rotateGitlabTrackerToken(projectId, trackerId, accessToken),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: gitlabTrackerKeys.project(wsId, projectId) }),
  });
}

export function useDisableGitlabTracker(wsId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackerId: string) => api.disableGitlabTracker(projectId, trackerId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: gitlabTrackerKeys.project(wsId, projectId) }),
  });
}

export function useDeleteGitlabTrackerMirrors(wsId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackerId: string) => api.deleteGitlabTrackerMirrors(projectId, trackerId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: gitlabTrackerKeys.project(wsId, projectId) }),
  });
}
