import { queryOptions } from "@tanstack/react-query";
import { api } from "./api";

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
