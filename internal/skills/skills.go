package skills

import (
	"crypto/md5"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed bundled/*/SKILL.md bundled/*/scripts/* bundled/*/references/*
var bundledFS embed.FS

type Skill struct {
	Name        string
	Description string
	Dir         string
	Body        string
}

type Loader struct {
	mu       sync.RWMutex
	skills   map[string]*Skill
	dir      string
	hash     string // md5 of metadata, for change detection
	stopCh   chan struct{}
}

func NewLoader(dir string) *Loader {
	l := &Loader{
		skills: make(map[string]*Skill),
		dir:    dir,
		stopCh: make(chan struct{}),
	}
	l.loadBundled()
	if dir != "" {
		Materialize(dir)
		l.loadDir(dir)
	}
	l.hash = l.computeHash()
	return l
}

// Materialize writes bundled skill files (SKILL.md, scripts/*, references/*) to dir
// so the LLM can fs_read them and execute scripts via shell. A skill dir that
// already exists on disk is left untouched (external skills override bundled).
// Best-effort: errors are silently skipped, matching loader behavior.
func Materialize(dir string) {
	entries, err := bundledFS.ReadDir("bundled")
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		target := filepath.Join(dir, e.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		src := "bundled/" + e.Name()
		fs.WalkDir(bundledFS, src, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel := strings.TrimPrefix(p, src+"/")
			dst := filepath.Join(target, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return nil
			}
			data, err := bundledFS.ReadFile(p)
			if err != nil {
				return nil
			}
			mode := os.FileMode(0644)
			if strings.HasPrefix(rel, "scripts/") {
				mode = 0755
			}
			os.WriteFile(dst, data, mode)
			return nil
		})
	}
}

// StartWatcher polls the skills directory every 60s for changes.
func (l *Loader) StartWatcher() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				l.reload()
			case <-l.stopCh:
				return
			}
		}
	}()
}

func (l *Loader) Stop() {
	close(l.stopCh)
}

func (l *Loader) reload() {
	if l.dir == "" {
		return
	}
	fresh := make(map[string]*Skill)
	// Bundled first
	loadBundledInto(fresh)
	// External overrides
	loadDirInto(l.dir, fresh)

	newHash := computeHashFrom(fresh)
	if newHash == l.hash {
		return
	}

	l.mu.Lock()
	l.skills = fresh
	l.hash = newHash
	l.mu.Unlock()
}

func (l *Loader) loadBundled() {
	loadBundledInto(l.skills)
}

func loadBundledInto(m map[string]*Skill) {
	entries, err := bundledFS.ReadDir("bundled")
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := bundledFS.ReadFile("bundled/" + e.Name() + "/SKILL.md")
		if err != nil {
			continue
		}
		name, desc, body := parseFrontmatter(string(data))
		if name == "" {
			name = e.Name()
		}
		m[name] = &Skill{Name: name, Description: desc, Body: body}
	}
}

func (l *Loader) loadDir(dir string) {
	loadDirInto(dir, l.skills)
}

func loadDirInto(dir string, m map[string]*Skill) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
		if err != nil {
			continue
		}
		name, desc, body := parseFrontmatter(string(data))
		if name == "" {
			name = e.Name()
		}
		m[name] = &Skill{Name: name, Description: desc, Dir: skillDir, Body: body}
	}
}

// Metadata returns skill names + descriptions for system prompt injection.
// Thread-safe — can be called while watcher is running.
func (l *Loader) Metadata() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return metadataFrom(l.skills)
}

func metadataFrom(skills map[string]*Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nAvailable skills:\n")
	for _, s := range skills {
		sb.WriteString("- ")
		sb.WriteString(s.Name)
		if s.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(s.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nSkills are located under the `skills/` directory in your working directory.")
	sb.WriteString("\nTo use a skill, read `skills/<name>/SKILL.md` with fs_read (relative path), then follow its instructions.")
	return sb.String()
}

func (l *Loader) GetBody(name string) (string, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s, ok := l.skills[name]
	if !ok {
		return "", false
	}
	return s.Body, true
}

func (l *Loader) Names() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	names := make([]string, 0, len(l.skills))
	for k := range l.skills {
		names = append(names, k)
	}
	return names
}

func (l *Loader) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.skills)
}

func (l *Loader) computeHash() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return computeHashFrom(l.skills)
}

func computeHashFrom(skills map[string]*Skill) string {
	h := md5.New()
	for name, s := range skills {
		fmt.Fprintf(h, "%s:%s\n", name, s.Description)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func parseFrontmatter(content string) (name, description, body string) {
	if !strings.HasPrefix(content, "---") {
		lines := strings.SplitN(content, "\n", 10)
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				description = trimmed
				break
			}
			if strings.HasPrefix(trimmed, "# ") {
				description = strings.TrimPrefix(trimmed, "# ")
				break
			}
		}
		return "", description, content
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
