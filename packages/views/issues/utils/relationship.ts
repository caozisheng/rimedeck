import type { Issue } from "@rimedeck/core/types";

export function computeRelatedIds(
  issue: Issue,
  allIssues: Issue[],
): Set<string> {
  const related = new Set<string>([issue.id]);
  const parentId = issue.parent_issue_id;

  for (const other of allIssues) {
    if (parentId && other.id === parentId) {
      related.add(other.id);
    }
    if (parentId && other.parent_issue_id === parentId) {
      related.add(other.id);
    }
    if (other.parent_issue_id === issue.id) {
      related.add(other.id);
    }
  }

  return related;
}
