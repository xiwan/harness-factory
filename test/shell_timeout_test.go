package test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xiwan/harness-factory/internal/tools"
)

func shellParams(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestShellTimeoutDefault(t *testing.T) {
	// Fast command completes well within the default 60s.
	s := tools.NewShellTool()
	out, err := s.Execute("exec", shellParams("echo ok"), t.TempDir())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("unexpected: %v %q", err, out)
	}
}

func TestShellTimeoutEnvOverride(t *testing.T) {
	// HF_SHELL_TIMEOUT=1s must kill a 5s sleep quickly.
	t.Setenv("HF_SHELL_TIMEOUT", "1s")
	s := tools.NewShellTool()
	start := time.Now()
	_, err := s.Execute("exec", shellParams("sleep 5"), t.TempDir())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected kill error, got nil")
	}
	if elapsed > 3*time.Second {
		t.Errorf("timeout override not applied: took %v", elapsed)
	}
}

func TestParseCommandsFdRedirection(t *testing.T) {
	cases := map[string][]string{
		"node driver.mjs 2>&1 | head -50":  {"node", "head"},
		"bash x.sh >&2":                    {"bash"},
		"node a.mjs &> out.log":            {"node"},
		"echo hi 2>&1; cat f":              {"echo", "cat"},
		"ls && cat f 2>/dev/null":          {"ls", "cat"},
		"echo `whoami` 2>&1":               {"__subcommand_blocked__"}, // strip must not weaken subcommand guard
		// Quoted separators are argument data, not command boundaries.
		`aws s3 cp a.md s3://b/a.md --content-type "text/markdown; charset=utf-8"`: {"aws"},
		`echo 'a|b;c' && cat f`:  {"echo", "cat"},
		`grep "x|y" f | head -1`: {"grep", "head"},
		`echo "unterminated; ls`: {"echo"}, // unterminated quote swallows the rest — never yields a hidden command
	}
	for cmd, want := range cases {
		got := tools.ParseCommands(cmd)
		if len(got) != len(want) {
			t.Errorf("ParseCommands(%q) = %v, want %v", cmd, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("ParseCommands(%q) = %v, want %v", cmd, got, want)
				break
			}
		}
	}
}

func TestShellTimeoutInvalidEnvFallsBack(t *testing.T) {
	// Garbage or out-of-range values must fall back to the 60s default,
	// not hang forever or die instantly.
	for _, v := range []string{"garbage", "-5s", "0", "2h"} {
		t.Setenv("HF_SHELL_TIMEOUT", v)
		s := tools.NewShellTool()
		out, err := s.Execute("exec", shellParams("echo ok"), t.TempDir())
		if err != nil || !strings.Contains(out, "ok") {
			t.Errorf("HF_SHELL_TIMEOUT=%q broke normal exec: %v %q", v, err, out)
		}
	}
}
