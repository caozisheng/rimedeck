package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/rulego/rulego/api/types"
	"gopkg.in/yaml.v3"
)

// Dify YAML structures for deserialization.

type difyExport struct {
	App struct {
		Name string `yaml:"name"`
	} `yaml:"app"`
	Workflow struct {
		Graph struct {
			Nodes []difyNode `yaml:"nodes"`
			Edges []difyEdge `yaml:"edges"`
		} `yaml:"graph"`
	} `yaml:"workflow"`
}

type difyNode struct {
	ID       string       `yaml:"id"`
	Position difyPosition `yaml:"position"`
	Data     difyNodeData `yaml:"data"`
}

type difyPosition struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
}

type difyNodeData struct {
	Type            string         `yaml:"type"`
	Title           string         `yaml:"title"`
	Variables       []any          `yaml:"variables,omitempty"`
	Model           map[string]any `yaml:"model,omitempty"`
	PromptTemplate  any            `yaml:"prompt_template,omitempty"`
	Code            string         `yaml:"code,omitempty"`
	CodeLanguage    string         `yaml:"code_language,omitempty"`
	URL             string         `yaml:"url,omitempty"`
	Method          string         `yaml:"method,omitempty"`
	Template        string         `yaml:"template,omitempty"`
}

type difyEdge struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

// Dify node types that map to nothing in RuleGo (entry/exit points).
var difySkipTypes = map[string]bool{
	"start":  true,
	"end":    true,
	"answer": true,
}

// Dify node types that cannot be converted and produce a warning.
var difyWarnTypes = map[string]bool{
	"iteration":          true,
	"knowledge-retrieval": true,
}

// Dify node type → RuleGo node type.
var difyTypeMap = map[string]string{
	"llm":                "agentLLM",
	"http-request":       "restApiCall",
	"code":               "jsTransform",
	"if-else":            "jsFilter",
	"template-transform": "jsTransform",
}

func importDify(raw []byte) (*ImportResult, error) {
	var export difyExport
	if err := yaml.Unmarshal(raw, &export); err != nil {
		return nil, fmt.Errorf("invalid Dify YAML: %w", err)
	}

	var warnings []ImportWarning

	// Track which source IDs are kept so we can filter edges.
	keptIDs := make(map[string]string) // dify id → rulego node id
	var ruleNodes []*types.RuleNode
	nodeIndex := 0

	for _, dn := range export.Workflow.Graph.Nodes {
		nodeType := dn.Data.Type

		if difySkipTypes[nodeType] {
			continue
		}

		if difyWarnTypes[nodeType] {
			warnings = append(warnings, ImportWarning{
				NodeID:   dn.ID,
				NodeName: nodeName(dn),
				Type:     "unsupported",
				Message:  fmt.Sprintf("Dify node type %q cannot be converted and was skipped", nodeType),
			})
			continue
		}

		rulegoType, warn := mapDifyType(dn)
		if warn != nil {
			warnings = append(warnings, *warn)
		}

		nodeID := fmt.Sprintf("node_%d", nodeIndex)
		keptIDs[dn.ID] = nodeID

		cfg := convertDifyConfig(dn)

		ruleNodes = append(ruleNodes, &types.RuleNode{
			Id:   nodeID,
			Type: rulegoType,
			Name: nodeName(dn),
			Configuration: cfg,
			AdditionalInfo: map[string]any{
				"layoutX": dn.Position.X,
				"layoutY": dn.Position.Y,
			},
		})
		nodeIndex++
	}

	// Build connections from edges, skipping any that reference removed nodes.
	var connections []types.NodeConnection
	for _, e := range export.Workflow.Graph.Edges {
		srcID, srcOK := keptIDs[e.Source]
		dstID, dstOK := keptIDs[e.Target]
		if !srcOK || !dstOK {
			continue
		}
		connections = append(connections, types.NodeConnection{
			FromId: srcID,
			ToId:   dstID,
			Type:   "Success",
		})
	}

	name := export.App.Name
	if name == "" {
		name = "Imported Dify Workflow"
	}

	chain := types.RuleChain{
		RuleChain: types.RuleChainBaseInfo{
			ID:   sanitizeID(name),
			Name: name,
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
		Source:   "dify",
	}, nil
}

func mapDifyType(dn difyNode) (string, *ImportWarning) {
	if t, ok := difyTypeMap[dn.Data.Type]; ok {
		return t, nil
	}
	return "restApiCall", &ImportWarning{
		NodeID:   dn.ID,
		NodeName: nodeName(dn),
		Type:     "degraded",
		Message:  fmt.Sprintf("unknown Dify type %q mapped to restApiCall; manual adjustment needed", dn.Data.Type),
	}
}

func convertDifyConfig(dn difyNode) types.Configuration {
	cfg := types.Configuration{}

	switch dn.Data.Type {
	case "llm":
		if dn.Data.Model != nil {
			if provider, ok := dn.Data.Model["provider"]; ok {
				cfg["provider"] = provider
			}
			if name, ok := dn.Data.Model["name"]; ok {
				cfg["model"] = name
			}
		}
		if dn.Data.PromptTemplate != nil {
			cfg["promptTemplate"] = dn.Data.PromptTemplate
		}

	case "http-request":
		if dn.Data.URL != "" {
			cfg["restEndpointUrlPattern"] = dn.Data.URL
		}
		if dn.Data.Method != "" {
			cfg["requestMethod"] = dn.Data.Method
		}

	case "code":
		if dn.Data.Code != "" {
			cfg["jsScript"] = dn.Data.Code
		}
		if dn.Data.CodeLanguage != "" {
			cfg["language"] = dn.Data.CodeLanguage
		}

	case "if-else":
		cfg["jsScript"] = "// TODO: Imported from Dify if-else node — adjust condition\nreturn msg.Data != '';"

	case "template-transform":
		if dn.Data.Template != "" {
			cfg["jsScript"] = fmt.Sprintf("// Template imported from Dify\nvar template = %q;\nreturn {'msg': template};", dn.Data.Template)
		}
	}

	return cfg
}

func nodeName(dn difyNode) string {
	if dn.Data.Title != "" {
		return dn.Data.Title
	}
	return dn.Data.Type + "_" + dn.ID
}
