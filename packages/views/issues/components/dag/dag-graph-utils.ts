import type { Issue, IssueDependency } from "@multica/core/types";

/**
 * Compute the critical path (longest chain of `blocks` edges) through the DAG.
 * Returns the set of edge IDs on the critical path and the set of issue IDs
 * reachable from blocked nodes (blocked chain).
 */
export function computeCriticalPath(
  issues: Issue[],
  edges: IssueDependency[],
): { criticalEdgeIds: Set<string>; blockedChainIds: Set<string> } {
  // Only consider "blocks" edges for critical path
  const blockEdges = edges.filter((e) => e.dep_type === "blocks");
  const issueSet = new Set(issues.map((i) => i.id));

  // Build adjacency: from -> [{ to, edgeId }]
  const adj = new Map<string, { to: string; edgeId: string }[]>();
  const inDegree = new Map<string, number>();

  for (const id of issueSet) {
    adj.set(id, []);
    inDegree.set(id, 0);
  }

  for (const e of blockEdges) {
    if (!issueSet.has(e.issue_id) || !issueSet.has(e.depends_on_issue_id)) continue;
    adj.get(e.issue_id)!.push({ to: e.depends_on_issue_id, edgeId: e.id });
    inDegree.set(e.depends_on_issue_id, (inDegree.get(e.depends_on_issue_id) ?? 0) + 1);
  }

  // Topological sort + longest path via DP
  const dist = new Map<string, number>();
  const prevEdge = new Map<string, string>(); // nodeId -> edgeId that leads to it on longest path
  const prevNode = new Map<string, string>(); // nodeId -> preceding nodeId on longest path

  const queue: string[] = [];
  for (const [id, deg] of inDegree) {
    dist.set(id, 0);
    if (deg === 0) queue.push(id);
  }

  // Kahn's algorithm with longest path relaxation
  let head = 0;
  while (head < queue.length) {
    const u = queue[head++]!;
    const uDist = dist.get(u) ?? 0;
    for (const { to, edgeId } of adj.get(u) ?? []) {
      const newDist = uDist + 1;
      if (newDist > (dist.get(to) ?? 0)) {
        dist.set(to, newDist);
        prevEdge.set(to, edgeId);
        prevNode.set(to, u);
      }
      const newDeg = (inDegree.get(to) ?? 1) - 1;
      inDegree.set(to, newDeg);
      if (newDeg === 0) queue.push(to);
    }
  }

  // Find the node with the longest distance — that's the tail of the critical path
  let maxDist = 0;
  let tailNode = "";
  for (const [id, d] of dist) {
    if (d > maxDist) {
      maxDist = d;
      tailNode = id;
    }
  }

  // Walk back to collect critical path edge IDs
  const criticalEdgeIds = new Set<string>();
  let cur = tailNode;
  while (prevEdge.has(cur)) {
    criticalEdgeIds.add(prevEdge.get(cur)!);
    cur = prevNode.get(cur)!;
  }

  // Blocked chain: find all blocked issues and trace their downstream
  const blockedIssues = issues.filter((i) => i.status === "blocked");
  const blockedChainIds = new Set<string>();

  for (const blocked of blockedIssues) {
    // BFS downstream from blocked node
    const visited = new Set<string>();
    const bfsQueue = [blocked.id];
    let bfsHead = 0;
    while (bfsHead < bfsQueue.length) {
      const node = bfsQueue[bfsHead++]!;
      if (visited.has(node)) continue;
      visited.add(node);
      blockedChainIds.add(node);
      for (const { to } of adj.get(node) ?? []) {
        if (!visited.has(to)) bfsQueue.push(to);
      }
    }
  }

  // Also add edges on blocked chains
  for (const e of blockEdges) {
    if (blockedChainIds.has(e.issue_id) && blockedChainIds.has(e.depends_on_issue_id)) {
      // Edge IDs on blocked path also go into the highlight set
    }
  }

  return { criticalEdgeIds, blockedChainIds };
}

/**
 * Get upstream and downstream issue IDs for a given focus issue.
 * Used for hover highlighting.
 */
export function getRelatedIds(
  focusId: string,
  edges: IssueDependency[],
): { upstreamIds: Set<string>; downstreamIds: Set<string>; relatedEdgeIds: Set<string> } {
  const upstream = new Map<string, IssueDependency[]>(); // to -> edges pointing to it
  const downstream = new Map<string, IssueDependency[]>(); // from -> edges from it

  for (const e of edges) {
    if (!downstream.has(e.issue_id)) downstream.set(e.issue_id, []);
    downstream.get(e.issue_id)!.push(e);
    if (!upstream.has(e.depends_on_issue_id)) upstream.set(e.depends_on_issue_id, []);
    upstream.get(e.depends_on_issue_id)!.push(e);
  }

  const upstreamIds = new Set<string>();
  const relatedEdgeIds = new Set<string>();

  // Walk upstream (who blocks me?)
  const upQueue = [focusId];
  let uHead = 0;
  while (uHead < upQueue.length) {
    const node = upQueue[uHead++]!;
    for (const e of upstream.get(node) ?? []) {
      relatedEdgeIds.add(e.id);
      if (!upstreamIds.has(e.issue_id)) {
        upstreamIds.add(e.issue_id);
        upQueue.push(e.issue_id);
      }
    }
  }

  // Walk downstream (who do I block?)
  const downstreamIds = new Set<string>();
  const downQueue = [focusId];
  let dHead = 0;
  while (dHead < downQueue.length) {
    const node = downQueue[dHead++]!;
    for (const e of downstream.get(node) ?? []) {
      relatedEdgeIds.add(e.id);
      if (!downstreamIds.has(e.depends_on_issue_id)) {
        downstreamIds.add(e.depends_on_issue_id);
        downQueue.push(e.depends_on_issue_id);
      }
    }
  }

  return { upstreamIds, downstreamIds, relatedEdgeIds };
}
