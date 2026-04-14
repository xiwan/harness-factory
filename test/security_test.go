package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
