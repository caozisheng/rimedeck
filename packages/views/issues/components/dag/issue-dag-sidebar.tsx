"use client";

import { useMemo, useState, useCallback, useRef, type WheelEvent, type PointerEvent } from "react";
import dagre from "@dagrejs/dagre";
import { useQuery } from "@tanstack/react-query";
import { api } from "@rimedeck/core/api";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { useWorkspacePaths } from "@rimedeck/core/paths";
import { useNavigation } from "../../../navigation";
import { useActorName } from "@rimedeck/core/workspace/hooks";
import type { Issue, IssueDependency, IssueStatus } from "@rimedeck/core/types";
import { StatusIcon } from "../status-icon";
import { useT } from "../../../i18n";

// ---------------------------------------------------------------------------
// Compact vertical dagre layout
// ---------------------------------------------------------------------------

const NODE_W = 180;
const NODE_H = 48;

interface MiniNode { id: string; x: number; y: number }
interface MiniEdge { id: string; from: string; to: string; depType: string; points: { x: number; y: number }[] }

function layoutVertical(nodeIds: string[], edges: IssueDependency[]) {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: "TB", ranksep: 36, nodesep: 16, marginx: 12, marginy: 12 });
  g.setDefaultEdgeLabel(() => ({}));

  const nodeSet = new Set(nodeIds);
  for (const id of nodeIds) g.setNode(id, { width: NODE_W, height: NODE_H });
  for (const e of edges) {
    if (nodeSet.has(e.issue_id) && nodeSet.has(e.depends_on_issue_id))
      g.setEdge(e.issue_id, e.depends_on_issue_id, { id: e.id });
  }
  dagre.layout(g);

  const nodes: MiniNode[] = nodeIds.map((id) => {
    const n = g.node(id);
    return { id, x: n.x - NODE_W / 2, y: n.y - NODE_H / 2 };
  });
  const miniEdges: MiniEdge[] = edges
    .filter((e) => nodeSet.has(e.issue_id) && nodeSet.has(e.depends_on_issue_id))
    .map((e) => {
      const ed = g.edge(e.issue_id, e.depends_on_issue_id);
      return { id: e.id, from: e.issue_id, to: e.depends_on_issue_id, depType: e.dep_type, points: ed?.points ?? [] };
    });

  const graph = g.graph();
  return { nodes, edges: miniEdges, width: (graph.width ?? 220) + 24, height: (graph.height ?? 200) + 24 };
}

// ---------------------------------------------------------------------------
// Edge SVG
// ---------------------------------------------------------------------------

function MiniEdges({ edges }: { edges: MiniEdge[] }) {
  return (
    <g>
      <defs>
        <marker id="dag-mini-arrow" viewBox="0 0 8 6" refX="8" refY="3" markerWidth="6" markerHeight="5" orient="auto-start-reverse">
          <path d="M 0 0 L 8 3 L 0 6 z" className="fill-muted-foreground/40" />
        </marker>
      </defs>
      {edges.map((e) => {
        const pts = e.points;
        if (pts.length === 0) return null;
        let d = `M ${pts[0]!.x} ${pts[0]!.y}`;
        for (let i = 1; i < pts.length; i++) d += ` L ${pts[i]!.x} ${pts[i]!.y}`;
        const isParent = e.depType === "parent";
        return (
          <path
            key={e.id}
            d={d}
            fill="none"
            className={isParent ? "stroke-purple-400/60" : "stroke-muted-foreground/30"}
            strokeWidth={1.5}
            strokeDasharray={isParent ? "5 3" : e.depType === "relates_to" ? "3 3" : undefined}
            markerEnd={e.depType !== "relates_to" ? "url(#dag-mini-arrow)" : undefined}
          />
        );
      })}
    </g>
  );
}

// ---------------------------------------------------------------------------
// Mini node card
// ---------------------------------------------------------------------------

const STATUS_BORDER: Record<IssueStatus, string> = {
  backlog: "border-muted-foreground/20",
  todo: "border-muted-foreground/40",
  in_progress: "border-blue-500",
  in_review: "border-yellow-500",
  done: "border-green-500/80",
  blocked: "border-destructive",
  cancelled: "border-muted-foreground/20",
};

function MiniNodeCard({
  node, issue, isCurrent, onClick,
}: {
  node: MiniNode; issue: Issue; isCurrent: boolean; onClick: (id: string) => void;
}) {
  const { getActorName } = useActorName();
  const name = issue.assignee_type && issue.assignee_id
    ? getActorName(issue.assignee_type, issue.assignee_id) : null;

  return (
    <foreignObject x={node.x} y={node.y} width={NODE_W} height={NODE_H}>
      <button
        type="button"
        className={`
          dag-node-card w-full h-full rounded-md bg-card px-2 py-1 text-left text-[10px] leading-tight
          transition-all select-none truncate
          ${isCurrent
            ? "border-[3px] border-red-500 shadow-lg shadow-red-500/20"
            : `border ${STATUS_BORDER[issue.status] ?? "border-border"} hover:shadow-sm`
          }
        `}
        onClick={(e) => { e.stopPropagation(); onClick(issue.id); }}
        onPointerDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-1 mb-0.5">
          <StatusIcon status={issue.status} className="size-3 shrink-0" />
          <span className="font-mono text-muted-foreground shrink-0">{issue.identifier}</span>
          <span className="font-medium truncate">{issue.title}</span>
        </div>
        {name && (
          <div className="text-muted-foreground truncate">
            {issue.assignee_type === "agent" ? "🤖" : "👤"} {name}
          </div>
        )}
      </button>
    </foreignObject>
  );
}

// ---------------------------------------------------------------------------
// Zoom/pan canvas
// ---------------------------------------------------------------------------

function MiniCanvas({ children }: { children: React.ReactNode }) {
  const [transform, setTransform] = useState({ x: 0, y: 0, scale: 1 });
  const dragRef = useRef<{ startX: number; startY: number; origX: number; origY: number } | null>(null);

  const handleWheel = useCallback((e: WheelEvent<SVGSVGElement>) => {
    e.preventDefault();
    const delta = -e.deltaY * 0.001;
    setTransform((prev) => {
      const newScale = Math.min(2, Math.max(0.3, prev.scale + delta));
      const rect = (e.target as Element).closest("svg")?.getBoundingClientRect();
      if (!rect) return { ...prev, scale: newScale };
      const cx = e.clientX - rect.left;
      const cy = e.clientY - rect.top;
      const ratio = newScale / prev.scale;
      return { x: cx - ratio * (cx - prev.x), y: cy - ratio * (cy - prev.y), scale: newScale };
    });
  }, []);

  const handlePointerDown = useCallback((e: PointerEvent<SVGSVGElement>) => {
    if (e.button !== 0 && e.button !== 1) return;
    if ((e.target as Element).closest(".dag-node-card")) return;
    dragRef.current = { startX: e.clientX, startY: e.clientY, origX: transform.x, origY: transform.y };
    (e.target as Element).closest("svg")?.setPointerCapture(e.pointerId);
  }, [transform.x, transform.y]);

  const handlePointerMove = useCallback((e: PointerEvent<SVGSVGElement>) => {
    const drag = dragRef.current;
    if (!drag) return;
    setTransform((prev) => ({ ...prev, x: drag.origX + e.clientX - drag.startX, y: drag.origY + e.clientY - drag.startY }));
  }, []);

  const handlePointerUp = useCallback(() => { dragRef.current = null; }, []);

  return (
    <svg
      width="100%" height="100%"
      style={{ display: "block", minHeight: 0 }}
      className="cursor-grab active:cursor-grabbing"
      onWheel={handleWheel}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
    >
      <g transform={`translate(${transform.x}, ${transform.y}) scale(${transform.scale})`}>
        {children}
      </g>
    </svg>
  );
}

// ---------------------------------------------------------------------------
// Scoped DAG: only show the sub-tree containing the current issue
// ---------------------------------------------------------------------------

function scopeToIssueDag(
  currentId: string,
  allNodes: Issue[],
  allEdges: IssueDependency[],
): { nodes: Issue[]; edges: IssueDependency[] } {
  const nodeMap = new Map<string, Issue>();
  for (const n of allNodes) nodeMap.set(n.id, n);

  const current = nodeMap.get(currentId);
  if (!current) return { nodes: [], edges: [] };

  // Find the root of the current issue's tree
  let rootId = currentId;
  if (current.parent_issue_id) {
    rootId = current.parent_issue_id;
    // Walk up further if grandparent exists
    const parent = nodeMap.get(rootId);
    if (parent?.parent_issue_id) rootId = parent.parent_issue_id;
  }

  // Collect root + all descendants
  const scopeIds = new Set<string>();
  const queue = [rootId];
  let head = 0;
  scopeIds.add(rootId);
  while (head < queue.length) {
    const id = queue[head++]!;
    for (const n of allNodes) {
      if (n.parent_issue_id === id && !scopeIds.has(n.id)) {
        scopeIds.add(n.id);
        queue.push(n.id);
      }
    }
  }

  // Also add siblings if current is a root-level issue (show peer roots with deps)
  if (!current.parent_issue_id) {
    // Include issues connected via explicit deps
    for (const e of allEdges) {
      if (e.dep_type !== "parent") {
        if (scopeIds.has(e.issue_id)) scopeIds.add(e.depends_on_issue_id);
        if (scopeIds.has(e.depends_on_issue_id)) scopeIds.add(e.issue_id);
      }
    }
  }

  const nodes = allNodes.filter((n) => scopeIds.has(n.id));
  const edges = allEdges.filter((e) => scopeIds.has(e.issue_id) && scopeIds.has(e.depends_on_issue_id));
  return { nodes, edges };
}

// ---------------------------------------------------------------------------
// Public component
// ---------------------------------------------------------------------------

interface IssueDagSidebarProps {
  issue: Issue;
}

export function IssueDagSidebar({ issue }: IssueDagSidebarProps) {
  const wsId = useWorkspaceId();
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const { push } = useNavigation();

  const projectId = issue.project_id;
  const { data } = useQuery({
    queryKey: ["project-dependency-graph", wsId, projectId],
    queryFn: () => api.getProjectDependencyGraph(projectId!),
    enabled: !!projectId,
  });

  // Scope to the current issue's sub-tree
  const scoped = useMemo(() => {
    if (!data) return null;
    return scopeToIssueDag(issue.id, data.nodes, data.edges);
  }, [data, issue.id]);

  const layout = useMemo(() => {
    if (!scoped || scoped.nodes.length === 0) return null;
    return layoutVertical(scoped.nodes.map((n) => n.id), scoped.edges);
  }, [scoped]);

  const issueMap = useMemo(() => {
    if (!scoped) return new Map<string, Issue>();
    const m = new Map<string, Issue>();
    for (const n of scoped.nodes) m.set(n.id, n);
    return m;
  }, [scoped]);

  if (!projectId || !layout || !scoped || scoped.nodes.length === 0) return null;

  return (
    <div className="flex flex-col h-full border-r bg-muted/10 shrink-0" style={{ width: 220, minWidth: 160, maxWidth: 400, resize: "horizontal", overflow: "hidden" }}>
      <div className="px-3 py-2 border-b shrink-0">
        <span className="text-xs font-medium text-muted-foreground">{t(($) => $.dag_view.nav_title)}</span>
      </div>
      <div className="flex-1 min-h-0 overflow-hidden">
        <MiniCanvas>
          <MiniEdges edges={layout.edges} />
          {layout.nodes.map((node) => {
            const iss = issueMap.get(node.id);
            if (!iss) return null;
            return (
              <MiniNodeCard
                key={node.id}
                node={node}
                issue={iss}
                isCurrent={node.id === issue.id}
                onClick={(id) => { if (id !== issue.id) push(paths.issueDetail(id)); }}
              />
            );
          })}
        </MiniCanvas>
      </div>
    </div>
  );
}
