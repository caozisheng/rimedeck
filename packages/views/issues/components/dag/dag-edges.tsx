import type { DagEdge } from "./use-dagre-layout";

function pointsToPath(points: { x: number; y: number }[]): string {
  if (points.length === 0) return "";
  const [first, ...rest] = points;
  let d = `M ${first!.x} ${first!.y}`;
  if (rest.length >= 2) {
    // Smooth curve through intermediate points
    for (let i = 0; i < rest.length - 1; i++) {
      const curr = rest[i]!;
      const next = rest[i + 1]!;
      const midX = (curr.x + next.x) / 2;
      const midY = (curr.y + next.y) / 2;
      d += ` Q ${curr.x} ${curr.y} ${midX} ${midY}`;
    }
    const last = rest[rest.length - 1]!;
    d += ` L ${last.x} ${last.y}`;
  } else if (rest.length === 1) {
    d += ` L ${rest[0]!.x} ${rest[0]!.y}`;
  }
  return d;
}

interface DagEdgesProps {
  edges: DagEdge[];
  highlightedEdgeIds?: Set<string>;
  criticalPathEdgeIds?: Set<string>;
}

export function DagEdges({ edges, highlightedEdgeIds, criticalPathEdgeIds }: DagEdgesProps) {
  return (
    <g className="dag-edges">
      <defs>
        <marker
          id="dag-arrow"
          viewBox="0 0 10 8"
          refX="10"
          refY="4"
          markerWidth="8"
          markerHeight="6"
          orient="auto-start-reverse"
        >
          <path d="M 0 0 L 10 4 L 0 8 z" className="fill-muted-foreground/50" />
        </marker>
        <marker
          id="dag-arrow-critical"
          viewBox="0 0 10 8"
          refX="10"
          refY="4"
          markerWidth="8"
          markerHeight="6"
          orient="auto-start-reverse"
        >
          <path d="M 0 0 L 10 4 L 0 8 z" className="fill-blue-500" />
        </marker>
        <marker
          id="dag-arrow-blocked"
          viewBox="0 0 10 8"
          refX="10"
          refY="4"
          markerWidth="8"
          markerHeight="6"
          orient="auto-start-reverse"
        >
          <path d="M 0 0 L 10 4 L 0 8 z" className="fill-destructive" />
        </marker>
      </defs>

      {edges.map((edge) => {
        const isCritical = criticalPathEdgeIds?.has(edge.id);
        const isHighlighted = highlightedEdgeIds?.has(edge.id);
        const isRelatesTo = edge.depType === "relates_to";
        const isParent = edge.depType === "parent";
        const dimmed = highlightedEdgeIds && highlightedEdgeIds.size > 0 && !isHighlighted;

        const strokeClass = isCritical
          ? "stroke-blue-500"
          : isParent
          ? "stroke-purple-400"
          : isHighlighted
          ? "stroke-destructive"
          : "stroke-muted-foreground/40";

        const markerEnd = isRelatesTo
          ? undefined
          : isCritical
          ? "url(#dag-arrow-critical)"
          : isHighlighted
          ? "url(#dag-arrow-blocked)"
          : "url(#dag-arrow)";

        return (
          <path
            key={edge.id}
            d={pointsToPath(edge.points)}
            fill="none"
            className={strokeClass}
            strokeWidth={isCritical ? 2.5 : isParent ? 2 : 1.5}
            strokeDasharray={isRelatesTo ? "6 4" : isParent ? "8 3" : undefined}
            markerEnd={markerEnd}
            opacity={dimmed ? 0.15 : 1}
          />
        );
      })}
    </g>
  );
}
