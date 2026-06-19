package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/rulego/rulego/api/types"
	"gopkg.in/yaml.v3"
)

// ImportWarning describes a non-fatal issue encountered during import.
type ImportWarning struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	Type     string `json:"type"`    // "unsupported" | "degraded" | "skipped"
	Message  string `json:"message"`
}

// ImportResult holds the converted RuleGo DSL and any warnings.
type ImportResult struct {
	Chain    json.RawMessage `json:"chain"`
	Warnings []ImportWarning `json:"warnings"`
	Source   string          `json:"source"` // "n8n" | "dify"
}

// AutoImport detects the format and converts to RuleGo DSL JSON.
func AutoImport(raw []byte) (*ImportResult, error) {
	if detectN8n(raw) {
		return importN8n(raw)
	}
	if detectDify(raw) {
		return importDify(raw)
	}
	return nil, fmt.Errorf("unrecognized workflow format: expected n8n JSON or Dify YAML")
}

// detectN8n returns true when raw looks like an n8n workflow export
// (JSON with top-level "nodes" array and "connections" object).
func detectN8n(raw []byte) bool {
	var probe struct {
		Nodes       json.RawMessage `json:"nodes"`
		Connections json.RawMessage `json:"connections"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	// Must have both fields as non-null values.
	if len(probe.Nodes) == 0 || len(probe.Connections) == 0 {
		return false
	}
	// "nodes" must start with '[' and "connections" must start with '{'.
	return probe.Nodes[0] == '[' && probe.Connections[0] == '{'
}

// detectDify returns true when raw looks like a Dify YAML export
// (YAML with top-level "workflow" key containing "graph").
func detectDify(raw []byte) bool {
	var probe struct {
		Workflow struct {
			Graph struct {
				Nodes []any `yaml:"nodes"`
			} `yaml:"graph"`
		} `yaml:"workflow"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return len(probe.Workflow.Graph.Nodes) > 0
}

// nonNilNodes ensures a nil slice becomes an empty slice so JSON
// marshals as [] rather than null, matching the RuleGo DSL convention.
func nonNilNodes(s []*types.RuleNode) []*types.RuleNode {
	if s == nil {
		return []*types.RuleNode{}
	}
	return s
}

func nonNilConns(s []types.NodeConnection) []types.NodeConnection {
	if s == nil {
		return []types.NodeConnection{}
	}
	return s
}
