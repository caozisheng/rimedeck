package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/rulego/rulego/api/types"
	"gopkg.in/yaml.v3"
)

// RuleGo node type → Dify node type (reverse of difyTypeMap).
var ruleGoToDifyType = map[string]string{
	"agentLLM":  "llm",
	"restApiCall":  "http-request",
	"jsTransform":  "code",
	"jsFilter":     "if-else",
}

// ExportDify converts a RuleGo DSL graph to Dify YAML workflow format.
func ExportDify(graph json.RawMessage, name string) ([]byte, error) {
	var chain types.RuleChain
	if err := json.Unmarshal(graph, &chain); err != nil {
		return nil, fmt.Errorf("invalid RuleGo DSL: %w", err)
	}

	nodes := chain.Metadata.Nodes
	if nodes == nil {
		nodes = []*types.RuleNode{}
	}

	// Build start + end nodes that Dify requires.
	difyNodes := []difyNode{
		{
			ID:       "start",
			Position: difyPosition{X: 0, Y: 0},
			Data:     difyNodeData{Type: "start", Title: "Start"},
		},
	}

	for _, rn := range nodes {
		difyType := resolveDifyType(rn.Type)

		dn := difyNode{
			ID:       rn.Id,
			Position: extractDifyPosition(rn),
			Data:     buildDifyNodeData(difyType, rn),
		}
		difyNodes = append(difyNodes, dn)
	}

	difyNodes = append(difyNodes, difyNode{
		ID:       "end",
		Position: difyPosition{X: 800, Y: 0},
		Data:     difyNodeData{Type: "end", Title: "End"},
	})

	// Build edges. Dify uses source/target ID pairs.
	var edges []difyEdge

	// Connect start → first node if nodes exist.
	if len(nodes) > 0 {
		edges = append(edges, difyEdge{
			Source: "start",
			Target: nodes[0].Id,
		})
	}

	for _, c := range chain.Metadata.Connections {
		edges = append(edges, difyEdge{
			Source: c.FromId,
			Target: c.ToId,
		})
	}

	// Connect last node → end for the sink nodes (those that have no outgoing connections).
	outgoing := make(map[string]bool, len(chain.Metadata.Connections))
	for _, c := range chain.Metadata.Connections {
		outgoing[c.FromId] = true
	}
	for _, rn := range nodes {
		if !outgoing[rn.Id] {
			edges = append(edges, difyEdge{
				Source: rn.Id,
				Target: "end",
			})
		}
	}

	export := difyExport{}
	export.App.Name = name
	export.Workflow.Graph.Nodes = difyNodes
	export.Workflow.Graph.Edges = edges

	return yaml.Marshal(export)
}

func resolveDifyType(ruleGoType string) string {
	if t, ok := ruleGoToDifyType[ruleGoType]; ok {
		return t
	}
	// Default unknown to code type.
	return "code"
}

func extractDifyPosition(rn *types.RuleNode) difyPosition {
	var pos difyPosition
	if rn.AdditionalInfo != nil {
		if lx, ok := rn.AdditionalInfo["layoutX"]; ok {
			pos.X = toFloat64(lx)
		}
		if ly, ok := rn.AdditionalInfo["layoutY"]; ok {
			pos.Y = toFloat64(ly)
		}
	}
	return pos
}

func buildDifyNodeData(difyType string, rn *types.RuleNode) difyNodeData {
	data := difyNodeData{
		Type:  difyType,
		Title: rn.Name,
	}
	if rn.Configuration == nil {
		return data
	}

	switch difyType {
	case "llm":
		model := make(map[string]any)
		if p, ok := rn.Configuration["provider"]; ok {
			model["provider"] = p
		}
		if m, ok := rn.Configuration["model"]; ok {
			model["name"] = m
		}
		if len(model) > 0 {
			data.Model = model
		}
		if pt, ok := rn.Configuration["promptTemplate"]; ok {
			data.PromptTemplate = pt
		}

	case "http-request":
		if u, ok := rn.Configuration["restEndpointUrlPattern"]; ok {
			if s, ok := u.(string); ok {
				data.URL = s
			}
		}
		if m, ok := rn.Configuration["requestMethod"]; ok {
			if s, ok := m.(string); ok {
				data.Method = s
			}
		}

	case "code":
		if js, ok := rn.Configuration["jsScript"]; ok {
			if s, ok := js.(string); ok {
				data.Code = s
			}
		}
		if lang, ok := rn.Configuration["language"]; ok {
			if s, ok := lang.(string); ok {
				data.CodeLanguage = s
			}
		}

	case "if-else":
		// Dify if-else doesn't have a direct script field; keep title descriptive.
		data.Title = fmt.Sprintf("%s (condition)", rn.Name)
	}

	return data
}
