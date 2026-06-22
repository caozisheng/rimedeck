export function getToolDisplayName(tool: string | undefined): string | undefined {
  if (!tool) return undefined;
  const normalized = tool.trim().toLowerCase().replace(/[\s-]+/g, "_");
  if (normalized === "exec_command" || normalized === "exec_command_result") {
    return "Bash";
  }
  return tool;
}

export function getToolResultDisplayName(tool: string | undefined): string {
  const name = getToolDisplayName(tool);
  return name ? `${name} result` : "Result";
}
