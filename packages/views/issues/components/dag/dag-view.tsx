"use client";

import { useState, useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../../navigation";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import type { Issue, IssueDependency } from "@multica/core/types";
import { DagGraph } from "./dag-graph";
import { useT } from "../../../i18n";

interface DagViewProps {
  projectId: string;
}

export function DagView({ projectId }: DagViewProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();

  const { t } = useT("issues");
  // Set of parent issue IDs whose children are currently expanded
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());

  const { data, isLoading, error } = useQuery({
    queryKey: ["project-dependency-graph", wsId, projectId],
    queryFn: () => api.getProjectDependencyGraph(projectId),
  });

  // Identify which issues are root (no parent) and which have children
  const { rootIds, childrenByParent, parentIssueIds } = useMemo(() => {
    if (!data) return { rootIds: new Set<string>(), childrenByParent: new Map<string, string[]>(), parentIssueIds: new Set<string>() };
    const roots = new Set<string>();
    const cMap = new Map<string, string[]>();
    const pIds = new Set<string>();

    for (const node of data.nodes) {
      if (!node.parent_issue_id) {
        roots.add(node.id);
      } else {
        pIds.add(node.parent_issue_id);
        const arr = cMap.get(node.parent_issue_id);
        if (arr) arr.push(node.id);
        else cMap.set(node.parent_issue_id, [node.id]);
      }
    }
    return { rootIds: roots, childrenByParent: cMap, parentIssueIds: pIds };
  }, [data]);

  // Compute visible nodes: roots + children of expanded nodes (recursively)
  const { visibleIssues, visibleEdges } = useMemo(() => {
    if (!data) return { visibleIssues: [] as Issue[], visibleEdges: [] as IssueDependency[] };

    const visibleSet = new Set<string>();

    function addVisible(id: string) {
      visibleSet.add(id);
      if (expandedIds.has(id)) {
        const children = childrenByParent.get(id);
        if (children) {
          for (const childId of children) {
            addVisible(childId);
          }
        }
      }
    }

    for (const rootId of rootIds) {
      addVisible(rootId);
    }

    const issues = data.nodes.filter((n) => visibleSet.has(n.id));
    const edges = data.edges.filter(
      (e) => visibleSet.has(e.issue_id) && visibleSet.has(e.depends_on_issue_id),
    );

    return { visibleIssues: issues, visibleEdges: edges };
  }, [data, expandedIds, rootIds, childrenByParent]);

  const toggleExpand = useCallback((issueId: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(issueId)) next.delete(issueId);
      else next.add(issueId);
      return next;
    });
  }, []);

  const expandAll = useCallback(() => {
    setExpandedIds(new Set(parentIssueIds));
  }, [parentIssueIds]);

  const collapseAll = useCallback(() => {
    setExpandedIds(new Set());
  }, []);

  const handleNodeClick = useCallback(
    (issueId: string) => {
      push(paths.issueDetail(issueId));
    },
    [push, paths],
  );

  if (isLoading) {
    return (
      <div className="flex-1 p-4">
        <Skeleton className="h-full w-full rounded-lg" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-destructive">
        {t(($) => $.dag_view.load_error)}
      </div>
    );
  }

  return (
    <DagGraph
      issues={visibleIssues}
      edges={visibleEdges}
      expandableIds={parentIssueIds}
      expandedIds={expandedIds}
      onToggleExpand={toggleExpand}
      onNodeClick={handleNodeClick}
      onExpandAll={expandAll}
      onCollapseAll={collapseAll}
    />
  );
}
