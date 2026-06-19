export type WorkflowStatus = "draft" | "active" | "archived";

export interface WorkflowSummary {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  icon: string;
  category: string;
  status: WorkflowStatus;
  version: number;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  published_at: string | null;
}

export interface Workflow extends WorkflowSummary {
  graph: Record<string, unknown>;
}

export type WorkflowRunStatus = "pending" | "running" | "completed" | "failed" | "cancelled";

export interface WorkflowNodeExecution {
  id: string;
  run_id: string;
  node_id: string;
  node_type: string;
  status: string;
  inputs: Record<string, unknown> | null;
  outputs: Record<string, unknown> | null;
  error: string | null;
  started_at: string | null;
  completed_at: string | null;
  tokens_used: number;
  duration_ms: number;
}

export interface WorkflowRun {
  id: string;
  workflow_id: string;
  workspace_id: string;
  agent_id: string;
  source: string;
  trigger_input: Record<string, unknown> | null;
  status: WorkflowRunStatus;
  total_nodes: number;
  completed_nodes: number;
  current_node_id: string | null;
  output: Record<string, unknown> | null;
  error: string | null;
  issue_id: string | null;
  autopilot_run_id: string | null;
  triggered_by: string | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  total_tokens: number;
  total_cost: string | null;
  node_executions?: WorkflowNodeExecution[];
}

export interface WorkflowTemplate {
  id: string;
  name: string;
  name_zh?: string;
  category: string;
  description: string;
  description_zh?: string;
  node_count: number;
  tags: string[];
  file: string;
}

export interface ImportWarning {
  node_id: string;
  node_name: string;
  type: "unsupported" | "degraded" | "skipped";
  message: string;
}

export interface WorkflowStats {
  total_runs: number;
  completed_runs: number;
  failed_runs: number;
  total_tokens: number;
  avg_duration_ms: number;
  last_run_at: string;
}
