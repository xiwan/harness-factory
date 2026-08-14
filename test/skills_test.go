package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xiwan/harness-factory/internal/skills"
)

func TestMaterializeBundledSkills(t *testing.T) {
	dir := t.TempDir()
	l := skills.NewLoader(dir)

	names := map[string]bool{}
	for _, n := range l.Names() {
		names[n] = true
	}
	for _, want := range []string{"game-qa", "skill-security-audit", "aidlc"} {
		if !names[want] {
			t.Errorf("bundled skill %q not loaded", want)
		}
	}

	// Scripts and references must land on disk so the LLM can fs_read / exec them.
	files := []string{
		"game-qa/SKILL.md",
		"game-qa/scripts/check-env.sh",
		"game-qa/scripts/setup.sh",
		"game-qa/scripts/qa-driver.mjs",
		"game-qa/references/api-contract.md",
		"game-qa/references/game-profiles.md",
		"game-qa/references/scoring.md",
		"skill-security-audit/scripts/audit.sh",
	}
	for _, f := range files {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Errorf("not materialized: %s", f)
			continue
		}
		if filepath.Dir(f) == filepath.Join(filepath.Dir(filepath.Dir(f)), "scripts") && info.Mode().Perm()&0100 == 0 {
			t.Errorf("script not executable: %s (mode %v)", f, info.Mode().Perm())
		}
	}
}

func TestMaterializeDoesNotClobberExisting(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing external skill dir with same name must win over bundled.
	custom := filepath.Join(dir, "game-qa")
	os.MkdirAll(custom, 0755)
	os.WriteFile(filepath.Join(custom, "SKILL.md"), []byte("---\nname: game-qa\ndescription: custom\n---\ncustom body"), 0644)

	l := skills.NewLoader(dir)
	body, ok := l.GetBody("game-qa")
	if !ok {
		t.Fatal("game-qa not loaded")
	}
	if body != "custom body" {
		t.Errorf("external skill overwritten by bundled: %q", body)
	}
	if _, err := os.Stat(filepath.Join(custom, "scripts", "qa-driver.mjs")); err == nil {
		t.Error("bundled files leaked into pre-existing external skill dir")
	}
}

func TestMaterializeIdempotent(t *testing.T) {
	dir := t.TempDir()
	skills.NewLoader(dir)
	p := filepath.Join(dir, "game-qa", "SKILL.md")
	os.WriteFile(p, []byte("modified"), 0644)
	skills.NewLoader(dir) // second load must not overwrite user edits
	data, _ := os.ReadFile(p)
	if string(data) != "modified" {
		t.Error("second Materialize overwrote existing files")
	}
}
