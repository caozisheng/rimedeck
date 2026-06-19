package workflow

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
)

//go:embed templates/*
var templateFS embed.FS

// TemplateIndex is the top-level structure of templates/index.json.
type TemplateIndex struct {
	Templates []TemplateMeta `json:"templates"`
}

// TemplateMeta describes a single built-in workflow template.
type TemplateMeta struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	NameZh        string   `json:"name_zh"`
	Category      string   `json:"category"`
	Description   string   `json:"description"`
	DescriptionZh string   `json:"description_zh"`
	NodeCount     int      `json:"node_count"`
	Tags          []string `json:"tags"`
	File          string   `json:"file"`
}

// ListTemplates returns the metadata for all built-in workflow templates.
func ListTemplates() ([]TemplateMeta, error) {
	data, err := templateFS.ReadFile("templates/index.json")
	if err != nil {
		return nil, fmt.Errorf("read template index: %w", err)
	}
	var idx TemplateIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse template index: %w", err)
	}
	return idx.Templates, nil
}

// LoadTemplate reads the RuleGo DSL JSON for a template by its ID.
func LoadTemplate(id string) (json.RawMessage, error) {
	templates, err := ListTemplates()
	if err != nil {
		return nil, err
	}
	for _, t := range templates {
		if t.ID == id {
			data, err := templateFS.ReadFile(path.Join("templates", t.File))
			if err != nil {
				return nil, fmt.Errorf("read template %q: %w", id, err)
			}
			return json.RawMessage(data), nil
		}
	}
	return nil, fmt.Errorf("template %q not found", id)
}
