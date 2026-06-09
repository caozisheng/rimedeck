package agenttmpl

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed bundled_skills/*/SKILL.md
var bundledSkillFS embed.FS

// LoadBundledSkill reads the SKILL.md content for a bundled skill by slug.
// The slug maps to bundled_skills/<slug>/SKILL.md in the embedded filesystem.
func LoadBundledSkill(slug string) ([]byte, error) {
	path := "bundled_skills/" + slug + "/SKILL.md"
	data, err := fs.ReadFile(bundledSkillFS, path)
	if err != nil {
		return nil, fmt.Errorf("bundled skill %q not found: %w", slug, err)
	}
	return data, nil
}
