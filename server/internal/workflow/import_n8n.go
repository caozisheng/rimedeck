package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rulego/rulego/api/types"
)

// n8n JSON structures for deserialization.

type n8nWorkflow struct {
	Name        string                                       `json:"name"`
	Nodes       []n8nNode                                    `json:"nodes"`
	Connections map[string]map[string][][]n8nConnectionTarget `json:"connections"`
}

type n8nNode struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	TypeVersion any            `json:"typeVersion"`
	Position    [2]float64     `json:"position"`
	Parameters  map[string]any `json:"parameters"`
}

type n8nConnectionTarget struct {
	Node  string `json:"node"`
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// n8n node type → RuleGo node type.
var n8nTypeMap = map[string]string{
	"n8n-nodes-base.httpRequest":  "restApiCall",
	"n8n-nodes-base.code":        "jsTransform",
	"n8n-nodes-base.function":    "jsTransform",
	"n8n-nodes-base.if":          "jsFilter",
	"n8n-nodes-base.set":         "jsTransform",
	"n8n-nodes-base.rssFeedRead": "rssFetch",
	"n8n-nodes-base.emailSend":   "sendEmail",
}

// n8n node types that are silently skipped (not real processing nodes).
var n8nSkipTypes = map[string]bool{
	"n8n-nodes-base.webhook": true,
	"n8n-nodes-base.noOp":   true,
}

func importN8n(raw []byte) (*ImportResult, error) {
	var wf n8nWorkflow
	if err := json.Unmarshal(raw, &wf); err != nil {
		return nil, fmt.Errorf("invalid n8n JSON: %w", err)
	}

	var warnings []ImportWarning

	// Build name → generated-id map for connection resolution.
	// Also build name → index for firstNodeIndex.
	nameToID := make(map[string]string, len(wf.Nodes))
	var ruleNodes []*types.RuleNode
	nodeIndex := 0

	for _, n := range wf.Nodes {
		if n8nSkipTypes[n.Type] {
			warnings = append(warnings, ImportWarning{
				NodeID:   n.ID,
				NodeName: n.Name,
				Type:     "skipped",
				Message:  fmt.Sprintf("n8n node type %q has no RuleGo equivalent and was skipped", n.Type),
			})
			continue
		}

		rulegoType, warn := mapN8nType(n)
		if warn != nil {
			warnings = append(warnings, *warn)
		}

		nodeID := fmt.Sprintf("node_%d", nodeIndex)
		nameToID[n.Name] = nodeID

		cfg := convertN8nConfig(n)

		ruleNodes = append(ruleNodes, &types.RuleNode{
			Id:   nodeID,
			Type: rulegoType,
			Name: n.Name,
			Configuration: cfg,
			AdditionalInfo: map[string]any{
				"layoutX": n.Position[0],
				"layoutY": n.Position[1],
			},
		})
		nodeIndex++
	}

	// Build connections. n8n connections are keyed by source node *name*.
	var connections []types.NodeConnection
	for srcName, outputs := range wf.Connections {
		srcID, ok := nameToID[srcName]
		if !ok {
			// Source was a skipped node; skip its connections too.
			continue
		}
		for _, outputGroup := range outputs { // "main", etc.
			for _, targets := range outputGroup {
				for _, t := range targets {
					dstID, ok := nameToID[t.Node]
					if !ok {
						continue
					}
					connections = append(connections, types.NodeConnection{
						FromId: srcID,
						ToId:   dstID,
						Type:   "Success",
					})
				}
			}
		}
	}

	chain := types.RuleChain{
		RuleChain: types.RuleChainBaseInfo{
			ID:   sanitizeID(wf.Name),
			Name: wf.Name,
		},
		Metadata: types.RuleMetadata{
			FirstNodeIndex: 0,
			Nodes:          nonNilNodes(ruleNodes),
			Connections:    nonNilConns(connections),
		},
	}

	chainJSON, err := json.Marshal(chain)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RuleGo chain: %w", err)
	}

	if warnings == nil {
		warnings = []ImportWarning{}
	}

	return &ImportResult{
		Chain:    chainJSON,
		Warnings: warnings,
		Source:   "n8n",
	}, nil
}

// mapN8nType resolves the RuleGo type for an n8n node.
// Returns a non-nil warning when the mapping is degraded or approximate.
func mapN8nType(n n8nNode) (string, *ImportWarning) {
	// Check LangChain nodes (prefix match).
	if strings.HasPrefix(n.Type, "@n8n/n8n-nodes-langchain.") {
		return "agentLLM", nil
	}

	if t, ok := n8nTypeMap[n.Type]; ok {
		return t, nil
	}

	// Unknown → restApiCall with degraded warning.
	return "restApiCall", &ImportWarning{
		NodeID:   n.ID,
		NodeName: n.Name,
		Type:     "degraded",
		Message:  fmt.Sprintf("unknown n8n type %q mapped to restApiCall; manual adjustment needed", n.Type),
	}
}

// convertN8nConfig translates n8n parameters to RuleGo configuration
// based on the source node type.
func convertN8nConfig(n n8nNode) types.Configuration {
	cfg := types.Configuration{}
	if n.Parameters == nil {
		return cfg
	}

	switch n.Type {
	case "n8n-nodes-base.httpRequest":
		if u, ok := n.Parameters["url"]; ok {
			cfg["restEndpointUrlPattern"] = u
		}
		if m, ok := n.Parameters["method"]; ok {
			cfg["requestMethod"] = m
		}
		// Pass through any headers / body as-is.
		if h, ok := n.Parameters["headerParameters"]; ok {
			cfg["headers"] = h
		}

	case "n8n-nodes-base.code", "n8n-nodes-base.function":
		if js, ok := n.Parameters["jsCode"]; ok {
			cfg["jsScript"] = js
		} else if js, ok := n.Parameters["functionCode"]; ok {
			cfg["jsScript"] = js
		}

	case "n8n-nodes-base.if":
		// Build a simple JS expression from n8n conditions when possible,
		// otherwise leave a placeholder that the user must fill in.
		cfg["jsScript"] = buildN8nIfScript(n.Parameters)

	case "n8n-nodes-base.set":
		// Set node assigns fields; represent as a transform script.
		cfg["jsScript"] = buildN8nSetScript(n.Parameters)

	case "n8n-nodes-base.rssFeedRead":
		if u, ok := n.Parameters["url"]; ok {
			cfg["url"] = u
		}

	case "n8n-nodes-base.emailSend":
		for _, key := range []string{"toEmail", "subject", "text", "html"} {
			if v, ok := n.Parameters[key]; ok {
				cfg[key] = v
			}
		}

	default:
		// For LangChain or unknown nodes, pass parameters as-is.
		for k, v := range n.Parameters {
			cfg[k] = v
		}
	}

	return cfg
}

// buildN8nIfScript produces a simple JS boolean expression for n8n "if" conditions.
func buildN8nIfScript(params map[string]any) string {
	// n8n v2 conditions use a "conditions" object with rules.
	// We produce a placeholder the user should edit.
	return "// TODO: Imported from n8n IF node — adjust condition\nreturn msg.Data != '';"
}

// buildN8nSetScript produces a transform script from n8n "set" node assignments.
func buildN8nSetScript(params map[string]any) string {
	return "// TODO: Imported from n8n Set node — adjust field assignments\nreturn {'msg': msg};"
}

// sanitizeID converts a human-readable name to a lowercase, hyphenated ID.
func sanitizeID(name string) string {
	id := strings.ToLower(name)
	id = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		if r == ' ' {
			return '-'
		}
		return -1
	}, id)
	// Collapse runs of hyphens.
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}
	id = strings.Trim(id, "-")
	if id == "" {
		id = "imported-workflow"
	}
	return id
}
