export interface BackupSkillFile {
  path: string;
  content: string;
}

export interface BackupSkill {
  name: string;
  description: string;
  content: string;
  config: Record<string, unknown>;
  files: BackupSkillFile[];
}

export interface BackupAgent {
  name: string;
  description: string;
  instructions: string;
  runtime_mode: string;
  runtime_config: Record<string, unknown>;
  custom_args: string[];
  mcp_config: unknown | null;
  model: string | null;
  thinking_level: string | null;
  visibility: string;
  max_concurrent_tasks: number;
  skill_names: string[];
}

export interface BackupSquadMember {
  member_type: "agent" | "member";
  name?: string;
  email?: string;
  role: string;
}

export interface BackupSquad {
  name: string;
  description: string;
  instructions: string;
  leader_name: string;
  members: BackupSquadMember[];
}

export interface BackupData {
  version: number;
  exported_at: string;
  app_version: string;
  skills: BackupSkill[];
  agents: BackupAgent[];
  squads: BackupSquad[];
}

export interface ImportResultCounts {
  skills: number;
  agents: number;
  squads: number;
}

export interface ImportResult {
  created: ImportResultCounts;
  skipped: ImportResultCounts;
  warnings: string[];
  errors: string[];
}

export interface ImportRequest extends BackupData {
  runtime_id: string;
  overwrite: boolean;
}
