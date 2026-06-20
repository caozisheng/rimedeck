'use client';

import { useCallback, useMemo, useState } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  Position,
  addEdge,
  useNodesState,
  useEdgesState,
  type Connection,
  type Node,
  Panel,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import './workflow-canvas.css';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useWorkspaceId } from '@multica/core/hooks';
import { agentListOptions, workflowRunKeys } from '@multica/core/workspace/queries';
import { api } from '@multica/core/api';
import type { Agent } from '@multica/core/types';
import { Button } from '@multica/ui/components/ui/button';
import { useT } from '../../i18n';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@multica/ui/components/ui/collapsible';
import { ChevronDown, ChevronRight, Loader2, Play } from 'lucide-react';
import { toast } from 'sonner';

import {
  toReactFlow,
  toRuleGoDSL,
  NODE_PALETTE,
  type RuleGoChain,
  type RuleGoChainInfo,
  type WorkflowNodeData,
} from '../utils/rulego-adapter';
import { RunMonitor } from './run-monitor';
import { RunHistory } from './run-history';

interface WorkflowEditorProps {
  workflowId: string;
  graph: RuleGoChain;
  chainInfo: RuleGoChainInfo;
  onChange?: (graph: RuleGoChain) => void | Promise<void>;
  readOnly?: boolean;
}

let nodeIdCounter = 0;

export function WorkflowEditor({
  workflowId,
  graph,
  chainInfo,
  onChange,
  readOnly,
}: WorkflowEditorProps) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { t } = useT('workflows');
  const initial = useMemo(() => toReactFlow(graph), [graph]);
  const [nodes, setNodes, onNodesChange] = useNodesState(initial.nodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initial.edges);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const [triggering, setTriggering] = useState(false);
  const [testAgentId, setTestAgentId] = useState('');

  // Load workspace agents for test-run picker.
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const activeAgents = useMemo(() => agents.filter((a: Agent) => !a.archived_at), [agents]);

  const onConnect = useCallback(
    (connection: Connection) => {
      setEdges((eds) => addEdge({ ...connection, type: 'smoothstep', animated: true, data: { connectionType: 'Success' } }, eds));
    },
    [setEdges],
  );

  // Persist changes back to RuleGo DSL.
  const handleSave = useCallback(() => {
    const dsl = toRuleGoDSL(nodes as Node<WorkflowNodeData>[], edges, chainInfo);
    onChange?.(dsl);
  }, [nodes, edges, chainInfo, onChange]);

  // Add a new node from palette.
  const addNode = useCallback(
    (type: string, label: string) => {
      const id = `node_${Date.now()}_${++nodeIdCounter}`;
      setNodes((nds) => [
        ...nds,
        {
          id,
          type: 'default',
          sourcePosition: Position.Bottom,
          targetPosition: Position.Top,
          position: { x: 200 + Math.random() * 100, y: nds.length * 120 + 50 + Math.random() * 40 },
          data: { label, nodeType: type, configuration: {} },
        } as Node<WorkflowNodeData>,
      ]);
    },
    [setNodes],
  );

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    setSelectedNodeId(node.id);
  }, []);

  // Derive selected node from current nodes array so edits are reflected.
  const selectedNode = selectedNodeId
    ? (nodes.find((n) => n.id === selectedNodeId) as Node<WorkflowNodeData> | undefined) ?? null
    : null;

  const updateSelectedNode = useCallback(
    (updater: (data: WorkflowNodeData) => WorkflowNodeData) => {
      if (!selectedNodeId) return;
      setNodes((nds) =>
        nds.map((n) =>
          n.id === selectedNodeId ? { ...n, data: updater(n.data as WorkflowNodeData) } : n,
        ),
      );
    },
    [selectedNodeId, setNodes],
  );

  const handleTestRun = useCallback(async () => {
    if (!workflowId || triggering) return;
    setTriggering(true);
    try {
      // Auto-save before running so the server executes the latest graph.
      const dsl = toRuleGoDSL(nodes as Node<WorkflowNodeData>[], edges, chainInfo);
      await onChange?.(dsl);

      const run = await api.triggerWorkflowRun(workflowId, testAgentId || undefined);
      setActiveRunId(run.id);
      setHistoryOpen(true);
      qc.invalidateQueries({
        queryKey: workflowRunKeys.list(wsId, workflowId),
      });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(msg || t(($) => $.editor.test_run_failed));
    } finally {
      setTriggering(false);
    }
  }, [workflowId, triggering, testAgentId, nodes, edges, chainInfo, onChange, wsId, qc, t]);

  return (
    <div className="flex h-full w-full flex-col">
      {/* Editor area */}
      <div className="flex flex-1 min-h-0">
        {/* Left: Node Palette */}
        {!readOnly && (
          <div className="w-48 shrink-0 border-r bg-muted/30 p-3 overflow-y-auto">
            <h3 className="text-xs font-semibold text-muted-foreground mb-3 uppercase tracking-wider">
              {t(($) => $.editor.nodes)}
            </h3>
            <div className="space-y-1">
              {NODE_PALETTE.map((item) => (
                <button
                  key={item.type}
                  onClick={() => addNode(item.type, item.label)}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent text-left"
                >
                  <span>{item.icon}</span>
                  <span>{item.label}</span>
                </button>
              ))}
            </div>
          </div>
        )}

        {/* Center: Canvas */}
        <div className="flex-1 relative">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={readOnly ? undefined : onNodesChange}
            onEdgesChange={readOnly ? undefined : onEdgesChange}
            onConnect={readOnly ? undefined : onConnect}
            onNodeClick={onNodeClick}
            fitView
            deleteKeyCode={readOnly ? null : 'Delete'}
            defaultEdgeOptions={{ type: 'smoothstep', animated: true }}
            className="bg-background workflow-canvas"
          >
            <Background gap={16} size={1} />
            <Controls />
            <MiniMap className="!bg-muted" />
            <Panel position="top-right">
              <div className="flex items-center gap-2 rounded-lg bg-background/90 backdrop-blur-sm border p-1.5 shadow-sm">
                {/* Agent picker for test run */}
                <select
                  className="h-8 rounded-md border bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                  value={testAgentId}
                  onChange={(e) => setTestAgentId(e.target.value)}
                >
                  <option value="">{activeAgents.length > 0 ? t(($) => $.editor.select_agent) : t(($) => $.editor.no_agents)}</option>
                  {activeAgents.map((a: Agent) => (
                    <option key={a.id} value={a.id}>{a.name}</option>
                  ))}
                </select>
                <Button
                  size="sm"
                  variant="secondary"
                  className="gap-1.5"
                  disabled={triggering || activeAgents.length === 0}
                  onClick={handleTestRun}
                >
                  {triggering ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <Play className="size-3.5" />
                  )}
                  {t(($) => $.editor.test_run)}
                </Button>
                {!readOnly && (
                  <button
                    onClick={handleSave}
                    className="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90"
                  >
                    {t(($) => $.editor.save)}
                  </button>
                )}
              </div>
            </Panel>
          </ReactFlow>
        </div>

        {/* Right: Node Config Panel */}
        {selectedNode && !readOnly && (
          <div className="w-72 shrink-0 border-l bg-muted/30 p-3 overflow-y-auto">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                {t(($) => $.editor.node_config)}
              </h3>
              <button
                type="button"
                onClick={() => setSelectedNodeId(null)}
                className="text-muted-foreground hover:text-foreground text-xs"
              >
                {t(($) => $.editor.close)}
              </button>
            </div>
            <div className="space-y-4 text-sm">
              {/* Name */}
              <div>
                <label className="text-muted-foreground text-xs block mb-1">{t(($) => $.editor.name_label)}</label>
                <input
                  type="text"
                  className="w-full rounded-md border bg-background px-2.5 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                  value={(selectedNode.data.label as string) ?? ''}
                  onChange={(e) => updateSelectedNode((d) => ({ ...d, label: e.target.value }))}
                />
              </div>
              {/* Type (read-only) */}
              <div>
                <label className="text-muted-foreground text-xs block mb-1">{t(($) => $.editor.type_label)}</label>
                <p className="font-mono text-xs bg-muted rounded px-2 py-1">{selectedNode.data.nodeType as string}</p>
              </div>
              {/* Description */}
              <div>
                <label className="text-muted-foreground text-xs block mb-1">{t(($) => $.editor.description_label)}</label>
                <input
                  type="text"
                  className="w-full rounded-md border bg-background px-2.5 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                  value={(selectedNode.data.description as string) ?? ''}
                  onChange={(e) => updateSelectedNode((d) => ({ ...d, description: e.target.value }))}
                  placeholder="Optional description"
                />
              </div>
              {/* Configuration — typed fields per node type */}
              <NodeConfigFields
                nodeType={selectedNode.data.nodeType as string}
                configuration={selectedNode.data.configuration as Record<string, unknown>}
                onChange={(config) => updateSelectedNode((d) => ({ ...d, configuration: config }))}
              />
              {/* Delete node */}
              <button
                type="button"
                onClick={() => {
                  setNodes((nds) => nds.filter((n) => n.id !== selectedNodeId));
                  setEdges((eds) => eds.filter((e) => e.source !== selectedNodeId && e.target !== selectedNodeId));
                  setSelectedNodeId(null);
                }}
                className="flex w-full items-center justify-center gap-1.5 rounded-md border border-destructive/30 px-3 py-1.5 text-xs text-destructive hover:bg-destructive/10 transition-colors"
              >
                {t(($) => $.editor.delete_node)}
              </button>
            </div>
          </div>
        )}
        {selectedNode && readOnly && (
          <div className="w-64 shrink-0 border-l bg-muted/30 p-3 overflow-y-auto">
            <h3 className="text-xs font-semibold text-muted-foreground mb-3 uppercase tracking-wider">
              {t(($) => $.editor.node_config)}
            </h3>
            <div className="space-y-3 text-sm">
              <div>
                <label className="text-muted-foreground text-xs">{t(($) => $.editor.name_label)}</label>
                <p className="font-medium">{selectedNode.data.label as string}</p>
              </div>
              <div>
                <label className="text-muted-foreground text-xs">{t(($) => $.editor.type_label)}</label>
                <p className="font-mono text-xs">{selectedNode.data.nodeType as string}</p>
              </div>
              <div>
                <label className="text-muted-foreground text-xs">{t(($) => $.editor.configuration_label)}</label>
                <pre className="mt-1 rounded bg-muted p-2 text-xs overflow-x-auto">
                  {JSON.stringify(selectedNode.data.configuration, null, 2)}
                </pre>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Bottom: Run History Panel */}
      <Collapsible open={historyOpen} onOpenChange={setHistoryOpen}>
        <div className="border-t bg-muted/20">
          <CollapsibleTrigger className="flex w-full items-center gap-2 px-4 py-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider hover:bg-muted/40 transition-colors">
            {historyOpen ? (
              <ChevronDown className="size-3.5" />
            ) : (
              <ChevronRight className="size-3.5" />
            )}
            {t(($) => $.editor.run_history)}
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="max-h-72 overflow-y-auto border-t">
              {/* Active run monitor (from Test Run) */}
              {activeRunId && (
                <div className="border-b bg-accent/5">
                  <div className="px-3 py-1.5 text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
                    {t(($) => $.editor.current_run)}
                  </div>
                  <RunMonitor runId={activeRunId} workflowId={workflowId} />
                </div>
              )}
              <RunHistory workflowId={workflowId} />
            </div>
          </CollapsibleContent>
        </div>
      </Collapsible>
    </div>
  );
}


// ---------------------------------------------------------------------------
// Typed node config fields — renders field-specific inputs per node type
// ---------------------------------------------------------------------------

// Per-field definition for a node type's configuration.
interface FieldDef {
  key: string;
  label: string;
  type: 'text' | 'textarea' | 'number' | 'select';
  placeholder?: string;
  options?: { value: string; label: string }[];
}

// Registry of known fields per RuleGo node type.
const NODE_CONFIG_FIELDS: Record<string, FieldDef[]> = {
  'rssFetch': [
    { key: 'url', label: 'Feed URL', type: 'text', placeholder: 'https://example.com/feed.xml' },
    { key: 'maxItems', label: 'Max Items', type: 'number', placeholder: '20' },
    { key: 'timeoutMs', label: 'Timeout (ms)', type: 'number', placeholder: '15000' },
  ],
  'webScrape': [
    { key: 'url', label: 'URL', type: 'text', placeholder: 'https://example.com/page' },
    { key: 'extractMode', label: 'Extract Mode', type: 'select', options: [
      { value: 'text', label: 'Text' },
      { value: 'html', label: 'HTML' },
      { value: 'raw', label: 'Raw' },
    ]},
    { key: 'timeout', label: 'Timeout (s)', type: 'number', placeholder: '30' },
  ],
  'agentLLM': [
    { key: 'promptTemplate', label: 'Prompt Template', type: 'textarea', placeholder: 'Analyze the following data:\n{{.data}}' },
    { key: 'maxTokens', label: 'Max Tokens', type: 'number', placeholder: '2000' },
    { key: 'temperature', label: 'Temperature', type: 'number', placeholder: '0.3' },
  ],
  'restApiCall': [
    { key: 'restEndpointUrlPattern', label: 'URL', type: 'text', placeholder: 'https://api.example.com/data' },
    { key: 'requestMethod', label: 'Method', type: 'select', options: [
      { value: 'GET', label: 'GET' },
      { value: 'POST', label: 'POST' },
      { value: 'PUT', label: 'PUT' },
      { value: 'DELETE', label: 'DELETE' },
    ]},
  ],
  'jsFilter': [
    { key: 'jsScript', label: 'JS Condition', type: 'textarea', placeholder: 'return msg.Data.score > 5;' },
  ],
  'jsTransform': [
    { key: 'jsScript', label: 'JS Script', type: 'textarea', placeholder: 'msg.Data = JSON.stringify({...});\\nreturn {msg, metadata, msgType};' },
  ],
  'docGenerate': [
    { key: 'format', label: 'Format', type: 'select', options: [
      { value: 'markdown', label: 'Markdown' },
      { value: 'pdf', label: 'PDF' },
      { value: 'docx', label: 'DOCX' },
    ]},
    { key: 'contentTemplate', label: 'Content Template', type: 'textarea', placeholder: '# Report\n\n{{.data}}' },
  ],
  'sendEmail': [
    { key: 'to', label: 'To', type: 'text', placeholder: 'team@company.com' },
    { key: 'subject', label: 'Subject', type: 'text', placeholder: 'Notification' },
    { key: 'body', label: 'Body', type: 'textarea', placeholder: '{{.data}}' },
  ],
  'spreadsheet': [
    { key: 'separator', label: 'Separator', type: 'text', placeholder: ',' },
  ],
};

function NodeConfigFields({
  nodeType,
  configuration,
  onChange,
}: {
  nodeType: string;
  configuration: Record<string, unknown>;
  onChange: (config: Record<string, unknown>) => void;
}) {
  const { t } = useT('workflows');
  const fields = NODE_CONFIG_FIELDS[nodeType];
  const [showAdvanced, setShowAdvanced] = useState(false);

  const updateField = (key: string, value: unknown) => {
    onChange({ ...configuration, [key]: value });
  };

  // Unknown node type or user wants raw JSON — show JSON editor.
  if (!fields || showAdvanced) {
    return (
      <div>
        <div className="flex items-center justify-between mb-1">
          <label className="text-muted-foreground text-xs">{t(($) => $.editor.configuration_label)}</label>
          {fields && (
            <button
              type="button"
              onClick={() => setShowAdvanced(false)}
              className="text-[10px] text-primary hover:underline"
            >
              {t(($) => $.editor.fields_back)}
            </button>
          )}
        </div>
        <ConfigJsonEditor value={configuration} onChange={onChange} />
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {fields.map((field) => (
        <div key={field.key}>
          <label className="text-muted-foreground text-xs block mb-1">{field.label}</label>
          {field.type === 'textarea' ? (
            <textarea
              className="w-full rounded-md border bg-background px-2.5 py-2 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-ring resize-y"
              rows={4}
              value={String(configuration[field.key] ?? '')}
              placeholder={field.placeholder}
              onChange={(e) => updateField(field.key, e.target.value)}
              spellCheck={false}
            />
          ) : field.type === 'select' ? (
            <select
              className="w-full rounded-md border bg-background px-2.5 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              value={String(configuration[field.key] ?? '')}
              onChange={(e) => updateField(field.key, e.target.value)}
            >
              <option value="">—</option>
              {field.options?.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          ) : field.type === 'number' ? (
            <input
              type="number"
              className="w-full rounded-md border bg-background px-2.5 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              value={configuration[field.key] != null ? String(configuration[field.key]) : ''}
              placeholder={field.placeholder}
              onChange={(e) => updateField(field.key, e.target.value ? Number(e.target.value) : undefined)}
            />
          ) : (
            <input
              type="text"
              className="w-full rounded-md border bg-background px-2.5 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              value={String(configuration[field.key] ?? '')}
              placeholder={field.placeholder}
              onChange={(e) => updateField(field.key, e.target.value)}
            />
          )}
        </div>
      ))}
      <button
        type="button"
        onClick={() => setShowAdvanced(true)}
        className="text-[10px] text-muted-foreground hover:text-foreground"
      >
        {t(($) => $.editor.advanced_json)}
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// JSON config editor — textarea with parse-on-blur
// ---------------------------------------------------------------------------

function ConfigJsonEditor({
  value,
  onChange,
}: {
  value: Record<string, unknown>;
  onChange: (v: Record<string, unknown>) => void;
}) {
  const [text, setText] = useState(() => JSON.stringify(value, null, 2));
  const [error, setError] = useState<string | null>(null);

  const handleBlur = () => {
    try {
      const parsed = JSON.parse(text);
      if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
        onChange(parsed);
        setError(null);
      } else {
        setError('Must be a JSON object');
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Invalid JSON');
    }
  };

  return (
    <div>
      <textarea
        className="w-full rounded-md border bg-background px-2.5 py-2 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring resize-y"
        rows={8}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setError(null);
        }}
        onBlur={handleBlur}
        spellCheck={false}
      />
      {error && (
        <p className="mt-1 text-[11px] text-destructive">{error}</p>
      )}
    </div>
  );
}