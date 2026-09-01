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
  last_error_code?: string | null;
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
  /** Include visible custom labels imported from active GitLab trackers. */
  include_remote?: boolean;
}

// Health snapshot for the tracker section's dead-letter panel. Numbers
// only — no free-form error strings and no credential material per
// design §11.2.
export interface GitlabTrackerHealth {
  id: string;
  state: "active" | "degraded" | "disabled";
  webhook_state: "active" | "unavailable" | "error";
  last_pull_at: string | null;
  last_full_reconcile_at: string | null;
  last_webhook_at: string | null;
  last_error_code?: string | null;
  pending_outbox_count: number;
  retrying_outbox_count: number;
  failed_outbox_count: number;
}
