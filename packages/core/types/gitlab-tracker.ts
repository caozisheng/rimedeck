export interface GitlabTracker {
  id: string;
  instance_url: string;
  path_with_namespace: string;
  web_url: string;
  state: "active" | "degraded" | "disabled";
  webhook_state: "active" | "unavailable" | "error";
  last_pull_at: string | null;
  pending_outbox_count: number;
  failed_outbox_count: number;
  token_configured: boolean;
  can_manage: boolean;
}

export interface ListGitlabTrackersResponse {
  trackers: GitlabTracker[];
}
