package workflow

import (
	"encoding/json"
	"testing"
)

func TestListTemplates(t *testing.T) {
	templates, err := ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates() error: %v", err)
	}
	if len(templates) != 8 {
		t.Fatalf("expected 8 templates, got %d", len(templates))
	}

	ids := map[string]bool{}
	for _, tmpl := range templates {
		if tmpl.ID == "" {
			t.Error("template has empty ID")
		}
		if tmpl.Name == "" {
			t.Errorf("template %q has empty Name", tmpl.ID)
		}
		if tmpl.Category == "" {
			t.Errorf("template %q has empty Category", tmpl.ID)
		}
		if tmpl.File == "" {
			t.Errorf("template %q has empty File", tmpl.ID)
		}
		if tmpl.NodeCount == 0 {
			t.Errorf("template %q has zero NodeCount", tmpl.ID)
		}
		ids[tmpl.ID] = true
	}

	// Verify all expected template IDs exist.
	expected := []string{
		"weekly-report", "meeting-minutes",
		"competitor-monitor", "news-aggregator", "price-tracker",
		"lead-enrichment", "api-report", "invoice-extract",
	}
	for _, id := range expected {
		if !ids[id] {
			t.Errorf("missing expected template %q", id)
		}
	}
}

func TestLoadTemplate(t *testing.T) {
	templates, err := ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates() error: %v", err)
	}

	for _, tmpl := range templates {
		t.Run(tmpl.ID, func(t *testing.T) {
			data, err := LoadTemplate(tmpl.ID)
			if err != nil {
				t.Fatalf("LoadTemplate(%q) error: %v", tmpl.ID, err)
			}
			if len(data) == 0 {
				t.Fatal("loaded template is empty")
			}

			// Verify it's valid JSON with the expected RuleGo structure.
			var dsl struct {
				RuleChain struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"ruleChain"`
				Metadata struct {
					FirstNodeIndex int `json:"firstNodeIndex"`
					Nodes          []struct {
						ID   string `json:"id"`
						Type string `json:"type"`
						Name string `json:"name"`
					} `json:"nodes"`
					Connections []struct {
						FromID string `json:"fromId"`
						ToID   string `json:"toId"`
						Type   string `json:"type"`
					} `json:"connections"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(data, &dsl); err != nil {
				t.Fatalf("template %q is not valid JSON: %v", tmpl.ID, err)
			}
			if dsl.RuleChain.ID == "" {
				t.Error("ruleChain.id is empty")
			}
			if dsl.RuleChain.Name == "" {
				t.Error("ruleChain.name is empty")
			}
			if len(dsl.Metadata.Nodes) == 0 {
				t.Error("metadata.nodes is empty")
			}
			if len(dsl.Metadata.Nodes) != tmpl.NodeCount {
				t.Errorf("node count mismatch: index says %d, template has %d",
					tmpl.NodeCount, len(dsl.Metadata.Nodes))
			}
			if len(dsl.Metadata.Connections) == 0 {
				t.Error("metadata.connections is empty")
			}

			// Verify each node has a type.
			for _, node := range dsl.Metadata.Nodes {
				if node.Type == "" {
					t.Errorf("node %q has empty type", node.ID)
				}
			}
		})
	}
}

func TestLoadTemplateNotFound(t *testing.T) {
	_, err := LoadTemplate("nonexistent-template")
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
}
