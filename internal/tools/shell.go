package tools

import (
	"encoding/json"
	"fmt"
	"os/exec"
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

	timer := time.AfterFunc(60*time.Second, func() { cmd.Process.Kill() })
	defer timer.Stop()

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("shell exec: %w\n%s", err, string(out))
	}
	return string(out), nil
}

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

	// Split on shell operators
	var cmds []string
	for _, seg := range strings.FieldsFunc(command, func(r rune) bool {
		return r == '&' || r == '|' || r == ';'
	}) {
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
