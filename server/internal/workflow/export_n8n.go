package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/rulego/rulego/api/types"
)

// RuleGo node type → n8n node type (reverse of n8nTypeMap).
var ruleGoToN8nType = map[string]string{
	"restApiCall": "n8n-nodes-base.httpRequest",
	"jsTransform": "n8n-nodes-base.code",
	"jsFilter":    "n8n-nodes-base.if",
	"sendEmail":   "n8n-nodes-base.emailSend",
	"agentLLM":  "@n8n/n8n-nodes-langchain.lmChatOpenAi",
	"rssFetch":  "n8n-nodes-base.rssFeedRead",
}

// ruleGoToN8nTypeWithNote maps types that require a note because the mapping
// is approximate (the target n8n type cannot fully represent the source).
var ruleGoToN8nTypeWithNote = map[string]struct {
	N8nType string
	Note    string
}{
	"webScrape":   {N8nType: "n8n-nodes-base.httpRequest", Note: "Originally a web-scrape node"},
	"docGenerate": {N8nType: "n8n-nodes-base.code", Note: "Originally a document-generation node"},
	"spreadsheet": {N8nType: "n8n-nodes-base.code", Note: "Originally a spreadsheet node"},
}

// ExportN8n converts a RuleGo DSL graph to n8n JSON workflow format.
func ExportN8n(graph json.RawMessage, name string) ([]byte, error) {
	var chain types.RuleChain
	if err := json.Unmarshal(graph, &chain); err != nil {
		return nil, fmt.Errorf("invalid RuleGo DSL: %w", err)
	}

	nodes := chain.Metadata.Nodes
	if nodes == nil {
		nodes = []*types.RuleNode{}
	}

	// Build id → index and id → name maps for connection resolution.
	idToName := make(map[string]string, len(nodes))
	for _, n := range nodes {
		idToName[n.Id] = n.Name
	}

	// Convert nodes.
	n8nNodes := make([]n8nNode, 0, len(nodes))
	for _, rn := range nodes {
		n8nType, note := resolveN8nType(rn.Type)

		params := convertToN8nParams(rn.Type, rn.Configuration)
		if note != "" {
			params["_importNote"] = note
		}

		var pos [2]float64
		if lx, ok := rn.AdditionalInfo["layoutX"]; ok {
			pos[0] = toFloat64(lx)
		}
		if ly, ok := rn.AdditionalInfo["layoutY"]; ok {
			pos[1] = toFloat64(ly)
		}

		n8nNodes = append(n8nNodes, n8nNode{
			ID:          rn.Id,
			Name:        rn.Name,
			Type:        n8nType,
			TypeVersion: 1,
			Position:    pos,
			Parameters:  params,
		})
	}

	// Convert connections: RuleGo's id-based → n8n's name-based.
	connections := make(map[string]map[string][][]n8nConnectionTarget)
	for _, c := range chain.Metadata.Connections {
		srcName, ok := idToName[c.FromId]
		if !ok {
			continue
		}
		dstName, ok := idToName[c.ToId]
		if !ok {
			continue
		}

		if connections[srcName] == nil {
			connections[srcName] = make(map[string][][]n8nConnectionTarget)
		}
		if connections[srcName]["main"] == nil {
			connections[srcName]["main"] = [][]n8nConnectionTarget{{}}
		}
		connections[srcName]["main"][0] = append(connections[srcName]["main"][0], n8nConnectionTarget{
			Node:  dstName,
			Type:  "main",
			Index: 0,
		})
	}

	wf := n8nWorkflow{
		Name:        name,
		Nodes:       n8nNodes,
		Connections: connections,
	}

	return json.MarshalIndent(wf, "", "  ")
}

// resolveN8nType maps a RuleGo type to n8n type, returning an optional note.
func resolveN8nType(ruleGoType string) (string, string) {
	if t, ok := ruleGoToN8nType[ruleGoType]; ok {
		return t, ""
	}
	if info, ok := ruleGoToN8nTypeWithNote[ruleGoType]; ok {
		return info.N8nType, info.Note
	}
	// Unknown type — default to httpRequest with a note.
	return "n8n-nodes-base.httpRequest", fmt.Sprintf("Unknown RuleGo type %q", ruleGoType)
}

// convertToN8nParams reverses the RuleGo config → n8n parameters mapping.
func convertToN8nParams(ruleGoType string, cfg types.Configuration) map[string]any {
	params := make(map[string]any)
	if cfg == nil {
		return params
	}

	switch ruleGoType {
	case "restApiCall":
		if u, ok := cfg["restEndpointUrlPattern"]; ok {
			params["url"] = u
		}
		if m, ok := cfg["requestMethod"]; ok {
			params["method"] = m
		}
		if h, ok := cfg["headers"]; ok {
			params["headerParameters"] = h
		}

	case "jsTransform":
		if js, ok := cfg["jsScript"]; ok {
			params["jsCode"] = js
		}

	case "jsFilter":
		if js, ok := cfg["jsScript"]; ok {
			params["jsCode"] = js
		}

	case "sendEmail":
		for _, key := range []string{"toEmail", "subject", "text", "html"} {
			if v, ok := cfg[key]; ok {
				params[key] = v
			}
		}

	case "rssFetch":
		if u, ok := cfg["url"]; ok {
			params["url"] = u
		}

	case "agentLLM":
		// Pass through LLM-related config as n8n parameters.
		for k, v := range cfg {
			params[k] = v
		}

	default:
		// Unknown: pass config as-is.
		for k, v := range cfg {
			params[k] = v
		}
	}

	return params
}

// toFloat64 converts an interface value to float64 for position extraction.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}
