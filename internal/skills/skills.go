// Package skills loads SKILL.md files from a skills directory.
// Metadata (name + description) is injected into system prompt.
// Full body is loaded on demand when the agent needs it.
package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a loaded skill.
type Skill struct {
	Name        string
	Description string
	Dir         string // absolute path to skill directory
	Body        string // full SKILL.md content below frontmatter (lazy loaded)
}

// Loader scans a skills directory and provides metadata + on-demand body loading.
type Loader struct {
	skills map[string]*Skill
}

// NewLoader scans the given directory for skills (each subdirectory with SKILL.md).
func NewLoader(dir string) *Loader {
	l := &Loader{skills: make(map[string]*Skill)}
	if dir == "" {
		return l
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return l
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, e.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}
		name, desc, body := parseFrontmatter(string(data))
		if name == "" {
			name = e.Name()
		}
		l.skills[name] = &Skill{
			Name:        name,
			Description: desc,
			Dir:         skillDir,
			Body:        body,
		}
	}
	return l
}

// Metadata returns a compact string of all skill names + descriptions for system prompt injection.
func (l *Loader) Metadata() string {
	if len(l.skills) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nAvailable skills:\n")
	for _, s := range l.skills {
		sb.WriteString("- ")
		sb.WriteString(s.Name)
		if s.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(s.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nTo use a skill, read its full instructions from the skills directory when needed.")
	return sb.String()
}

// GetBody returns the full SKILL.md body for a skill (below frontmatter).
func (l *Loader) GetBody(name string) (string, bool) {
	s, ok := l.skills[name]
	if !ok {
		return "", false
	}
	return s.Body, true
}

// Names returns all loaded skill names.
func (l *Loader) Names() []string {
	names := make([]string, 0, len(l.skills))
	for k := range l.skills {
		names = append(names, k)
	}
	return names
}

// Count returns the number of loaded skills.
func (l *Loader) Count() int {
	return len(l.skills)
}

// parseFrontmatter extracts name, description, and body from SKILL.md content.
func parseFrontmatter(content string) (name, description, body string) {
	if !strings.HasPrefix(content, "---") {
		return "", "", content
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return "", "", content
	}
	fm := content[3 : end+3]
	body = strings.TrimSpace(content[end+6:])

	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return
}
