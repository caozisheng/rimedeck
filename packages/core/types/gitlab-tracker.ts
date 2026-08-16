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

export interface ValidateGitlabTrackerRequest {
  repository_url: string;
  access_token: string;
  instance_hint?: string;
}

export interface GitlabTrackerPermissions {
  can_write_issues: boolean;
  can_configure_webhook: boolean;
}

export interface ValidateGitlabTrackerResponse {
  host: string;
  instance_url: string;
  path_with_namespace: string;
  remote_project_id: number;
  web_url: string;
  default_branch: string;
  permissions: GitlabTrackerPermissions;
}

export interface CreateGitlabTrackerRequest {
  repository_url: string;
  access_token: string;
}

export interface RetryGitlabTrackerResponse {
  reset_count: number;
}

export interface ListLabelsParams {
  project_id?: string;
  source?: "local" | "gitlab";
  tracker_id?: string;
}
