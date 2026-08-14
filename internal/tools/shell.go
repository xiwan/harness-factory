package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type ShellTool struct{}

func NewShellTool() *ShellTool { return &ShellTool{} }

func (s *ShellTool) Name() string { return "shell" }

func (s *ShellTool) Operations() []Operation {
	return []Operation{
		{Name: "exec", Description: "Execute a shell command", Parameters: []ParamDef{
			{Name: "command", Type: "string", Description: "Command to execute", Required: true},
		}},
	}
}

func (s *ShellTool) Execute(op string, params json.RawMessage, cwd string) (string, error) {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}

	// Extract the base command for allowlist/blocklist checking
	// (permission checker handles the allowlist logic, we just execute here)
	cmd := exec.Command("sh", "-c", p.Command)
	cmd.Dir = cwd

	timer := time.AfterFunc(shellTimeout(), func() { cmd.Process.Kill() })
	defer timer.Stop()

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("shell exec: %w\n%s", err, string(out))
	}
	return string(out), nil
}

// shellTimeout returns the per-command kill timeout. Long-running workloads
// (e.g. browser-driven QA sessions) can raise it via HF_SHELL_TIMEOUT
// (Go duration, e.g. "300s"). Values outside (0, 30m] fall back to 60s.
func shellTimeout() time.Duration {
	if v := os.Getenv("HF_SHELL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 && d <= 30*time.Minute {
			return d
		}
	}
	return 60 * time.Second
}

// fdRedirectRe matches fd-redirection tokens like "2>&1", ">&2", "&>" so they
// are not mistaken for command separators when splitting on '&'.
var fdRedirectRe = regexp.MustCompile(`[0-9]*>&[0-9]*|&>>?`)

// ParseCommands splits a command string on &&, ||, ;, | and returns base command names.
// Also detects subcommand patterns that could bypass allowlist checks.
func ParseCommands(command string) []string {
	// Detect subcommand patterns that bypass simple splitting
	subcommandPatterns := []string{"$(", "`", "<(", ">("}
	for _, pat := range subcommandPatterns {
		if strings.Contains(command, pat) {
			// Return a sentinel that won't match any allowlist
			return []string{"__subcommand_blocked__"}
		}
	}

	// Strip fd redirections ("cmd 2>&1 | head") before splitting — otherwise
	// the "&" inside "2>&1" yields bogus segments like "1" that fail allowlists.
	command = fdRedirectRe.ReplaceAllString(command, " ")

	// Split on shell operators, but only outside quotes — a quoted ';', '|' or
	// '&' is argument data, not a separator (e.g. --content-type
	// "text/markdown; charset=utf-8"), and splitting there rejects legitimate
	// allowlisted commands.
	var segs []string
	var cur strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		switch {
		case escaped:
			escaped = false
			cur.WriteRune(r)
		case r == '\\' && quote != '\'':
			escaped = true
			cur.WriteRune(r)
		case quote != 0:
			if r == quote {
				quote = 0
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			cur.WriteRune(r)
		case r == '&' || r == '|' || r == ';':
			segs = append(segs, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	segs = append(segs, cur.String())

	var cmds []string
	for _, seg := range segs {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		// Handle paths like /usr/bin/pytest → pytest
		parts := strings.Split(fields[0], "/")
		base := parts[len(parts)-1]
		if base != "" {
			cmds = append(cmds, base)
		}
	}
	return cmds
}

// BaseCommand extracts the first command (backward compat).
func BaseCommand(command string) string {
	cmds := ParseCommands(command)
	if len(cmds) == 0 {
		return ""
	}
	return cmds[0]
}
