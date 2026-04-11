package tools

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type GitTool struct{}

func NewGitTool() *GitTool { return &GitTool{} }

func (g *GitTool) Name() string { return "git" }

func (g *GitTool) Operations() []Operation {
	return []Operation{
		{Name: "status", Description: "Show git status", Parameters: []ParamDef{}},
		{Name: "diff", Description: "Show git diff", Parameters: []ParamDef{
			{Name: "args", Type: "string", Description: "Additional diff arguments (e.g. --cached, file path)", Required: false},
		}},
		{Name: "log", Description: "Show git log", Parameters: []ParamDef{
			{Name: "args", Type: "string", Description: "Log arguments (e.g. -10 --oneline)", Required: false},
		}},
		{Name: "show", Description: "Show a git object", Parameters: []ParamDef{
			{Name: "ref", Type: "string", Description: "Git ref to show (e.g. HEAD, commit hash)", Required: true},
		}},
		{Name: "commit", Description: "Create a git commit", Parameters: []ParamDef{
			{Name: "message", Type: "string", Description: "Commit message", Required: true},
		}},
		{Name: "push", Description: "Push to remote", Parameters: []ParamDef{
			{Name: "args", Type: "string", Description: "Push arguments", Required: false},
		}},
	}
}

func (g *GitTool) Execute(op string, params json.RawMessage, cwd string) (string, error) {
	var p struct {
		Args    string `json:"args"`
		Ref     string `json:"ref"`
		Message string `json:"message"`
	}
	json.Unmarshal(params, &p)

	var args []string
	switch op {
	case "status":
		args = []string{"status", "--short"}
	case "diff":
		args = append([]string{"diff"}, splitArgs(p.Args)...)
	case "log":
		args = append([]string{"log"}, splitArgs(p.Args)...)
		if p.Args == "" {
			args = append(args, "-20", "--oneline")
		}
	case "show":
		args = []string{"show", p.Ref}
	case "commit":
		args = []string{"commit", "-m", p.Message}
	case "push":
		args = append([]string{"push"}, splitArgs(p.Args)...)
	default:
		return "", fmt.Errorf("git: unknown op %s", op)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", op, err, string(out))
	}
	return string(out), nil
}

func splitArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}
