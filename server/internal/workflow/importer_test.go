package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── AutoImport detection ──────────────────────────────────────────────

func TestAutoImport_DetectsN8n(t *testing.T) {
	n8nJSON := `{
		"name": "Test Workflow",
		"nodes": [
			{
				"id": "uuid-1",
				"name": "HTTP Request",
				"type": "n8n-nodes-base.httpRequest",
				"position": [260, 340],
				"parameters": {"url": "https://api.example.com", "method": "GET"}
			}
		],
		"connections": {
			"HTTP Request": {"main": [[]]}
		}
	}`
	result, err := AutoImport([]byte(n8nJSON))
	if err != nil {
		t.Fatalf("AutoImport failed: %v", err)
	}
	if result.Source != "n8n" {
		t.Errorf("expected source=n8n, got %q", result.Source)
	}
}

func TestAutoImport_DetectsDify(t *testing.T) {
	difyYAML := `
app:
  name: "Test App"
workflow:
  graph:
    nodes:
      - id: start
        position: {x: 100, y: 200}
        data:
          type: start
          variables: []
      - id: node1
        position: {x: 400, y: 200}
        data:
          type: llm
          title: "LLM Call"
          model: {provider: openai, name: gpt-4o}
    edges:
      - source: start
        target: node1
`
	result, err := AutoImport([]byte(difyYAML))
	if err != nil {
		t.Fatalf("AutoImport failed: %v", err)
	}
	if result.Source != "dify" {
		t.Errorf("expected source=dify, got %q", result.Source)
	}
}

func TestAutoImport_RejectsGarbage(t *testing.T) {
	_, err := AutoImport([]byte(`{"random": true}`))
	if err == nil {
		t.Fatal("expected error for unrecognized format")
	}
}

// ── n8n importer ──────────────────────────────────────────────────────

func TestImportN8n_NodeTypeMapping(t *testing.T) {
	n8nJSON := `{
		"name": "Type Mapping Test",
		"nodes": [
			{"id":"1","name":"HTTP","type":"n8n-nodes-base.httpRequest","position":[0,0],"parameters":{"url":"http://x","method":"POST"}},
			{"id":"2","name":"Code","type":"n8n-nodes-base.code","position":[100,0],"parameters":{"jsCode":"return items;"}},
			{"id":"3","name":"Func","type":"n8n-nodes-base.function","position":[200,0],"parameters":{"functionCode":"return items;"}},
			{"id":"4","name":"If","type":"n8n-nodes-base.if","position":[300,0],"parameters":{}},
			{"id":"5","name":"Set","type":"n8n-nodes-base.set","position":[400,0],"parameters":{}},
			{"id":"6","name":"RSS","type":"n8n-nodes-base.rssFeedRead","position":[500,0],"parameters":{"url":"http://feed"}},
			{"id":"7","name":"Email","type":"n8n-nodes-base.emailSend","position":[600,0],"parameters":{"toEmail":"a@b.c","subject":"hi"}},
			{"id":"8","name":"LC","type":"@n8n/n8n-nodes-langchain.agent","position":[700,0],"parameters":{}},
			{"id":"9","name":"Webhook","type":"n8n-nodes-base.webhook","position":[800,0],"parameters":{}},
			{"id":"10","name":"NoOp","type":"n8n-nodes-base.noOp","position":[900,0],"parameters":{}},
			{"id":"11","name":"Unknown","type":"n8n-nodes-base.somethingNew","position":[1000,0],"parameters":{}}
		],
		"connections": {}
	}`
	result, err := importN8n([]byte(n8nJSON))
	if err != nil {
		t.Fatalf("importN8n failed: %v", err)
	}

	var chain struct {
		Metadata struct {
			Nodes []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"nodes"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(result.Chain, &chain); err != nil {
		t.Fatalf("failed to parse chain: %v", err)
	}

	// Webhook and NoOp should be skipped → 9 nodes total
	wantTypes := map[string]string{
		"HTTP":    "restApiCall",
		"Code":    "jsTransform",
		"Func":    "jsTransform",
		"If":      "jsFilter",
		"Set":     "jsTransform",
		"RSS":     "rssFetch",
		"Email":   "sendEmail",
		"LC":      "agentLLM",
		"Unknown": "restApiCall",
	}

	if len(chain.Metadata.Nodes) != len(wantTypes) {
		t.Fatalf("expected %d nodes, got %d", len(wantTypes), len(chain.Metadata.Nodes))
	}

	for _, n := range chain.Metadata.Nodes {
		want, ok := wantTypes[n.Name]
		if !ok {
			t.Errorf("unexpected node %q in output", n.Name)
			continue
		}
		if n.Type != want {
			t.Errorf("node %q: expected type %q, got %q", n.Name, want, n.Type)
		}
	}

	// Check warnings: 2 skipped (Webhook, NoOp) + 1 degraded (Unknown)
	skipped, degraded := 0, 0
	for _, w := range result.Warnings {
		switch w.Type {
		case "skipped":
			skipped++
		case "degraded":
			degraded++
		}
	}
	if skipped != 2 {
		t.Errorf("expected 2 skipped warnings, got %d", skipped)
	}
	if degraded != 1 {
		t.Errorf("expected 1 degraded warning, got %d", degraded)
	}
}

func TestImportN8n_Connections(t *testing.T) {
	n8nJSON := `{
		"name": "Connection Test",
		"nodes": [
			{"id":"a","name":"Step1","type":"n8n-nodes-base.httpRequest","position":[0,0],"parameters":{"url":"http://x","method":"GET"}},
			{"id":"b","name":"Step2","type":"n8n-nodes-base.code","position":[200,0],"parameters":{"jsCode":"x"}},
			{"id":"c","name":"Step3","type":"n8n-nodes-base.set","position":[400,0],"parameters":{}}
		],
		"connections": {
			"Step1": {"main": [[{"node":"Step2","type":"main","index":0}]]},
			"Step2": {"main": [[{"node":"Step3","type":"main","index":0}]]}
		}
	}`
	result, err := importN8n([]byte(n8nJSON))
	if err != nil {
		t.Fatalf("importN8n failed: %v", err)
	}

	var chain struct {
		Metadata struct {
			Connections []struct {
				FromId string `json:"fromId"`
				ToId   string `json:"toId"`
				Type   string `json:"type"`
			} `json:"connections"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(result.Chain, &chain); err != nil {
		t.Fatalf("failed to parse chain: %v", err)
	}

	if len(chain.Metadata.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(chain.Metadata.Connections))
	}

	// Step1(node_0) → Step2(node_1), Step2(node_1) → Step3(node_2)
	c0 := chain.Metadata.Connections[0]
	c1 := chain.Metadata.Connections[1]
	// Connection order from map iteration is non-deterministic, so check both.
	found01, found12 := false, false
	for _, c := range []struct{ FromId, ToId, Type string }{
		{c0.FromId, c0.ToId, c0.Type},
		{c1.FromId, c1.ToId, c1.Type},
	} {
		if c.FromId == "node_0" && c.ToId == "node_1" && c.Type == "Success" {
			found01 = true
		}
		if c.FromId == "node_1" && c.ToId == "node_2" && c.Type == "Success" {
			found12 = true
		}
	}
	if !found01 {
		t.Error("missing connection node_0 → node_1")
	}
	if !found12 {
		t.Error("missing connection node_1 → node_2")
	}
}

func TestImportN8n_ConfigMapping(t *testing.T) {
	n8nJSON := `{
		"name": "Config Test",
		"nodes": [
			{"id":"1","name":"HTTP","type":"n8n-nodes-base.httpRequest","position":[100,200],"parameters":{"url":"https://api.example.com/v1","method":"POST"}},
			{"id":"2","name":"Code","type":"n8n-nodes-base.code","position":[300,200],"parameters":{"jsCode":"return items.map(i => i);"}}
		],
		"connections": {}
	}`
	result, err := importN8n([]byte(n8nJSON))
	if err != nil {
		t.Fatalf("importN8n failed: %v", err)
	}

	var chain struct {
		Metadata struct {
			Nodes []struct {
				Name          string         `json:"name"`
				Configuration map[string]any `json:"configuration"`
				AdditionalInfo map[string]any `json:"additionalInfo"`
			} `json:"nodes"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(result.Chain, &chain); err != nil {
		t.Fatalf("failed to parse chain: %v", err)
	}

	// HTTP node config
	httpNode := chain.Metadata.Nodes[0]
	if httpNode.Configuration["restEndpointUrlPattern"] != "https://api.example.com/v1" {
		t.Errorf("HTTP url mismatch: %v", httpNode.Configuration)
	}
	if httpNode.Configuration["requestMethod"] != "POST" {
		t.Errorf("HTTP method mismatch: %v", httpNode.Configuration)
	}
	// Position
	if httpNode.AdditionalInfo["layoutX"] != float64(100) {
		t.Errorf("HTTP layoutX mismatch: %v", httpNode.AdditionalInfo["layoutX"])
	}
	if httpNode.AdditionalInfo["layoutY"] != float64(200) {
		t.Errorf("HTTP layoutY mismatch: %v", httpNode.AdditionalInfo["layoutY"])
	}

	// Code node config
	codeNode := chain.Metadata.Nodes[1]
	if codeNode.Configuration["jsScript"] != "return items.map(i => i);" {
		t.Errorf("Code jsScript mismatch: %v", codeNode.Configuration)
	}
}

func TestImportN8n_SkippedNodeConnectionsDrop(t *testing.T) {
	// Webhook → Step1. Webhook is skipped, so its outbound connections should also be dropped.
	n8nJSON := `{
		"name": "Skip Conn Test",
		"nodes": [
			{"id":"w","name":"Webhook","type":"n8n-nodes-base.webhook","position":[0,0],"parameters":{}},
			{"id":"s","name":"Step","type":"n8n-nodes-base.httpRequest","position":[200,0],"parameters":{"url":"http://x","method":"GET"}}
		],
		"connections": {
			"Webhook": {"main": [[{"node":"Step","type":"main","index":0}]]}
		}
	}`
	result, err := importN8n([]byte(n8nJSON))
	if err != nil {
		t.Fatalf("importN8n failed: %v", err)
	}

	var chain struct {
		Metadata struct {
			Connections []json.RawMessage `json:"connections"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(result.Chain, &chain); err != nil {
		t.Fatalf("failed to parse chain: %v", err)
	}

	if len(chain.Metadata.Connections) != 0 {
		t.Errorf("expected 0 connections (source was skipped), got %d", len(chain.Metadata.Connections))
	}
}

// ── Dify importer ─────────────────────────────────────────────────────

func TestImportDify_NodeTypeMapping(t *testing.T) {
	difyYAML := `
app:
  name: "Dify Test"
workflow:
  graph:
    nodes:
      - id: start
        position: {x: 0, y: 0}
        data:
          type: start
      - id: n1
        position: {x: 200, y: 0}
        data:
          type: llm
          title: "LLM Node"
          model: {provider: openai, name: gpt-4o}
      - id: n2
        position: {x: 400, y: 0}
        data:
          type: http-request
          title: "API Call"
          url: "https://example.com"
          method: POST
      - id: n3
        position: {x: 600, y: 0}
        data:
          type: code
          title: "Transform"
          code: "return result"
      - id: n4
        position: {x: 800, y: 0}
        data:
          type: if-else
          title: "Branch"
      - id: n5
        position: {x: 1000, y: 0}
        data:
          type: template-transform
          title: "Template"
          template: "Hello {{name}}"
      - id: n6
        position: {x: 1200, y: 0}
        data:
          type: end
      - id: n7
        position: {x: 1400, y: 0}
        data:
          type: iteration
          title: "Loop"
      - id: n8
        position: {x: 1600, y: 0}
        data:
          type: knowledge-retrieval
          title: "KB Search"
    edges:
      - source: start
        target: n1
      - source: n1
        target: n2
`
	result, err := importDify([]byte(difyYAML))
	if err != nil {
		t.Fatalf("importDify failed: %v", err)
	}

	var chain struct {
		RuleChain struct {
			Name string `json:"name"`
		} `json:"ruleChain"`
		Metadata struct {
			Nodes []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"nodes"`
			Connections []struct {
				FromId string `json:"fromId"`
				ToId   string `json:"toId"`
			} `json:"connections"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(result.Chain, &chain); err != nil {
		t.Fatalf("failed to parse chain: %v", err)
	}

	if chain.RuleChain.Name != "Dify Test" {
		t.Errorf("expected name %q, got %q", "Dify Test", chain.RuleChain.Name)
	}

	// start, end skipped; iteration and knowledge-retrieval warned+skipped → 5 nodes
	wantTypes := map[string]string{
		"LLM Node":  "agentLLM",
		"API Call":  "restApiCall",
		"Transform": "jsTransform",
		"Branch":    "jsFilter",
		"Template":  "jsTransform",
	}

	if len(chain.Metadata.Nodes) != len(wantTypes) {
		t.Fatalf("expected %d nodes, got %d", len(wantTypes), len(chain.Metadata.Nodes))
	}
	for _, n := range chain.Metadata.Nodes {
		want, ok := wantTypes[n.Name]
		if !ok {
			t.Errorf("unexpected node %q", n.Name)
			continue
		}
		if n.Type != want {
			t.Errorf("node %q: expected type %q, got %q", n.Name, want, n.Type)
		}
	}

	// Edge start→n1: start is skipped, so this connection drops.
	// Edge n1→n2: both kept → 1 connection
	if len(chain.Metadata.Connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(chain.Metadata.Connections))
	}
	if chain.Metadata.Connections[0].FromId != "node_0" || chain.Metadata.Connections[0].ToId != "node_1" {
		t.Errorf("unexpected connection: %+v", chain.Metadata.Connections[0])
	}

	// Warnings: 2 unsupported (iteration, knowledge-retrieval)
	unsupported := 0
	for _, w := range result.Warnings {
		if w.Type == "unsupported" {
			unsupported++
		}
	}
	if unsupported != 2 {
		t.Errorf("expected 2 unsupported warnings, got %d", unsupported)
	}
}

func TestImportDify_ConfigMapping(t *testing.T) {
	difyYAML := `
app:
  name: "Config Test"
workflow:
  graph:
    nodes:
      - id: n1
        position: {x: 100, y: 200}
        data:
          type: http-request
          title: "Fetch Data"
          url: "https://api.example.com/data"
          method: GET
      - id: n2
        position: {x: 300, y: 200}
        data:
          type: code
          title: "Process"
          code: "return data.map(x => x.value)"
          code_language: javascript
    edges: []
`
	result, err := importDify([]byte(difyYAML))
	if err != nil {
		t.Fatalf("importDify failed: %v", err)
	}

	var chain struct {
		Metadata struct {
			Nodes []struct {
				Name          string         `json:"name"`
				Configuration map[string]any `json:"configuration"`
				AdditionalInfo map[string]any `json:"additionalInfo"`
			} `json:"nodes"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(result.Chain, &chain); err != nil {
		t.Fatalf("failed to parse chain: %v", err)
	}

	if len(chain.Metadata.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(chain.Metadata.Nodes))
	}

	httpNode := chain.Metadata.Nodes[0]
	if httpNode.Configuration["restEndpointUrlPattern"] != "https://api.example.com/data" {
		t.Errorf("HTTP url mismatch: %v", httpNode.Configuration)
	}
	if httpNode.Configuration["requestMethod"] != "GET" {
		t.Errorf("HTTP method mismatch: %v", httpNode.Configuration)
	}
	if httpNode.AdditionalInfo["layoutX"] != float64(100) {
		t.Errorf("layoutX mismatch: %v", httpNode.AdditionalInfo)
	}

	codeNode := chain.Metadata.Nodes[1]
	if codeNode.Configuration["jsScript"] != "return data.map(x => x.value)" {
		t.Errorf("code jsScript mismatch: %v", codeNode.Configuration)
	}
	if codeNode.Configuration["language"] != "javascript" {
		t.Errorf("code language mismatch: %v", codeNode.Configuration)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"My Workflow", "my-workflow"},
		{"Hello   World!", "hello-world"},
		{"  ---test---  ", "test"},
		{"", "imported-workflow"},
		{"已有中文", "imported-workflow"},
		{"mix-123_ok", "mix-123_ok"},
	}
	for _, tc := range tests {
		got := sanitizeID(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestImportN8n_OutputMatchesRuleGoDSL(t *testing.T) {
	// Verify the output JSON has the exact structure rulego.New() expects:
	// "nodes":[] and "connections":[] (never null).
	n8nJSON := `{
		"name": "Empty Connections",
		"nodes": [
			{"id":"1","name":"Only","type":"n8n-nodes-base.httpRequest","position":[0,0],"parameters":{"url":"http://x","method":"GET"}}
		],
		"connections": {}
	}`
	result, err := importN8n([]byte(n8nJSON))
	if err != nil {
		t.Fatalf("importN8n failed: %v", err)
	}

	raw := string(result.Chain)

	// Must contain "nodes":[ and "connections":[ (not null)
	if !strings.Contains(raw, `"nodes":[`) {
		t.Errorf("expected nodes as array, got: %s", raw)
	}
	if !strings.Contains(raw, `"connections":[`) {
		t.Errorf("expected connections as array, got: %s", raw)
	}
	if strings.Contains(raw, `"connections":null`) {
		t.Error("connections must not be null")
	}

	// Must be valid JSON that round-trips through rulego types.
	var chain struct {
		RuleChain struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"ruleChain"`
		Metadata struct {
			FirstNodeIndex int                    `json:"firstNodeIndex"`
			Nodes          []map[string]any       `json:"nodes"`
			Connections    []map[string]any       `json:"connections"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(result.Chain, &chain); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if chain.RuleChain.ID != "empty-connections" {
		t.Errorf("chain id = %q, want %q", chain.RuleChain.ID, "empty-connections")
	}
	if len(chain.Metadata.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(chain.Metadata.Nodes))
	}
	if len(chain.Metadata.Connections) != 0 {
		t.Errorf("expected 0 connections, got %d", len(chain.Metadata.Connections))
	}
}

func TestImportDify_EmptyEdgesNotNull(t *testing.T) {
	difyYAML := `
app:
  name: "No Edges"
workflow:
  graph:
    nodes:
      - id: n1
        position: {x: 0, y: 0}
        data:
          type: llm
          title: "Solo"
          model: {provider: openai, name: gpt-4o}
    edges: []
`
	result, err := importDify([]byte(difyYAML))
	if err != nil {
		t.Fatalf("importDify failed: %v", err)
	}
	raw := string(result.Chain)
	if strings.Contains(raw, `"connections":null`) {
		t.Error("connections must not be null")
	}
	if strings.Contains(raw, `"nodes":null`) {
		t.Error("nodes must not be null")
	}
}
