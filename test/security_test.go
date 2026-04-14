package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiwan/harness-factory/internal/acp"
	"github.com/xiwan/harness-factory/internal/agent"
	"github.com/xiwan/harness-factory/internal/profile"
	"github.com/xiwan/harness-factory/internal/tools"
)

func TestFsPathTraversal(t *testing.T) {
	cwd := t.TempDir()
	os.WriteFile(filepath.Join(cwd, "ok.txt"), []byte("safe"), 0644)
	fs := tools.NewFSTool()

	blocked := []string{"../../etc/passwd", "/etc/passwd", "../../../etc/shadow"}
	for _, p := range blocked {
		params, _ := json.Marshal(map[string]string{"path": p})
		_, err := fs.Execute("read", params, cwd)
		if err == nil || !strings.Contains(err.Error(), "escapes working directory") {
			t.Errorf("path %q should be blocked, got: %v", p, err)
		}
	}

	// Allowed path
	params, _ := json.Marshal(map[string]string{"path": "ok.txt"})
	out, err := fs.Execute("read", params, cwd)
	if err != nil || out != "safe" {
		t.Errorf("ok.txt should be readable, got err=%v out=%q", err, out)
	}
}

func TestWebSSRF(t *testing.T) {
	web := tools.NewWebTool()

	blocked := []struct{ url, reason string }{
		{"file:///etc/passwd", "blocked scheme"},
		{"ftp://evil.com", "blocked scheme"},
		{"http://169.254.169.254/meta", "blocked"},
		{"http://127.0.0.1:8080", "blocked"},
		{"http://10.0.0.1/internal", "blocked"},
	}
	for _, tc := range blocked {
		params, _ := json.Marshal(map[string]string{"url": tc.url})
		_, err := web.Execute("fetch", params, "/tmp")
		if err == nil || !strings.Contains(err.Error(), "blocked") {
			t.Errorf("URL %q should be blocked (%s), got: %v", tc.url, tc.reason, err)
		}
	}
}

func TestGitArgInjection(t *testing.T) {
	git := tools.NewGitTool()
	cwd := t.TempDir()

	blocked := []string{"--exec=evil", "--upload-pack=evil", "-c", "--config=x", "--receive-pack=x"}
	for _, arg := range blocked {
		params, _ := json.Marshal(map[string]string{"args": arg})
		_, err := git.Execute("diff", params, cwd)
		if err == nil || !strings.Contains(err.Error(), "blocked git flag") {
			t.Errorf("git arg %q should be blocked, got: %v", arg, err)
		}
	}
}

func TestShellSubcommandBypass(t *testing.T) {
	blocked := []string{"$(rm -rf /)", "echo `whoami`", "cat <(ls)", "echo >(cat)"}
	for _, cmd := range blocked {
		cmds := tools.ParseCommands(cmd)
		if len(cmds) != 1 || cmds[0] != "__subcommand_blocked__" {
			t.Errorf("command %q should be blocked, got: %v", cmd, cmds)
		}
	}

	// Normal commands should pass through
	cmds := tools.ParseCommands("pytest && echo ok")
	if len(cmds) != 2 || cmds[0] != "pytest" || cmds[1] != "echo" {
		t.Errorf("normal command should parse correctly, got: %v", cmds)
	}
}

func TestSystemPromptGeneration(t *testing.T) {
	p := &profile.Profile{
		Tools: map[string]profile.ToolConfig{
			"fs":  {Permissions: []string{"read", "list"}},
			"git": {Permissions: []string{"diff", "log", "show"}},
		},
		Agent: profile.AgentConfig{
			Model:        "test",
			SystemPrompt: "You are a code reviewer.",
			Temperature:  0.3,
		},
	}
	reg := tools.NewRegistry()
	tr := acp.NewTransport(strings.NewReader(""), os.Stdout)

	a := agent.New(p, reg, tr, "/workspace/project", "test-sess", "Review PR #42")
	prompt := a.SystemPrompt()

	t.Logf("=== Generated System Prompt ===\n%s", prompt)

	// Verify sections
	if !strings.Contains(prompt, "You are a code reviewer.") {
		t.Error("missing role section")
	}
	if !strings.Contains(prompt, "fs_read") || !strings.Contains(prompt, "git_diff") {
		t.Error("missing tool names")
	}
	if !strings.Contains(prompt, "/workspace/project") {
		t.Error("missing cwd")
	}
	if !strings.Contains(prompt, "Review PR #42") {
		t.Error("missing goal")
	}

	// Without goal
	a2 := agent.New(p, reg, tr, "/tmp", "test-sess2", "")
	prompt2 := a2.SystemPrompt()
	if strings.Contains(prompt2, "Task goal") {
		t.Error("goal section should be absent when goal is empty")
	}

	t.Logf("=== Without Goal ===\n%s", prompt2)
}
