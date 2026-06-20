/**
 * Adapter between RuleGo DSL JSON (stored in DB / passed to engine)
 * and React Flow's { nodes, edges } format (rendered on canvas).
 *
 * This is a pure rendering adapter — no format conversion at the data level.
 * The DB stores RuleGo DSL, the engine executes RuleGo DSL, and this adapter
 * maps it to/from React Flow's component model for the editor UI.
 */

import { Position, type Node, type Edge } from '@xyflow/react';

// ── RuleGo DSL types (matches api/types/dsl.go) ──

export interface RuleGoChain {
  ruleChain: RuleGoChainInfo;
  metadata: RuleGoMetadata;
}

export interface RuleGoChainInfo {
  id: string;
  name: string;
  debugMode?: boolean;
  root?: boolean;
  disabled?: boolean;
  configuration?: Record<string, unknown>;
  additionalInfo?: Record<string, unknown>;
}

export interface RuleGoMetadata {
  firstNodeIndex: number;
  nodes: RuleGoNode[];
  connections: RuleGoConnection[];
}

export interface RuleGoNode {
  id: string;
  type: string;
  name: string;
  debugMode?: boolean;
  configuration: Record<string, unknown>;
  additionalInfo?: {
    layoutX?: number;
    layoutY?: number;
    description?: string;
    [key: string]: unknown;
  };
}

export interface RuleGoConnection {
  fromId: string;
  toId: string;
  type: string; // "Success" | "True" | "False" | custom
  label?: string;
}

// ── React Flow node data ──

export interface SOPNodeData {
  label: string;
  nodeType: string;
  configuration: Record<string, unknown>;
  debugMode?: boolean;
  description?: string;
  [key: string]: unknown;
}

// ── Conversion: RuleGo DSL → React Flow ──

export function toReactFlow(chain: RuleGoChain): { nodes: Node<SOPNodeData>[]; edges: Edge[] } {
  const nodes: Node<SOPNodeData>[] = (chain.metadata.nodes ?? []).map((n) => ({
    id: n.id,
    type: 'default',
    sourcePosition: Position.Bottom,
    targetPosition: Position.Top,
    position: {
      x: n.additionalInfo?.layoutX ?? 0,
      y: n.additionalInfo?.layoutY ?? 0,
    },
    data: {
      label: n.name,
      nodeType: n.type,
      configuration: n.configuration ?? {},
      debugMode: n.debugMode,
      description: n.additionalInfo?.description,
    },
  }));

  const edges: Edge[] = (chain.metadata.connections ?? []).map((c, i) => ({
    id: `e-${c.fromId}-${c.toId}-${i}`,
    source: c.fromId,
    target: c.toId,
    type: 'smoothstep',
    animated: true,
    data: { connectionType: c.type },
  }));

  return { nodes, edges };
}

// ── Conversion: React Flow → RuleGo DSL ──

export function toRuleGoDSL(
  nodes: Node<SOPNodeData>[],
  edges: Edge[],
  chainInfo: RuleGoChainInfo,
): RuleGoChain {
  return {
    ruleChain: chainInfo,
    metadata: {
      firstNodeIndex: 0,
      nodes: nodes.map((n) => ({
        id: n.id,
        type: n.data.nodeType,
        name: n.data.label,
        debugMode: n.data.debugMode ?? false,
        configuration: n.data.configuration ?? {},
        additionalInfo: {
          layoutX: Math.round(n.position.x),
          layoutY: Math.round(n.position.y),
          description: n.data.description,
        },
      })),
      connections: edges.map((e) => ({
        fromId: e.source,
        toId: e.target,
        type: (e.data as Record<string, unknown>)?.connectionType as string || 'Success',
        label: typeof e.label === 'string' ? e.label : undefined,
      })),
    },
  };
}

// ── Empty DSL template ──

export function emptyRuleGoChain(name: string = ''): RuleGoChain {
  return {
    ruleChain: { id: '', name },
    metadata: { firstNodeIndex: 0, nodes: [], connections: [] },
  };
}

// All nodes use React Flow's built-in "default" type. Custom node
// components (with per-type visuals) can be registered later via
// ReactFlow's `nodeTypes` prop; until then "default" avoids the
// "Node type not found" warning.

// ── Node palette definition ──

export interface NodePaletteItem {
  type: string;       // RuleGo node type
  label: string;
  icon: string;       // emoji
  category: 'control' | 'ai' | 'data' | 'document' | 'integration';
}

export const NODE_PALETTE: NodePaletteItem[] = [
  // AI
  { type: 'agentLLM', label: 'LLM (Agent)', icon: '🤖', category: 'ai' },
  // Data
  { type: 'restApiCall', label: 'HTTP Request', icon: '🌐', category: 'data' },
  { type: 'jsTransform', label: 'Code / Transform', icon: '📝', category: 'data' },
  { type: 'jsFilter', label: 'Condition', icon: '🔀', category: 'control' },
  // Document
  { type: 'docGenerate', label: 'Document Gen', icon: '📄', category: 'document' },
  { type: 'spreadsheet', label: 'Spreadsheet', icon: '📊', category: 'document' },
  // Integration
  { type: 'sendEmail', label: 'Send Email', icon: '📧', category: 'integration' },
  { type: 'webScrape', label: 'Web Scraper', icon: '🕷️', category: 'integration' },
  { type: 'rssFetch', label: 'RSS Feed', icon: '📰', category: 'integration' },
  { type: 'dbClient', label: 'Database', icon: '🗄️', category: 'integration' },
];
