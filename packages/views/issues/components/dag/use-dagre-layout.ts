import { useMemo } from "react";
import dagre from "@dagrejs/dagre";
import type { IssueDependency } from "@multica/core/types";

export interface DagNode {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface DagEdge {
  id: string;
  from: string;
  to: string;
  depType: "blocks" | "relates_to" | "parent";
  points: { x: number; y: number }[];
}

export interface DagLayout {
  nodes: DagNode[];
  edges: DagEdge[];
  width: number;
  height: number;
}

const NODE_WIDTH = 220;
const NODE_HEIGHT = 100;
const RANK_SEP = 80;
const NODE_SEP = 40;

/**
 * Computes a dagre layout for the given nodes and edges.
 * Returns positioned nodes and routed edges.
 */
export function computeDagLayout(
  nodeIds: string[],
  edges: IssueDependency[],
): DagLayout {
  const g = new dagre.graphlib.Graph();
  g.setGraph({
    rankdir: "LR",
    ranksep: RANK_SEP,
    nodesep: NODE_SEP,
    marginx: 20,
    marginy: 20,
  });
  g.setDefaultEdgeLabel(() => ({}));

  const nodeSet = new Set(nodeIds);
  for (const id of nodeIds) {
    g.setNode(id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  }

  for (const edge of edges) {
    if (nodeSet.has(edge.issue_id) && nodeSet.has(edge.depends_on_issue_id)) {
      g.setEdge(edge.issue_id, edge.depends_on_issue_id, { id: edge.id });
    }
  }

  dagre.layout(g);

  const nodes: DagNode[] = nodeIds.map((id) => {
    const n = g.node(id);
    return {
      id,
      x: n.x - NODE_WIDTH / 2,
      y: n.y - NODE_HEIGHT / 2,
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
    };
  });

  const dagEdges: DagEdge[] = edges
    .filter((e) => nodeSet.has(e.issue_id) && nodeSet.has(e.depends_on_issue_id))
    .map((e) => {
      const edgeData = g.edge(e.issue_id, e.depends_on_issue_id);
      return {
        id: e.id,
        from: e.issue_id,
        to: e.depends_on_issue_id,
        depType: e.dep_type,
        points: edgeData?.points ?? [],
      };
    });

  const graphData = g.graph();

  return {
    nodes,
    edges: dagEdges,
    width: (graphData.width ?? 800) + 40,
    height: (graphData.height ?? 600) + 40,
  };
}

export function useDagLayout(nodeIds: string[], edges: IssueDependency[]) {
  return useMemo(
    () => computeDagLayout(nodeIds, edges),
    [nodeIds, edges],
  );
}
