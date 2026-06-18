"use client";

import { useState, useMemo, useCallback, type MouseEvent } from "react";
import type { Issue, IssueDependency } from "@multica/core/types";
import { DagCanvas } from "./dag-canvas";
import { DagEdges } from "./dag-edges";
import { DagNodeCard } from "./dag-node-card";
import { useDagLayout } from "./use-dagre-layout";
import { computeCriticalPath, getRelatedIds } from "./dag-graph-utils";

interface DagGraphProps {
  issues: Issue[];
  edges: IssueDependency[];
  /** Issue IDs that have children and can be expanded */
  expandableIds: Set<string>;
  /** Issue IDs currently expanded */
  expandedIds: Set<string>;
  onToggleExpand: (issueId: string) => void;
  onNodeClick?: (issueId: string) => void;
  onExpandAll: () => void;
  onCollapseAll: () => void;
}

export function DagGraph({
  issues,
  edges,
  expandableIds,
  expandedIds,
  onToggleExpand,
  onNodeClick,
  onExpandAll,
  onCollapseAll,
}: DagGraphProps) {
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null);

  const nodeIds = useMemo(() => issues.map((i) => i.id), [issues]);
  const issueMap = useMemo(() => {
    const m = new Map<string, Issue>();
    for (const i of issues) m.set(i.id, i);
    return m;
  }, [issues]);

  const layout = useDagLayout(nodeIds, edges);

  const { criticalEdgeIds } = useMemo(
    () => computeCriticalPath(issues, edges),
    [issues, edges],
  );

  const hoverRelated = useMemo(() => {
    if (!hoveredId) return null;
    return getRelatedIds(hoveredId, edges);
  }, [hoveredId, edges]);

  const highlightedEdgeIds = useMemo(() => {
    if (!hoverRelated) return undefined;
    return hoverRelated.relatedEdgeIds;
  }, [hoverRelated]);

  const isNodeHighlighted = useCallback(
    (id: string): boolean => {
      if (!hoveredId) return false;
      if (id === hoveredId) return true;
      if (!hoverRelated) return false;
      return hoverRelated.upstreamIds.has(id) || hoverRelated.downstreamIds.has(id);
    },
    [hoveredId, hoverRelated],
  );

  const isNodeDimmed = useCallback(
    (id: string): boolean => {
      if (!hoveredId) return false;
      return !isNodeHighlighted(id);
    },
    [hoveredId, isNodeHighlighted],
  );

  const handleContextMenu = useCallback((e: MouseEvent<SVGSVGElement>) => {
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY });
  }, []);

  const closeContextMenu = useCallback(() => {
    setContextMenu(null);
  }, []);

  if (issues.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center text-muted-foreground text-sm">
        No issues to display
      </div>
    );
  }

  return (
    <div className="relative flex flex-col flex-1 min-h-0" onClick={closeContextMenu}>
      <DagCanvas
        width={layout.width}
        height={layout.height}
        onContextMenu={handleContextMenu}
      >
        <DagEdges
          edges={layout.edges}
          highlightedEdgeIds={highlightedEdgeIds}
          criticalPathEdgeIds={criticalEdgeIds}
        />
        {layout.nodes.map((node) => {
          const issue = issueMap.get(node.id);
          if (!issue) return null;
          const isExpandable = expandableIds.has(node.id);
          const isExpanded = expandedIds.has(node.id);
          return (
            <DagNodeCard
              key={node.id}
              node={node}
              issue={issue}
              highlighted={isNodeHighlighted(node.id)}
              dimmed={isNodeDimmed(node.id)}
              expandable={isExpandable}
              expanded={isExpanded}
              onToggleExpand={onToggleExpand}
              onClick={onNodeClick}
              onMouseEnter={setHoveredId}
              onMouseLeave={() => setHoveredId(null)}
            />
          );
        })}
      </DagCanvas>

      {/* Right-click context menu */}
      {contextMenu && (
        <div
          className="fixed z-50 min-w-36 rounded-md border bg-popover p-1 shadow-md"
          style={{ left: contextMenu.x, top: contextMenu.y }}
        >
          <button
            className="flex w-full items-center rounded-sm px-2 py-1.5 text-sm hover:bg-accent"
            onClick={() => { onExpandAll(); closeContextMenu(); }}
          >
            Expand All
          </button>
          <button
            className="flex w-full items-center rounded-sm px-2 py-1.5 text-sm hover:bg-accent"
            onClick={() => { onCollapseAll(); closeContextMenu(); }}
          >
            Collapse All
          </button>
        </div>
      )}
    </div>
  );
}
