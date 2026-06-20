package daemon

import "encoding/json"

// injectSOPMcpServer appends the Rimedeck SOP MCP server to the agent's
// managed McpConfig so agent runtimes see trigger_sop / list_sops as
// available tools. The existing config (if any) is preserved; the SOP
// entry is added under the key "rimedeck-sops".
func injectSOPMcpServer(existing json.RawMessage, serverBaseURL, agentID, taskToken string) json.RawMessage {
	type mcpServerEntry struct {
		URL       string            `json:"url"`
		Transport string            `json:"transport,omitempty"`
		Headers   map[string]string `json:"headers,omitempty"`
	}
	type mcpConfigShape struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}

	var cfg mcpConfigShape
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &cfg)
	}
	if cfg.McpServers == nil {
		cfg.McpServers = make(map[string]json.RawMessage)
	}

	entry := mcpServerEntry{
		URL:       serverBaseURL + "/mcp/sops/" + agentID,
		Transport: "sse",
		Headers:   map[string]string{"Authorization": "Bearer " + taskToken},
	}
	entryJSON, _ := json.Marshal(entry)
	cfg.McpServers["rimedeck-sops"] = entryJSON

	data, _ := json.Marshal(cfg)
	return data
}
